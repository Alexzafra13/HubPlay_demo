#!/usr/bin/env python3
"""seek-check — prueba de saltos adelante/atrás contra el servidor REAL.

Genera dos películas de 12 min con ffmpeg (H.264 → DirectStream/remux y
MPEG-4 → Transcode), arranca el binario con una config temporal, hace el
wizard por API, escanea, y pide segmentos HLS fuera de orden
(0,1,2,60,61,10,11,100,59,119,3). Para cada uno comprueba con ffprobe que
el primer PTS de vídeo cae donde dice el manifest (suma de EXTINF), con
tolerancia de 2,5 s (offset MPEG-TS de ~1,4 s + jitter).

Uso (desde la raíz del repo, con ffmpeg/ffprobe en el PATH):

    go build -o /tmp/hubplay ./cmd/hubplay
    python3 scripts/seek-check.py /tmp/hubplay

Código de salida 0 si todo cuadra. Estado verificado el 2026-11-26:
Transcode OK; Remux con 6/11 segmentos desplazados +2 s (el manifest
sintético declara 6 s y el remux solo puede cortar en keyframes del
fichero) — ver docs/memory/roadmap-producto.md, pilar 2.
"""
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

SEQ = [0, 1, 2, 60, 61, 10, 11, 100, 59, 119, 3]
TOLERANCE = 2.5
DURATION = 720  # 12 min → 120 segmentos de 6 s


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def gen_media(root):
    movies = os.path.join(root, "movies")
    specs = [
        # keyframe cada 4 s: NO cae en la rejilla de 6 s → destapa el remux
        ("Remux Seek (2026)", "mkv", ["-c:v", "libx264", "-preset", "ultrafast", "-g", "100", "-c:a", "aac"]),
        ("Transcode Seek (2026)", "avi", ["-c:v", "mpeg4", "-q:v", "8", "-g", "250", "-c:a", "libmp3lame"]),
    ]
    for title, ext, codec in specs:
        d = os.path.join(movies, title)
        os.makedirs(d)
        subprocess.run(
            ["ffmpeg", "-v", "error", "-y",
             "-f", "lavfi", "-i", f"testsrc2=size=320x180:rate=25:duration={DURATION}",
             "-f", "lavfi", "-i", f"sine=frequency=440:duration={DURATION}",
             *codec, "-shortest", os.path.join(d, f"{title}.{ext}")],
            check=True)
    return movies


class Client:
    def __init__(self, base):
        self.base, self.tok = base, ""

    def req(self, method, path, body=None, timeout=60):
        data = json.dumps(body).encode() if body is not None else None
        r = urllib.request.Request(path if path.startswith("http") else self.base + path,
                                   data=data, method=method)
        r.add_header("Content-Type", "application/json")
        if self.tok:
            r.add_header("Authorization", "Bearer " + self.tok)
        t = time.time()
        try:
            with urllib.request.urlopen(r, timeout=timeout) as resp:
                return resp.status, resp.read(), time.time() - t
        except urllib.error.HTTPError as e:
            return e.code, e.read(), time.time() - t

    def json(self, method, path, body=None):
        st, raw, _ = self.req(method, path, body)
        if st >= 300:
            raise RuntimeError(f"{method} {path}: HTTP {st} {raw[:200]!r}")
        return json.loads(raw or b"{}").get("data")


def check_item(c, item_id, title):
    master = c.req("GET", f"/api/v1/stream/{item_id}/master.m3u8")[1].decode()
    variant = [l for l in master.splitlines() if l and not l.startswith("#")][0]
    vurl = variant if variant.startswith(("/", "http")) else f"/api/v1/stream/{item_id}/{variant}"
    pl = c.req("GET", vurl)[1].decode()
    segs = [l for l in pl.splitlines() if l and not l.startswith("#")]
    durs = [float(x) for x in re.findall(r"#EXTINF:([\d.]+)", pl)]
    vdir = vurl.rsplit("/", 1)[0] + "/"
    method = c.json("GET", f"/api/v1/stream/{item_id}/info").get("method")
    print(f"\n{title} — {method}, {len(segs)} segmentos")
    bad = 0
    tmp = tempfile.mkdtemp()
    for idx in SEQ:
        u = segs[idx] if segs[idx].startswith(("/", "http")) else vdir + segs[idx]
        st, body, dt = c.req("GET", u)
        if st != 200:
            print(f"  seg {idx:3d}: HTTP {st} en {dt:.2f}s")
            bad += 1
            continue
        p = os.path.join(tmp, f"{idx}.ts")
        with open(p, "wb") as f:
            f.write(body)
        out = subprocess.run(["ffprobe", "-v", "error", "-select_streams", "v:0",
                              "-show_entries", "packet=pts_time", "-of", "csv=p=0", p],
                             capture_output=True, text=True).stdout.split()
        pts = [float(x.strip(",")) for x in out if x.strip(",") not in ("", "N/A")]
        expected = sum(durs[:idx])
        if not pts:
            print(f"  seg {idx:3d}: sin vídeo")
            bad += 1
            continue
        drift = min(pts) - expected
        ok = abs(drift) < TOLERANCE
        bad += 0 if ok else 1
        print(f"  seg {idx:3d}: {dt:5.2f}s  manifest {expected:6.1f}s  pts {min(pts):7.2f}..{max(pts):7.2f}"
              f"  drift {drift:+.2f}  {'OK' if ok else 'MAL'}")
    shutil.rmtree(tmp, ignore_errors=True)
    return bad


def main():
    if len(sys.argv) != 2:
        sys.exit(__doc__)
    binary = os.path.abspath(sys.argv[1])
    root = tempfile.mkdtemp(prefix="hubplay-seek-")
    proc = None
    try:
        movies = gen_media(root)
        port = free_port()
        cfg = os.path.join(root, "hubplay.yaml")
        with open(cfg, "w") as f:
            f.write(f"""server:
  bind: "127.0.0.1"
  port: {port}
database:
  driver: "sqlite"
  path: "{root}/hubplay.db"
logging:
  level: "warn"
streaming:
  cache_dir: "{root}/transcode"
mdns:
  enabled: false
discovery:
  enabled: false
""")
        log = open(os.path.join(root, "server.log"), "w")
        proc = subprocess.Popen([binary, "--config", cfg], stdout=log, stderr=log)
        c = Client(f"http://127.0.0.1:{port}")
        for _ in range(120):
            try:
                if c.req("GET", "/api/v1/setup/status", timeout=2)[0] == 200:
                    break
            except OSError:
                pass
            time.sleep(0.25)
        c.tok = c.json("POST", "/api/v1/auth/setup",
                       {"username": "admin", "password": "Admin12345!", "display_name": "Seek"})["access_token"]
        c.json("POST", "/api/v1/setup/libraries",
               {"libraries": [{"name": "Movies", "content_type": "movies", "paths": [movies]}]})
        c.json("POST", "/api/v1/setup/complete", {"start_scan": True})
        items = []
        for _ in range(120):
            items = c.json("GET", "/api/v1/items?limit=10&type=movie").get("items") or []
            if len(items) >= 2:
                break
            time.sleep(0.5)
        bad = sum(check_item(c, it["id"], it["title"]) for it in sorted(items, key=lambda i: i["title"]))
        print("\nRESULTADO:", "OK" if bad == 0 else f"{bad} segmentos desplazados o fallidos")
        sys.exit(0 if bad == 0 else 1)
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=10)
        shutil.rmtree(root, ignore_errors=True)


if __name__ == "__main__":
    main()

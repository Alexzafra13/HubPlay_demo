#!/usr/bin/env python3
"""seek-check — prueba de saltos adelante/atrás contra el servidor REAL.

Genera cuatro películas de 6 min con ffmpeg (tres H.264 → DirectStream/
remux: keyframes regulares, irregulares con B-frames, y GOP abierto en
MP4 con audio AC3 a convertir; y una MPEG-4 → Transcode), arranca el binario con una config temporal, hace el
wizard por API, escanea, y pide segmentos HLS fuera de orden
(arranque, saltos adelante y atrás, final). Para cada uno comprueba con
ffprobe que empieza en keyframe y donde dice el manifest (suma de
EXTINF): desfase constante entre segmentos (±0,1 s; el offset MPEG-TS
es ~1,4 s) y, entre segmentos consecutivos, sin huecos ni solapes.

Uso (desde la raíz del repo, con ffmpeg/ffprobe en el PATH):

    go build -o /tmp/hubplay ./cmd/hubplay
    python3 scripts/seek-check.py /tmp/hubplay

Código de salida 0 si todo cuadra. Historia: el 2026-11-26 el remux
fallaba (6/11 segmentos +2 s: manifest sintético de 6 s frente a cortes
en los keyframes del fichero); desde el remux por segmentos
(internal/stream/remux.go) los cortes del manifest son los reales.
"""
import json
import os
import random
import re
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

# Posiciones como fracción del número de segmentos: arranque, salto
# adelante, atrás, casi al final, de vuelta al principio.
SEQ = [0.0, "+1", "+1", 0.5, "+1", 0.08, "+1", 0.85, 0.49, 1.0, 0.03]
TOLERANCE = 2.5   # |desfase| máximo (offset MPEG-TS ~1,4 s)
SPREAD = 0.1      # variación máxima del desfase entre segmentos
DURATION = 360  # 6 min


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def gen_media(root):
    movies = os.path.join(root, "movies")
    # Keyframes irregulares (como x264 con detección de escena).
    rnd = random.Random(7)
    t, kf = 0.0, []
    while t < DURATION:
        kf.append(f"{t:.3f}")
        t += rnd.uniform(0.8, 9.5)
    irregular = ",".join(kf)
    specs = [
        # keyframe cada 4 s: NO cae en la rejilla de 6 s
        ("Remux Regular (2026)", "mkv", ["-c:v", "libx264", "-preset", "ultrafast", "-g", "100", "-c:a", "aac"]),
        # B-frames + keyframes irregulares
        ("Remux Irregular (2026)", "mkv", ["-c:v", "libx264", "-preset", "fast", "-x264-params",
                                           "keyint=600:min-keyint=1:scenecut=0", "-force_key_frames", irregular,
                                           "-c:a", "aac"]),
        # GOP abierto (keyframes no IDR) en MP4 + audio AC3 (el navegador
        # no lo decodifica → vídeo copiado y audio convertido a AAC)
        ("Remux OpenGOP AC3 (2026)", "mp4", ["-c:v", "libx264", "-preset", "fast", "-x264-params",
                                             "keyint=150:min-keyint=20:open-gop=1:bframes=3",
                                             "-c:a", "ac3", "-b:a", "192k"]),
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
    n = len(segs)
    order, prev = [], 0
    for pos in SEQ:
        idx = prev + 1 if pos == "+1" else min(n - 1, round(pos * (n - 1)))
        order.append(idx)
        prev = idx
    drifts, ranges = [], {}
    for idx in order:
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
                              "-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", p],
                             capture_output=True, text=True).stdout.split()
        pk = []
        for line in out:
            f = line.split(",")
            if f[0] not in ("", "N/A"):
                pk.append((float(f[0]), "K" in f[1]))
        if not pk:
            print(f"  seg {idx:3d}: sin vídeo")
            bad += 1
            continue
        pts = sorted(t for t, _ in pk)
        expected = sum(durs[:idx])
        drift = pts[0] - expected
        drifts.append(drift)
        ranges[idx] = (pts[0], pts[-1])
        key_first = pk[0][1]
        ok = abs(drift) < TOLERANCE and key_first
        bad += 0 if ok else 1
        print(f"  seg {idx:3d}: {dt:5.2f}s  manifest {expected:6.1f}s  pts {pts[0]:7.2f}..{pts[-1]:7.2f}"
              f"  drift {drift:+.2f}{'' if key_first else '  (NO empieza en keyframe)'}  {'OK' if ok else 'MAL'}")
    if drifts and max(drifts) - min(drifts) > SPREAD:
        print(f"  desfase variable entre segmentos: {min(drifts):+.3f}..{max(drifts):+.3f}  MAL")
        bad += 1
    # Continuidad al reproducir seguido: el siguiente empieza justo
    # tras el anterior (ni hueco ni solape de más de un fotograma).
    for a in ranges:
        if a + 1 in ranges:
            gap = ranges[a + 1][0] - ranges[a][1]
            if not (0 < gap < 0.2):
                print(f"  seg {a}->{a + 1}: hueco/solape {gap:+.3f}s  MAL")
                bad += 1
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
            if len(items) >= 4:
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

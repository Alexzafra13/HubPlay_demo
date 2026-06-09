// k6 API regression / load script for HubPlay's browse surface.
//
// k6 (https://k6.io) is the load tool you want for real numbers: it
// reports latency percentiles (p95/p99) and FAILS if a threshold is
// breached — so it doubles as a CI gate. This drives the authenticated
// browse/search/list mix; it does NOT measure transcoding (that's a
// media-server concern measured by watching CPU/GPU under real playback —
// see docs/perf-measurement.md §3).
//
// Seed a catalogue first (go run ./cmd/hpseed -items 5000 -channels 5000),
// then:
//
//   k6 run -e BASE=http://localhost:8096 -e USER=admin -e PASS=hubplay123 \
//     scripts/perf/k6-api.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE = __ENV.BASE || "http://localhost:8096";
const USER = __ENV.USER || "admin";
const PASS = __ENV.PASS || "hubplay123";

export const options = {
  scenarios: {
    browse: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "30s", target: 30 },
        { duration: "1m", target: 30 },
        { duration: "10s", target: 0 },
      ],
    },
  },
  thresholds: {
    // Tune these to your hardware: they're the pass/fail line.
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500", "p(99)<1500"],
  },
};

export function setup() {
  const res = http.post(
    `${BASE}/api/v1/auth/login`,
    JSON.stringify({ username: USER, password: PASS }),
    { headers: { "Content-Type": "application/json" } }
  );
  check(res, { "login 200": (r) => r.status === 200 });
  const body = res.json();
  const token = body.access_token || (body.data && body.data.access_token);
  if (!token) throw new Error("login returned no access_token");
  return { token };
}

const PATHS = [
  "/api/v1/items?limit=50",
  "/api/v1/items?limit=50&offset=200",
  "/api/v1/items/latest?limit=20",
  "/api/v1/items/search?q=Movie&limit=50",
  "/api/v1/items/genres",
  "/api/v1/libraries",
];

export default function (data) {
  const params = { headers: { Authorization: `Bearer ${data.token}` } };
  const path = PATHS[Math.floor(Math.random() * PATHS.length)];
  const res = http.get(`${BASE}${path}`, params);
  check(res, { "2xx": (r) => r.status >= 200 && r.status < 300 });
  sleep(0.2);
}

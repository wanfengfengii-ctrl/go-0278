#!/usr/bin/env bash
# Deterministic smoke test for the rock-bolt pullout inspection service.
#
# It builds the server, starts it against a temporary SQLite database, drives a
# real end-to-end flow over the local HTTP API, and asserts each response. It
# performs no external network access, never relies on `go test`, and cleans up
# every spawned process and temporary file on exit.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${HERE}"

WORKDIR="$(mktemp -d)"
SERVER_PID=""
BIN=""

cleanup() {
  if [[ -n "${SERVER_PID}" ]]; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT

echo "==> building server binary"
BIN="${WORKDIR}/server"
CGO_ENABLED=0 go build -o "${BIN}" ./cmd/server

# Pick a free local port deterministically.
PORT="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
ADDR="127.0.0.1:${PORT}"
DB="${WORKDIR}/smoke.db"

echo "==> starting server on ${ADDR}"
ADDR="${ADDR}" DB_PATH="${DB}" "${BIN}" &
SERVER_PID=$!

# Wait for the health endpoint to come up.
ready=""
for _ in $(seq 1 50); do
  if code="$(curl -s -o /dev/null -w '%{http_code}' "http://${ADDR}/healthz" 2>/dev/null || true)"; then
    if [[ "${code}" == "200" ]]; then
      ready="1"
      break
    fi
  fi
  sleep 0.1
done
if [[ -z "${ready}" ]]; then
  echo "server did not become healthy" >&2
  exit 1
fi

# request performs a single JSON POST and prints "<http_status> <body>".
# The body and status are captured from one curl invocation so a request is never
# sent twice (which would break idempotent write endpoints).
request() {
  local path="$1"
  local payload="$2"
  local out status body
  out="$(curl -s -w '\n%{http_code}' -X POST "http://${ADDR}${path}" \
    -H 'Content-Type: application/json' \
    -d "${payload}")"
  status="${out##*$'\n'}"
  body="${out%$'\n'*}"
  printf '%s %s\n' "${status}" "${body}"
}

# get performs a single JSON GET and prints "<http_status> <body>".
get() {
  local path="$1"
  local out status body
  out="$(curl -s -w '\n%{http_code}' "http://${ADDR}${path}")"
  status="${out##*$'\n'}"
  body="${out%$'\n'*}"
  printf '%s %s\n' "${status}" "${body}"
}

assert_status() {
  local got="$1" want="$2" step="$3"
  if [[ "${got}" != "${want}" ]]; then
    echo "FAIL ${step}: expected HTTP ${want}, got ${got}" >&2
    exit 1
  fi
}

assert_contains() {
  local body="$1" needle="$2" step="$3"
  if ! printf '%s' "${body}" | grep -q "${needle}"; then
    echo "FAIL ${step}: body does not contain '${needle}'" >&2
    exit 1
  fi
}

# 1. Create a pending-lock task.
resp="$(request /v1/tasks '{"mileage_start_mm":12340000,"mileage_end_mm":12380000,"rock_grade":"III","cycle_no":1,"sampling_seed":42}')"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 201 "create task"
assert_contains "${body}" '"id":"t-1"' "create task"

# 2. Lock the task with a complete snapshot.
LOCK='{
  "operation_id":"op-lock",
  "layout_revision":"rev-3","layout_digest":"ld1","grout_digest":"gd1",
  "rock_grade":"III","cycle_no":1,
  "layer_quotas":{"crown":2,"left_waist":1},
  "load_levels":[{"level":1,"target_load":50000,"hold_secs":2}],
  "stop_threshold":900000,"accept_threshold":500000,
  "influence_bound":"1","calibration_digest":"cd1",
  "candidates":[
    {"mileage_mm":12350000,"ring_no":1,"azimuth":"crown","hole_no":1,"coord_x":100,"coord_y":200,"coord_z":300,"bar_batch":"bar-A","anchor_batch":"anc-X","cycle_no":1},
    {"mileage_mm":12350000,"ring_no":1,"azimuth":"crown","hole_no":2,"coord_x":101,"coord_y":201,"coord_z":301,"bar_batch":"bar-A","anchor_batch":"anc-X","cycle_no":1},
    {"mileage_mm":12350000,"ring_no":1,"azimuth":"left_waist","hole_no":1,"coord_x":102,"coord_y":202,"coord_z":302,"bar_batch":"bar-B","anchor_batch":"anc-Y","cycle_no":1},
    {"mileage_mm":12350000,"ring_no":2,"azimuth":"crown","hole_no":1,"coord_x":103,"coord_y":203,"coord_z":303,"bar_batch":"bar-A","anchor_batch":"anc-Z","cycle_no":1}
  ]
}'
resp="$(request /v1/tasks/t-1/lock "${LOCK}")"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "lock task"
assert_contains "${body}" '"status":"pending_sample"' "lock task"

# 3. Sample.
resp="$(request /v1/tasks/t-1/samples '{"operation_id":"op-sample"}')"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "sample"
assert_contains "${body}" '"digest"' "sample"

# 4. Sample tree.
resp="$(get /v1/tasks/t-1/sample-tree)"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "sample tree"
assert_contains "${body}" '"sample-1"' "sample tree"

# 5. Verify all sampled holes.
resp="$(request /v1/tasks/t-1/holes/verify '{"operation_id":"op-verify","sample_ids":["sample-1","sample-2","sample-3"]}')"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "verify holes"

# 6. Acquire the three-device lease combination.
resp="$(request /v1/tests/t-1/leases '{"operation_id":"op-lease","test_id":"t-1","task_id":"t-1","generation":1,"puller":"puller-1","pump":"pump-1","displacement":"disp-1","start":1,"end":100000}')"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "acquire leases"

# 7. Submit the full load sequence for sample-1.
readings=(
  '{"stage":"preload","load":1000,"displacement":100}'
  '{"stage":"load","load_level":1,"load":50000,"displacement":900}'
  '{"stage":"hold","load_level":1,"load":50000,"displacement":950,"hold_secs":2}'
  '{"stage":"unload","load":0,"displacement":400}'
  '{"stage":"rebound","displacement":400,"rebound":300}'
)
i=0
for step in "${readings[@]}"; do
  i=$((i+1))
  payload="$(printf '{"operation_id":"op-read-%d","task_id":"t-1","sample_id":"sample-1","device_puller":"puller-1","device_pump":"pump-1","device_displacement":"disp-1",%s' "${i}" "${step#?}")"
  resp="$(request /v1/tests/t-1/readings "${payload}")"
  status="${resp%% *}"; body="${resp#* }"
  assert_status "${status}" 200 "reading ${i}"
  assert_contains "${body}" '"accepted":true' "reading ${i}"
done

# 8. Evidence chain is immutable and queryable.
resp="$(get /v1/tests/t-1/evidence)"
status="${resp%% *}"; body="${resp#* }"
assert_status "${status}" 200 "evidence"
assert_contains "${body}" '"rebound"' "evidence"

echo "SMOKE OK"

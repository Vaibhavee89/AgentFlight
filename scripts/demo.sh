#!/usr/bin/env bash
# Starts agentflight with seeded demo faults and fills it with sample runs,
# so the UI has something to show on first open. Runs until Ctrl-C.
set -euo pipefail

BIN="${1:?usage: demo.sh <path-to-agentflight-binary>}"
ADDR="${DEMO_ADDR:-127.0.0.1:8080}"
DB="${DEMO_DB:-demo.db}"
BASE="http://${ADDR}"

rm -f "${DB}" "${DB}-wal" "${DB}-shm"

"${BIN}" serve --addr "${ADDR}" --db "${DB}" \
  --mock-latency 600ms --demo-faults 0.3 --demo-seed 7 --bench-dir docs &
SERVER_PID=$!
trap 'kill "${SERVER_PID}" 2>/dev/null || true' EXIT INT TERM

# Wait for the server to answer, up to about 15 seconds.
for _ in $(seq 1 60); do
  if curl -fsS "${BASE}/healthz" >/dev/null 2>&1; then break; fi
  sleep 0.25
done
curl -fsS "${BASE}/healthz" >/dev/null

json_field() { python3 -c "import sys,json; print(json.load(sys.stdin)[sys.argv[1]])" "$1"; }

# Polls a run until its status matches the wanted value, up to about 30 seconds.
wait_status() {
  local id="$1" want="$2"
  for _ in $(seq 1 120); do
    local got
    got="$(curl -fsS "${BASE}/runs/${id}" | json_field status)"
    if [ "${got}" = "${want}" ]; then return 0; fi
    sleep 0.25
  done
  echo "run ${id} did not reach ${want}" >&2
  return 1
}

start_run() {
  curl -fsS -X POST -d "{\"prompt\":\"$1\"}" "${BASE}/runs" | json_field id
}

echo "seeding sample runs..."
for prompt in "summarise the disk usage report" "check the nightly backup" "list open pull requests"; do
  id="$(start_run "${prompt}")"
  wait_status "${id}" completed
  echo "  completed  ${id}  ${prompt}"
done

# One run is cancelled part way through, then resumed to completion.
id="$(start_run "rebuild the search index, then verify it")"
sleep 0.8
curl -fsS -X POST "${BASE}/runs/${id}/cancel" >/dev/null
wait_status "${id}" cancelled
echo "  cancelled  ${id}"
curl -fsS -X POST "${BASE}/runs/${id}/resume" >/dev/null
wait_status "${id}" completed
echo "  resumed    ${id}  completed after resume"

echo
echo "Dashboard: ${BASE}/ui/runs"
echo "Press Ctrl-C to stop the server."
wait "${SERVER_PID}"

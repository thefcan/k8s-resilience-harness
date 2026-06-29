#!/usr/bin/env bash
# Measure the steady-state baseline: port-forward the testapp Service, drive a
# constant request rate with loadgen, and write a JSON report to results/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NAMESPACE:-kresil}"
RPS="${RPS:-50}"
DURATION="${DURATION:-30s}"
LOCAL_PORT="${LOCAL_PORT:-18080}"
OUT="${OUT:-${ROOT}/results/baseline.json}"

PF_PID=""
cleanup() {
  if [[ -n "${PF_PID}" ]]; then
    kill "${PF_PID}" >/dev/null 2>&1 || true
    wait "${PF_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> port-forwarding svc/testapp ${LOCAL_PORT}->80"
kubectl -n "${NS}" port-forward svc/testapp "${LOCAL_PORT}:80" >/dev/null 2>&1 &
PF_PID=$!

echo "==> waiting for the forward to accept traffic"
ready=false
for _ in $(seq 1 40); do
  if curl -sf "http://localhost:${LOCAL_PORT}/livez" >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 0.5
done
if [[ "${ready}" != "true" ]]; then
  echo "error: testapp did not become reachable via port-forward" >&2
  exit 1
fi

echo "==> running loadgen baseline (${RPS} rps for ${DURATION})"
( cd "${ROOT}" && go run ./loadgen \
    -target "http://localhost:${LOCAL_PORT}/work" \
    -rps "${RPS}" \
    -duration "${DURATION}" \
    -out "${OUT}" )

echo "==> baseline report written to ${OUT}"

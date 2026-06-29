#!/usr/bin/env bash
# Measure the steady-state baseline: drive a constant request rate at the
# testapp Service (via the kind-published NodePort, load-balanced across all
# replicas) and write a JSON report to results/.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RPS="${RPS:-50}"
DURATION="${DURATION:-30s}"
NODE_PORT="${NODE_PORT:-30080}"
HOST="${HOST:-localhost}"
OUT="${OUT:-${ROOT}/results/baseline.json}"

BASE_URL="http://${HOST}:${NODE_PORT}"

echo "==> waiting for testapp to accept traffic at ${BASE_URL}"
ready=false
for _ in $(seq 1 40); do
  if curl -sf "${BASE_URL}/livez" >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 0.5
done
if [[ "${ready}" != "true" ]]; then
  echo "error: testapp is not reachable at ${BASE_URL} (is the cluster up and deployed?)" >&2
  exit 1
fi

echo "==> running loadgen baseline (${RPS} rps for ${DURATION})"
( cd "${ROOT}" && go run ./loadgen \
    -target "${BASE_URL}/work" \
    -rps "${RPS}" \
    -duration "${DURATION}" \
    -out "${OUT}" )

echo "==> baseline report written to ${OUT}"

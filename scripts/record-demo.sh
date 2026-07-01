#!/usr/bin/env bash
# Records the terminal demo (docs/img/demo.gif) of a real pod-kill resilience
# experiment, using asciinema (a pure-PTY recorder) + agg (cast -> GIF).
#
# Prereqs:
#   * a running kresil kind cluster:  make cluster-up images deploy
#   * asciinema + agg on PATH:         brew install asciinema agg
# Usage:
#   scripts/record-demo.sh
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
CAST="$REPO/docs/img/demo.cast"
GIF="$REPO/docs/img/demo.gif"

# ---------------------------------------------------------------------------
# inner mode: the on-screen sequence that asciinema records. Kept in this same
# file so the whole recipe is one artifact.
# ---------------------------------------------------------------------------
if [[ "${1:-}" == "inner" ]]; then
  cd "$REPO"
  export PATH="$(go env GOPATH)/bin:$PATH"
  GREEN=$'\033[1;32m'; DIM=$'\033[38;5;245m'; RESET=$'\033[0m'
  say()  { printf '%s%s%s\n' "$DIM" "$1" "$RESET"; }
  run() {                       # animate typing at a shell prompt, then execute
    printf '%s❯%s ' "$GREEN" "$RESET"
    local s="$1" i
    for (( i = 0; i < ${#s}; i++ )); do printf '%s' "${s:i:1}"; sleep 0.018; done
    printf '\n'
    eval "$s"
  }
  clear
  sleep 0.4
  say "# 3 testapp replicas + a Redis StatefulSet, live on a kind cluster"
  sleep 0.3
  run "kubectl get pods -n kresil"
  sleep 1.8
  say "# Inject a fault, check a steady-state hypothesis, emit a verdict:"
  sleep 0.3
  run "go run ./cmd/harness run -experiment experiments/pod-kill.yaml"
  sleep 1.5
  exit 0
fi

# ---------------------------------------------------------------------------
# outer mode: record + render
# ---------------------------------------------------------------------------
command -v asciinema >/dev/null || { echo "need asciinema: brew install asciinema"; exit 1; }
command -v agg       >/dev/null || { echo "need agg: brew install agg"; exit 1; }

# Warm the build cache so `go run` doesn't stall on compilation mid-recording.
( cd "$REPO" && go build ./... >/dev/null 2>&1 ) || true

rm -f "$CAST"
asciinema rec \
  --window-size 126x28 \
  --output-format asciicast-v2 \
  --overwrite \
  --command "bash '$0' inner" \
  "$CAST"

agg \
  --theme github-dark \
  --font-size 20 \
  --line-height 1.35 \
  --speed 1.5 \
  --idle-time-limit 1.6 \
  --last-frame-duration 3 \
  "$CAST" "$GIF"

rm -f "$CAST"
echo "wrote $GIF"
ls -lh "$GIF"

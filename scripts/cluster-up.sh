#!/usr/bin/env bash
# Bring up the local kind cluster used by the harness.
#
# Idempotent: if a cluster with the same name already exists it is reused.
# Honours CLUSTER_NAME (default: kresil).
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-kresil}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KIND_CONFIG="${SCRIPT_DIR}/kind-config.yaml"

# `kind` installed via `go install` lands in $(go env GOPATH)/bin, which is not
# always on PATH. Add it so local runs work without extra shell setup.
if command -v go >/dev/null 2>&1; then
  export PATH="$(go env GOPATH)/bin:${PATH}"
fi

if ! command -v kind >/dev/null 2>&1; then
  echo "error: 'kind' not found on PATH." >&2
  echo "       install it with: go install sigs.k8s.io/kind@v0.30.0" >&2
  exit 1
fi

if ! docker info >/dev/null 2>&1; then
  echo "error: Docker daemon is not reachable. Start Docker and retry." >&2
  exit 1
fi

if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  echo "==> kind cluster '${CLUSTER_NAME}' already exists; reusing it."
else
  echo "==> creating kind cluster '${CLUSTER_NAME}'..."
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --config "${KIND_CONFIG}" \
    --wait 90s
fi

echo "==> cluster info:"
kubectl cluster-info --context "kind-${CLUSTER_NAME}"

echo "==> nodes:"
kubectl get nodes -o wide

echo "==> kind cluster '${CLUSTER_NAME}' is ready."

#!/usr/bin/env bash
# Tear down the local kind cluster.
# Honours CLUSTER_NAME (default: kresil). Safe to run when no cluster exists.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-kresil}"

if command -v go >/dev/null 2>&1; then
  export PATH="$(go env GOPATH)/bin:${PATH}"
fi

if ! command -v kind >/dev/null 2>&1; then
  echo "error: 'kind' not found on PATH." >&2
  exit 1
fi

if kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  echo "==> deleting kind cluster '${CLUSTER_NAME}'..."
  kind delete cluster --name "${CLUSTER_NAME}"
  echo "==> done."
else
  echo "==> no kind cluster named '${CLUSTER_NAME}'; nothing to do."
fi

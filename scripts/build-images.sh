#!/usr/bin/env bash
# Build the testapp image and load it into the kind cluster so the Deployment
# (imagePullPolicy: IfNotPresent) can use it without a registry.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-kresil}"
IMAGE="${TESTAPP_IMAGE:-testapp:dev}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if command -v go >/dev/null 2>&1; then
  export PATH="$(go env GOPATH)/bin:${PATH}"
fi

echo "==> building image ${IMAGE}"
docker build -f "${ROOT}/testapp/Dockerfile" -t "${IMAGE}" "${ROOT}"

echo "==> loading ${IMAGE} into kind cluster '${CLUSTER_NAME}'"
kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"

echo "==> done."

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

# Best-effort preload of Prometheus (M4) into kind: on Linux CI this avoids a
# Docker Hub pull at pod-schedule time, but Docker Desktop's containerd image
# store can't `kind load` a multi-arch manifest list, so we don't fail the build
# if it doesn't take — the Deployment's imagePullPolicy: IfNotPresent means the
# kubelet just pulls it directly instead. Keep this tag in sync with
# deploy/base/prometheus-deployment.yaml.
PROM_IMAGE="${PROM_IMAGE:-prom/prometheus:v3.6.0}"
echo "==> preloading ${PROM_IMAGE} into kind (best-effort)"
if docker pull -q "${PROM_IMAGE}" >/dev/null 2>&1 \
  && kind load docker-image "${PROM_IMAGE}" --name "${CLUSTER_NAME}" >/dev/null 2>&1; then
  echo "==> ${PROM_IMAGE} preloaded into kind"
else
  echo "==> preload skipped; kubelet will pull ${PROM_IMAGE} on schedule"
fi

echo "==> done."

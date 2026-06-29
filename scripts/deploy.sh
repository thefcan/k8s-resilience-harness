#!/usr/bin/env bash
# Apply the kustomize manifests and wait for Redis + testapp to become ready.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
NS="${NAMESPACE:-kresil}"

echo "==> applying manifests (deploy/base)"
kubectl apply -k "${ROOT}/deploy/base"

echo "==> waiting for redis to be ready"
kubectl -n "${NS}" rollout status statefulset/redis --timeout=120s

echo "==> waiting for testapp to be ready"
kubectl -n "${NS}" rollout status deployment/testapp --timeout=120s

echo "==> workloads:"
kubectl -n "${NS}" get pods -o wide

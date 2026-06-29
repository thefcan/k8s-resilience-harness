# k8s-resilience-harness

> A Kubernetes resilience/chaos testing harness in Go: inject controlled faults
> into a system running on Kubernetes, check a **steady-state hypothesis**,
> measure recovery, and report a deterministic pass/fail — with an ML-based
> anomaly layer over accumulated runs.

This is a learning/portfolio project, built milestone by milestone. It does
**not** claim production Kubernetes operations experience; it is an honest,
runnable artifact that demonstrates resilience testing of a distributed system.

## Why

Distributed systems fail in ways unit tests never see: a pod is killed
mid-request, a node drains, the network slows. The only way to trust that a
system survives those events is to cause them on purpose and measure what
happens. This harness automates that loop — define a steady-state hypothesis,
inject a fault, and get a reproducible pass/fail plus a recovery-time number —
so resilience becomes a CI gate rather than a hope.

---

## Status

**Milestone M1 — SUT + loadgen + baseline — ✅ done.**

A multi-replica HTTP service (`testapp`) backed by a Redis StatefulSet is
deployed into a local [`kind`](https://kind.sigs.k8s.io/) cluster via kustomize,
and a constant-rate load generator (`loadgen`) measures the steady-state
baseline — success rate and latency percentiles. Fault injection and pass/fail
verdicts arrive in M2.

```text
M0  skeleton + kind cluster bring-up        ✅ done
M1  SUT + loadgen + baseline steady-state   ✅ done
M2  pod-kill injector + verdict + report    ⬜ planned   (first CV-able artifact)
M3  node-drain + resource + network faults  ⬜ planned
M4  Prometheus metrics platform             ⬜ planned
M5  scikit-learn anomaly analysis           ⬜ planned
M6  polish: README, diagram, docs           ⬜ planned
```

---

## Requirements

- Go 1.26+
- Docker (running)
- `kind` (`go install sigs.k8s.io/kind@v0.30.0`)
- `kubectl`
- `golangci-lint` v2 (for `make lint`)

## Quickstart

```bash
# Full M1 demo in one command:
#   kind cluster -> build/load image -> deploy -> measure baseline
make demo

# ...or step by step:
make cluster-up        # 1 control-plane + 2 workers
make images            # build testapp image, load into kind
make deploy            # apply manifests, wait for Redis + testapp
make baseline          # port-forward + loadgen -> results/baseline.json

# Lint + unit tests with the race detector
make lint
make test

# Tear the cluster down
make cluster-down
```

Run `make help` to list all targets. The baseline run is tunable via env:
`RPS=100 DURATION=60s make baseline`.

---

## Layout

```text
cmd/harness/          # harness CLI entrypoint
testapp/              # SUT: multi-replica HTTP service (/livez, /healthz, /work) + Dockerfile
loadgen/              # constant-RPS load generator -> baseline report
internal/
  buildinfo/          # version metadata (ldflags-stamped)
  logger/             # structured slog logger (text/json, levels)
  metrics/            # success rate + latency percentiles (shared)
deploy/base/          # kustomize: namespace, Redis StatefulSet, testapp Deployment
scripts/
  kind-config.yaml    # kind cluster topology
  cluster-up.sh       # create/reuse the local cluster
  cluster-down.sh     # delete the local cluster
  build-images.sh     # build testapp image + kind load
  deploy.sh           # kubectl apply -k + wait for rollout
  baseline.sh         # port-forward + loadgen -> results/baseline.json
experiments/          # declarative experiment YAML       (M2+)
results/              # run outputs (baseline.json, ...)
.github/workflows/    # CI: lint+test, integration (deploy + baseline)
```

## Architecture (M1)

```text
   loadgen ──(constant RPS, /work)──► NodePort :30080 ──► testapp ×3 ──(INCR)──► Redis
      │                               (kube-proxy LB)        ▲
      └─ records success rate + p50/p95/p99 ─────────────────┘
                  │
                  └─► results/baseline.json   (the steady-state baseline)
```

- **`testapp`** — `/livez` (liveness, independent of Redis), `/healthz`
  (readiness, reflects Redis reachability + drains on SIGTERM), `/work` (atomic
  Redis INCR). Generic error bodies; details are logged server-side.
- **NodePort, not port-forward** — load is driven through a kind-published
  NodePort so kube-proxy load-balances across all three replicas and re-routes
  around a killed pod. A graceful drain (fail readiness, wait, then shut down)
  keeps pod deletion from producing spurious connection errors — both needed
  for honest M2 fault measurements.
- **`loadgen`** — paced ticker + bounded worker pool; in-flight requests are not
  cancelled when the duration elapses. Pool saturation is counted separately, so
  `achieved_rps` reflects requests actually sent, not ticks emitted.
- **`internal/metrics`** — latency percentiles use nearest-rank over *successful*
  requests only; reused by the in-fault probe in M2.

### Example baseline report

A real run (`make baseline`, 50 rps for 20s) against the kind deployment — see
[`results/baseline.sample.json`](results/baseline.sample.json):

```json
{
  "requested_rps": 50,
  "requests": 1000,
  "saturated": 0,
  "achieved_rps": 50.0,
  "summary": {
    "total": 1000, "succeeded": 1000, "failed": 0, "success_rate": 1,
    "p50_ms": 2.4, "p95_ms": 6.0, "p99_ms": 8.1, "max_ms": 19.0
  }
}
```

## CI

GitHub Actions runs two jobs on every push/PR:

- **lint + unit tests** — `golangci-lint`, `go test -race ./...`, `go build`.
- **integration (kind deploy + baseline)** — installs `kind`, builds and loads
  the image, deploys via the project's own scripts, measures the baseline, and
  **fails the build if the steady-state success rate drops below 95%**. The
  baseline report is uploaded as an artifact.

## License

MIT — see [LICENSE](LICENSE).

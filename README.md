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

**Milestone M2 — pod-kill injector + steady-state verdict + report — ✅ done.**

The harness drives constant load at a multi-replica `testapp` (backed by a Redis
StatefulSet) running in a local [`kind`](https://kind.sigs.k8s.io/) cluster,
injects a fault via `client-go` (kill a random pod), and judges the run against
a declarative **steady-state hypothesis** — emitting a deterministic pass/fail
verdict, a recovery time, and a JSON + human report. A violated hypothesis fails
the process (and the CI build).

```text
M0  skeleton + kind cluster bring-up        ✅ done
M1  SUT + loadgen + baseline steady-state   ✅ done
M2  pod-kill injector + verdict + report    ✅ done
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
# Full demo in one command:
#   kind cluster -> build/load image -> deploy -> baseline -> pod-kill experiment
make demo

# ...or step by step:
make cluster-up        # 1 control-plane + 2 workers
make images            # build testapp image, load into kind
make deploy            # apply manifests, wait for Redis + testapp
make baseline          # loadgen -> results/baseline.json
make experiment        # harness run pod-kill -> results/pod-kill.json (+ verdict)

# Lint + unit tests with the race detector
make lint
make test

# Tear the cluster down
make cluster-down
```

Run `make help` to list all targets. To run an experiment directly:

```bash
go run ./cmd/harness run -experiment experiments/pod-kill.yaml -out results/pod-kill.json
# exits non-zero if the steady-state hypothesis is violated
```

---

## Layout

```text
cmd/harness/          # harness CLI: `harness run -experiment <file>`
testapp/              # SUT: multi-replica HTTP service (/livez, /healthz, /work) + Dockerfile
loadgen/              # constant-RPS load generator -> baseline report
internal/
  buildinfo/          # version metadata (ldflags-stamped)
  logger/             # structured slog logger (text/json, levels)
  metrics/            # success rate + latency percentiles (shared)
  experiment/         # declarative experiment model: parse + validate
  k8s/                # client-go clientset (in-cluster or kubeconfig)
  inject/             # fault injectors — pod-kill (client-go)
  probe/              # in-fault prober + windowed metrics + recovery time
  report/             # verdict + JSON / human report
deploy/base/          # kustomize: namespace, Redis StatefulSet, testapp Deployment
experiments/
  pod-kill.yaml       # declarative pod-kill experiment
scripts/              # cluster-up/down, build-images, deploy, baseline
results/              # run outputs (baseline.json, pod-kill.json, ...)
.github/workflows/    # CI: lint+test, integration (deploy + baseline + experiment)
```

## Architecture

```text
   harness run ─┬─► probe ──(constant RPS, /work)─► NodePort :30080 ─► testapp ×3 ─► Redis
                │                                   (kube-proxy LB)        ▲
                ├─► inject (client-go) ── kill a random pod ───────────────┘
                │
                └─► windowed metrics (baseline | fault) + recovery time
                          │
                          └─► verdict (PASS/FAIL) + results/<run>.json
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

## Experiments & verdict

An experiment is declarative — a steady-state hypothesis, a fault, and the phase
timing ([`experiments/pod-kill.yaml`](experiments/pod-kill.yaml)):

```yaml
steadyState:
  minSuccessRate: 0.95
  maxP95Ms: 300
fault:
  type: pod-kill
  namespace: kresil
  selector: app=testapp
  count: 1
phases:
  baselineSeconds: 5
  faultSeconds: 15
  recoveryTimeoutSeconds: 30
```

The harness measures a baseline, kills a pod, then measures the fault window and
recovery time, and checks the hypothesis. A real PASS run against the kind
deployment — see [`results/pod-kill.sample.json`](results/pod-kill.sample.json):

```text
Experiment: testapp-pod-kill
Fault:      pod-kill  killed=[testapp-c57f57cf4-d9z6b]
Baseline:   requests=249 ok=249 fail=0 success_rate=1.0000 p50=4.1ms p95=15.0ms ...
Fault win:  requests=750 ok=750 fail=0 success_rate=1.0000 p50=3.4ms p95=11.1ms ...
Recovery:   0.0s (recovered=true)
Verdict:    PASS
  - all steady-state conditions held
```

With three replicas behind a load-balanced Service and a graceful drain, killing
one pod stays within steady state and recovers immediately. If the hypothesis is
violated, the harness prints the failing condition and exits non-zero.

## CI

GitHub Actions runs two jobs on every push/PR:

- **lint + unit tests** — `golangci-lint`, `go test -race ./...`, `go build`.
  Unit tests cover the verdict logic, percentile math, the pod-kill injector
  (via a `client-go` **fake clientset**), and the load/probe engines.
- **integration (kind deploy + baseline + experiment)** — installs `kind`,
  builds and loads the image, deploys via the project's own scripts, measures
  the baseline (**fails if success rate < 95%**), then runs the pod-kill
  experiment (**fails if the steady-state hypothesis is violated**). Both
  reports are uploaded as artifacts.

## License

MIT — see [LICENSE](LICENSE).

# Results & Measured Metrics

Every number on this page is **measured, not asserted**. Reproduce it with
`make demo` on a local [`kind`](https://kind.sigs.k8s.io/) cluster, or read it off
the GitHub Actions run that gates every push. Full JSON reports live in
[`results/*.sample.json`](../results); rendered screenshots are in the
[README](../README.md#experiments--verdict).

---

## Resilience experiments (real runs on kind)

The harness drives **50 req/s** at a 3-replica `testapp` (behind a load-balanced
NodePort, backed by a Redis StatefulSet), injects a fault, and judges the fault
window against a declarative steady-state hypothesis. Four fault types across both
the **PASS** and **FAIL** paths:

| Fault | Baseline | Fault window | Recovery | Verdict |
|---|---|---|---|---|
| `pod-kill` | 100% ok · p95 10.2 ms | 750 req · 100% ok · p95 10.8 ms · p99 18.1 ms | 0.0 s | **PASS** |
| `node-drain` | 100% ok · p95 15.9 ms | 1247 req · 100% ok · p95 11.5 ms · p99 26.6 ms | 0.0 s | **PASS** |
| `resource-pressure` | 100% ok · p95 7.5 ms | 1249 req · 100% ok · p95 10.0 ms · **p99 52.7 ms** | 0.0 s | **PASS** |
| `dependency-partition` | 100% ok · p95 8.2 ms | 1000 req · **0.6% ok** · 994 failed | did not recover | **FAIL** *(by design)* |

**Reading the table**

- The first three are faults the system is *built to survive* — killing a pod,
  draining a node, and CPU contention all stay within steady state because the
  load-balanced Service absorbs them. Note `resource-pressure`'s p99 climbing to
  **52.7 ms**: the contention is real and visible in the tail, but p95 stays well
  under the ceiling, so the verdict is an honest PASS.
- `dependency-partition` cuts testapp off from Redis. Success collapses to
  **0.6%**, the system never returns to steady state, and the harness emits
  **FAIL** and **exits non-zero** — proving the gate actually fires rather than
  only ever printing PASS. CI runs this one as a **negative test**: the build fails
  unless the harness correctly fails.

*(For the partition, the latency percentiles reflect only the handful of requests
that completed; the meaningful signal there is the success rate, 0.6%.)*

---

## Code quality (measured)

| Metric | Value |
|---|---|
| Total statement coverage | **64.5%** — `go test -race -covermode=atomic -coverprofile ./...` |
| Core-logic coverage | report **89%** · inject **87%** · metrics **84%** · experiment **77%** · logger **100%** |
| Unit tests | **55** test functions, all run under the **race detector** (`-race`) |
| Static analysis | `golangci-lint` **0 issues** · `go vet` clean · `go build` clean |
| CI | GitHub Actions **green on every push** — lint + unit *and* a full kind integration job (deploy → 4 experiments → negative test) |

The uncovered code is deliberate, not neglected: `internal/k8s` (the client-go
clientset factory) and `internal/buildinfo` (version metadata) are thin glue, and
the uncovered parts of `probe` / `cmd` / `testapp` are the HTTP and filesystem
boundaries — all exercised by the integration layer that runs against a **real
cluster in CI**, not by unit tests. The pure logic (verdict, percentiles, recovery
detection, experiment validation) is unit-tested at 77–100%.

---

## Reproduce it yourself

```bash
make demo        # kind up -> build+load image -> deploy -> baseline -> pod-kill experiment
make test        # unit tests under -race
make lint        # golangci-lint

# any single experiment (exits non-zero if the steady-state hypothesis is violated):
go run ./cmd/harness run -experiment experiments/dependency-partition.yaml
```

**Toolchain:** Go 1.26 · client-go v0.34.1 · kind v0.30.0 (1 control-plane + 2
workers, Kubernetes v1.34.0). Every experiment above is re-run in CI on each push,
so these results are continuously reproduced, not a one-off.

---

## What this demonstrates

- **Fault injection with `client-go`** against the real Kubernetes API — pod
  deletion, the Eviction API, node cordon, and StatefulSet scaling — unit-tested
  with a fake clientset and verified end-to-end on kind.
- **Steady-state hypothesis testing** — declarative experiments, windowed
  baseline/fault metrics, recovery-time measurement, and a deterministic PASS/FAIL
  verdict wired into CI as a gate.
- **Testing discipline** — logic is separated from IO so the logic is unit-tested
  (race-clean) while the IO is integration-tested; a **negative test** proves the
  gate fails when it should, which is the part most resilience demos skip.

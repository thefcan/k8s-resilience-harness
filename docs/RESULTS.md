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
| `pod-kill` | 100% ok · p95 6.1 ms | 749 req · 100% ok · p95 7.4 ms · p99 15.2 ms | 0.0 s | **PASS** |
| `node-drain` | 100% ok · p95 6.8 ms | 1249 req · 100% ok · p95 7.0 ms · p99 11.2 ms | 0.0 s | **PASS** |
| `resource-pressure` | 100% ok · p95 8.1 ms | 1249 req · 100% ok · p95 8.7 ms · **max 136 ms** | 0.0 s | **PASS** |
| `dependency-partition` | 100% ok · p95 6.8 ms | 1000 req · **0.7% ok** · 993 failed | did not recover | **FAIL** *(by design)* |

**Reading the table**

- The first three are faults the system is *built to survive* — killing a pod,
  draining a node, and CPU contention all stay within steady state because the
  load-balanced Service absorbs them. `resource-pressure`'s tail widens (max
  climbs to ~136 ms) under the contention, but p95 stays well under the ceiling,
  so the verdict is an honest PASS.
- `dependency-partition` cuts testapp off from Redis. Success collapses to
  **0.7%**, the system never returns to steady state, and the harness emits
  **FAIL** and **exits non-zero** — proving the gate actually fires rather than
  only ever printing PASS. CI runs this one as a **negative test**: the build fails
  unless the harness correctly fails.

---

## Server-side cross-check (M4 — Prometheus)

Every run also queries an in-cluster **Prometheus** for the *server-side* view of
the fault window — independent of the harness's own client-side probe. The two
views corroborate each other (and, for the partition, agree on the failure):

| Fault | targets up | requests served | 5xx | min `redis_up` | client verdict |
|---|---|---|---|---|---|
| `pod-kill` | 3 / 3 | 741 | 0 | 1 | PASS — server clean |
| `node-drain` | 3 / 3 | 1015 | 0 | 1 | PASS — server clean |
| `resource-pressure` | 3 / 3 | 1113 | 0 | 1 | PASS — server clean |
| `dependency-partition` | 3 / 3 | 907 | **887** | **0** | FAIL — Redis gone, app still up |

**Why this matters**

- `requests served` runs higher than the probe's client-side count because
  Prometheus counts *every* route each pod handles — `/work` plus the `/healthz`
  readiness probes and its own `/metrics` scrapes — while the probe only counts
  its `/work` calls. Both views agree on what matters: **0 server-side 5xx** on
  all three PASS runs.
- For the partition, the server-side view **pinpoints the failure**:
  `min redis_up = 0` (a testapp pod saw its Redis dependency vanish) while
  `targets up = 3` (testapp itself never crashed). That is a *dependency* outage,
  not an app crash — exactly the root-cause signal a resilience test should
  surface, and it independently confirms the client-side FAIL.

---

## Code quality (measured)

| Metric | Value |
|---|---|
| Total statement coverage | **65.4%** — `go test -race -covermode=atomic -coverprofile ./...` |
| Core-logic coverage | report **87%** · inject **87%** · metrics **84%** · experiment **77%** · prom **76%** · logger **100%** |
| Unit tests | **64** test functions, all run under the **race detector** (`-race`) |
| Static analysis | `golangci-lint` **0 issues** · `go vet` clean · `go build` clean |
| CI | GitHub Actions **green on every push** — lint + unit *and* a full kind integration job (deploy → Prometheus scrape check → 4 experiments → negative test) |

The uncovered code is deliberate, not neglected: `internal/k8s` (the client-go
clientset factory) and `internal/buildinfo` (version metadata) are thin glue, and
the uncovered parts of `probe` / `cmd` / `testapp` are the HTTP and filesystem
boundaries — all exercised by the integration layer that runs against a **real
cluster in CI**, not by unit tests. The pure logic (verdict, percentiles, recovery
detection, experiment validation, PromQL result parsing) is unit-tested at 76–100%.

---

## Reproduce it yourself

```bash
make demo        # kind up -> image -> deploy (incl. Prometheus) -> baseline -> pod-kill experiment
make test        # unit tests under -race
make lint        # golangci-lint

# any single experiment (exits non-zero if the steady-state hypothesis is violated):
go run ./cmd/harness run -experiment experiments/dependency-partition.yaml -prometheus http://localhost:30090
```

**Toolchain:** Go 1.26 · client-go v0.34.1 · Prometheus v3.6.0 · kind v0.30.0
(1 control-plane + 2 workers, Kubernetes v1.34.0). Every experiment above is
re-run in CI on each push, so these results are continuously reproduced, not a
one-off.

---

## What this demonstrates

- **Fault injection with `client-go`** against the real Kubernetes API — pod
  deletion, the Eviction API, node cordon, and StatefulSet scaling — unit-tested
  with a fake clientset and verified end-to-end on kind.
- **Steady-state hypothesis testing** — declarative experiments, windowed
  baseline/fault metrics, recovery-time measurement, and a deterministic PASS/FAIL
  verdict wired into CI as a gate.
- **Server-side corroboration** — an instrumented SUT (`/metrics`) scraped by an
  in-cluster Prometheus that the harness queries with PromQL, so every verdict is
  cross-checked against the cluster's own view of the fault window.
- **Testing discipline** — logic is separated from IO so the logic is unit-tested
  (race-clean) while the IO is integration-tested; a **negative test** proves the
  gate fails when it should, which is the part most resilience demos skip.

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

**Milestone M0 — skeleton + cluster bring-up — ✅ done.**

A reproducible local [`kind`](https://kind.sigs.k8s.io/) cluster that comes up
and tears down cleanly, a Go CLI skeleton with structured logging, linting, and
a green CI that brings the cluster up and down. Fault injection and verdicts
arrive in later milestones.

```text
M0  skeleton + kind cluster bring-up        ✅ done
M1  SUT + loadgen + baseline steady-state   ⬜ planned
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
# Bring up the local kind cluster (1 control-plane + 2 workers)
make cluster-up

# Build & run the CLI skeleton (M0: prints a startup banner)
make run

# Lint + unit tests with the race detector
make lint
make test

# Tear the cluster down
make cluster-down
```

Run `make help` to list all targets.

---

## Layout

```text
cmd/harness/          # CLI entrypoint
internal/
  buildinfo/          # version metadata (ldflags-stamped)
  logger/             # structured slog logger (text/json, levels)
scripts/
  kind-config.yaml    # kind cluster topology
  cluster-up.sh       # create/reuse the local cluster
  cluster-down.sh     # delete the local cluster
deploy/               # kustomize manifests              (M1+)
experiments/          # declarative experiment YAML       (M2+)
results/              # run outputs                        (M2+)
.github/workflows/    # CI: lint+test, kind up/down
```

## CI

GitHub Actions runs two jobs on every push/PR:

- **lint + unit tests** — `golangci-lint`, `go test -race ./...`, `go build`.
- **kind cluster bring-up / tear-down** — installs `kind`, runs the project's
  own `cluster-up.sh` / `cluster-down.sh` scripts against a real cluster.

## License

MIT — see [LICENSE](LICENSE).

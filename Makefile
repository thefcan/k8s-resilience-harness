# k8s-resilience-harness — developer entrypoints.
# Run `make` or `make help` to list targets.

CLUSTER_NAME ?= kresil
BIN          := bin/harness
GOPATH_BIN   := $(shell go env GOPATH)/bin
export PATH  := $(GOPATH_BIN):$(PATH)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the harness binary into bin/.
	go build -o $(BIN) ./cmd/harness

.PHONY: run
run: ## Run the harness (M0: prints startup banner).
	go run ./cmd/harness

.PHONY: test
test: ## Run unit tests with the race detector.
	go test -race ./...

.PHONY: lint
lint: ## Run golangci-lint.
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum.
	go mod tidy

.PHONY: cluster-up
cluster-up: ## Create (or reuse) the local kind cluster.
	bash scripts/cluster-up.sh

.PHONY: cluster-down
cluster-down: ## Delete the local kind cluster.
	bash scripts/cluster-down.sh

.PHONY: images
images: ## Build the testapp image and load it into kind.
	bash scripts/build-images.sh

.PHONY: deploy
deploy: ## Apply manifests and wait for Redis + testapp rollout.
	bash scripts/deploy.sh

.PHONY: baseline
baseline: ## Port-forward + run loadgen, write results/baseline.json.
	bash scripts/baseline.sh

.PHONY: demo
demo: cluster-up images deploy baseline ## One command: cluster -> image -> deploy -> baseline.

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf bin/

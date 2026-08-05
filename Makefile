# cmiyc-pool Makefile
#
# Run `make` or `make help` to list targets. Every target is documented with a
# trailing `## ...` comment which the help target parses.

BINARY      := cmiyc
PKG         := ./cmd/cmiyc
BIN_DIR     := bin
IMAGE       := cmiyc-pool
IMAGE_TAG   ?= latest
GO          ?= go

# CGO off everywhere so local builds match the distroless container.
export CGO_ENABLED := 0

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Resolve deps and write go.sum (needs network; run this first)
	$(GO) mod tidy

.PHONY: deps-upgrade
deps-upgrade: ## Upgrade all Go dependencies to latest and re-tidy (needs network)
	$(GO) get -u ./...
	$(GO) mod tidy

.PHONY: build
build: ## Build the binary to ./bin/cmiyc
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="-s -w" -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: run
run: ## Run the pool server locally (needs DATABASE_URL and POOL_TOKEN)
	$(GO) run $(PKG) serve

.PHONY: fmt
fmt: ## Format all Go source
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet static checks
	$(GO) vet ./...

.PHONY: test
test: ## Run unit tests
	$(GO) test ./...

.PHONY: check
check: fmt vet test ## Format, vet, and test in one pass

.PHONY: docker
docker: ## Build the container image
	docker build -t $(IMAGE):$(IMAGE_TAG) .

.PHONY: docker-run
docker-run: ## Run the container locally (expects DATABASE_URL and POOL_TOKEN in env)
	docker run --rm -p 8080:8080 \
		-e PORT=8080 \
		-e DATABASE_URL="$(DATABASE_URL)" \
		-e POOL_TOKEN="$(POOL_TOKEN)" \
		$(IMAGE):$(IMAGE_TAG)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

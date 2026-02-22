GO ?= go
BUF ?= buf
BIN_DIR ?= bin

.PHONY: all
all: build

##@ General
.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: generate
generate: ## Generate proto code (buf dep update + buf generate)
	$(BUF) dep update
	$(BUF) generate

##@ Build
.PHONY: build
build: generate ## Build build-api binary
	$(GO) build -o $(BIN_DIR)/build-api ./cmd/server

.PHONY: run
run: build ## Build and run server
	./$(BIN_DIR)/build-api --grpc-addr=:9090 --http-addr=:8080

##@ Database
.PHONY: migrate-up
migrate-up: ## Run DB migrations up (requires DATABASE_URL)
	migrate -path migrations -database "$${DATABASE_URL}" up

.PHONY: migrate-down
migrate-down: ## Run DB migrations down (requires DATABASE_URL)
	migrate -path migrations -database "$${DATABASE_URL}" down

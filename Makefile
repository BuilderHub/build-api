# Build API - gRPC + HTTP gateway

GO ?= go
BIN ?= bin/server

.PHONY: build run migrate-up migrate-down generate

build: ## Build server binary
	$(GO) build -o $(BIN) ./cmd/server

# Generate Go and gRPC gateway code from .proto.
# Prefer buf from nix dev shell; falls back to Docker if buf not in PATH.
# Run from build-api: make generate. Proto root is api/proto; output is api/gen.
generate:
	@if command -v buf >/dev/null 2>&1; then \
		buf dep update api/proto && buf generate --template buf.gen.yaml api/proto; \
	else \
		echo "buf not in PATH (run 'nix develop' for buf), using Docker..."; \
		docker run --rm -v "$$(pwd):/workspace" -w /workspace bufbuild/buf:latest dep update api/proto && \
		docker run --rm -v "$$(pwd):/workspace" -w /workspace bufbuild/buf:latest generate --template buf.gen.yaml api/proto; \
	fi

run: build ## Run server (requires DATABASE_URL, JWT_SECRET)
	./$(BIN)

migrate-up: ## Run database migrations up
	migrate -path migrations -database "$${DATABASE_URL}" up

migrate-down: ## Run database migrations down
	migrate -path migrations -database "$${DATABASE_URL}" down

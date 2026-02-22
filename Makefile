# Build API - gRPC + HTTP gateway

GO ?= go
BIN ?= bin/server

.PHONY: build run migrate-up migrate-down

build: ## Build server binary
	$(GO) build -o $(BIN) ./cmd/server

run: build ## Run server (requires DATABASE_URL, JWT_SECRET)
	./$(BIN)

migrate-up: ## Run database migrations up
	migrate -path migrations -database "$${DATABASE_URL}" up

migrate-down: ## Run database migrations down
	migrate -path migrations -database "$${DATABASE_URL}" down

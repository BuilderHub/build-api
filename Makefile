GO ?= go
BUF ?= buf
BIN_DIR ?= bin

.PHONY: generate
generate:
	$(BUF) mod update
	$(BUF) generate

.PHONY: build
build: generate
	$(GO) build -o $(BIN_DIR)/build-api ./cmd/server

.PHONY: run
run: build
	./$(BIN_DIR)/build-api --grpc-addr=:9090 --http-addr=:8080

.PHONY: migrate-up
migrate-up:
	migrate -path migrations -database "$${DATABASE_URL}" up

.PHONY: migrate-down
migrate-down:
	migrate -path migrations -database "$${DATABASE_URL}" down

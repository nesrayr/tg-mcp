BIN := bin/tg-mcp

.PHONY: build install test lint generate

build:
	go build -o $(BIN) ./cmd/tg-mcp

install:
	go install ./cmd/tg-mcp

test:
	go test -race ./...
	go test -run Test_MemoryBound ./internal/office/

lint:
	go vet ./...
	go tool staticcheck ./...

generate:
	go generate ./...

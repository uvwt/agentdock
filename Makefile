.PHONY: fmt test build docker

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

build:
	go build -trimpath ./cmd/coding-tools-mcp

docker:
	docker compose build --no-cache


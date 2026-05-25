.PHONY: fmt test vet build check run docker-build docker-up docker-down logs clean

APP := agentdock
IMAGE := agentdock:local
WORKSPACE ?= /workspace
HOST ?= 127.0.0.1
PORT ?= 8765
LOG_LEVEL ?= info

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -trimpath -o ./bin/$(APP) ./cmd/agentdock

check: fmt test vet build

run:
	go run ./cmd/agentdock --workspace $(WORKSPACE) --host $(HOST) --port $(PORT) --log-level $(LOG_LEVEL)

docker-build:
	docker build -t $(IMAGE) .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

logs:
	docker compose logs -f

clean:
	rm -rf ./bin

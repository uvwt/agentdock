.PHONY: fmt test vet race build check run docker-build docker-browser-build docker-up docker-down smoke-docker logs clean install-macos restart-macos smoke-macos

APP := agentdock
IMAGE := agentdock:local
BROWSER_IMAGE := agentdock:browser
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

race:
	go test -race ./internal/session ./internal/tools

build:
	go build -trimpath -o ./bin/$(APP) ./cmd/agentdock

check: fmt test vet build

run:
	go run ./cmd/agentdock --workspace $(WORKSPACE) --host $(HOST) --port $(PORT) --log-level $(LOG_LEVEL)

install-macos:
	./scripts/install-macos.sh

restart-macos:
	./scripts/restart-macos.sh

smoke-macos:
	./scripts/smoke-desktop-macos.sh

docker-build:
	docker build -t $(IMAGE) .

docker-browser-build:
	docker build -f Dockerfile.browser -t $(BROWSER_IMAGE) .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

smoke-docker:
	./scripts/smoke-docker.sh

logs:
	docker compose logs -f

clean:
	rm -rf ./bin

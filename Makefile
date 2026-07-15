.PHONY: fmt test vet race build check run docker-build docker-dev-build docker-browser-build docker-up docker-browser-up docker-down smoke-docker logs clean clean-local-artifacts install-linux install-macos test-install-macos deploy-macos-source restart-macos smoke-macos

APP := agentdock
IMAGE := agentdock:local
DEV_IMAGE := agentdock:dev
BROWSER_IMAGE := agentdock:browser
HOST ?= 127.0.0.1
PORT ?= 8765
LOG_LEVEL ?= info
BUILD_COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_LDFLAGS := -X github.com/uvwt/agentdock/internal/buildinfo.Commit=$(BUILD_COMMIT) -X github.com/uvwt/agentdock/internal/buildinfo.BuildDate=$(BUILD_DATE)

fmt:
	gofmt -w ./cmd ./internal

test:
	go test ./...

vet:
	go vet ./...

race:
	go test -race ./...

build:
	go build -trimpath -ldflags "$(BUILD_LDFLAGS)" -o ./bin/$(APP) ./cmd/agentdock

check: fmt test vet build

run:
	go run ./cmd/agentdock --host $(HOST) --port $(PORT) --log-level $(LOG_LEVEL)

install-linux:
	./scripts/install-linux.sh

install-macos:
	./scripts/install-macos.sh

test-install-macos:
	./scripts/test-install-macos.sh

deploy-macos-source:
	./scripts/deploy-macos-source.sh

restart-macos:
	./scripts/restart-macos.sh

smoke-macos:
	printf '%s\n' '{"skill_action":"status","check_screenshot":false,"check_applescript":true}' | ./skill-sources/desktop/run.py

docker-build:
	docker build --target runtime -t $(IMAGE) .

docker-dev-build:
	docker build --target dev -t $(DEV_IMAGE) .

docker-browser-build:
	docker build --target browser -t $(BROWSER_IMAGE) .

docker-up:
	AGENTDOCK_IMAGE=$(IMAGE) docker compose up -d

docker-browser-up:
	AGENTDOCK_IMAGE=$(IMAGE) AGENTDOCK_BROWSER_IMAGE=$(BROWSER_IMAGE) docker compose -f docker-compose.yml -f docker-compose.browser.yml up -d

docker-down:
	docker compose down

smoke-docker:
	./scripts/smoke-docker.sh

logs:
	docker compose logs -f

clean:
	rm -rf ./bin

clean-local-artifacts:
	@printf 'cleaning ignored local AgentDock artifacts\n'
	@rm -f ./agentdock.new ./agentdock.new.* ./agentdock.prev ./agentdock.prev.* ./agentdock.bak* ./agentdock.killed*
	@rm -rf ./bin ./coverage.out

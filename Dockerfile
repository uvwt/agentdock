# syntax=docker/dockerfile:1.7

FROM golang:1.26.5-bookworm@sha256:18aedc16aa19b3fd7ded7245fc14b109e054d65d22ed53c355c899582bbb2113 AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agentdock ./cmd/agentdock

FROM node:22.17.0-bookworm-slim@sha256:b04ce4ae4e95b522112c2e5c52f781471a5cbc3b594527bcddedee9bc48c03a0 AS runtime-base

ARG AGENTDOCK_UID=10001
ARG AGENTDOCK_GID=10001

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      fd-find \
      git \
      jq \
      make \
      openssh-client \
      python3 \
      python3-pip \
      ripgrep \
      tar \
      tini \
      unzip \
      wget \
    && npm install --global pnpm@10.28.0 \
    && ln -s /usr/bin/fdfind /usr/local/bin/fd \
    && groupadd --gid "${AGENTDOCK_GID}" agentdock \
    && useradd --uid "${AGENTDOCK_UID}" --gid "${AGENTDOCK_GID}" --create-home --home-dir /home/agentdock --shell /bin/bash agentdock \
    && install -d -o agentdock -g agentdock -m 0700 \
      /home/agentdock/.agentdock \
      /home/agentdock/.agentdock/browser-artifacts \
      /home/agentdock/.agentdock/tmp \
      /home/agentdock/AgentDock \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build --chmod=0755 /out/agentdock /usr/local/bin/agentdock
COPY --chmod=0755 docker-entrypoint.sh /usr/local/bin/agentdock-entrypoint
COPY --chmod=0755 scripts/docker-healthcheck.sh /usr/local/bin/agentdock-healthcheck

ENV HOME=/home/agentdock \
    AGENTDOCK_HOST=0.0.0.0 \
    AGENTDOCK_PORT=8765

USER agentdock:agentdock
WORKDIR /home/agentdock/AgentDock
EXPOSE 8765

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["agentdock-healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "agentdock-entrypoint"]
CMD ["agentdock"]

FROM runtime-base AS dev

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential \
      pkg-config \
    && install -d -o agentdock -g agentdock /home/agentdock/go \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /usr/local/go /usr/local/go

ENV PATH=/usr/local/go/bin:${PATH} \
    GOPATH=/home/agentdock/go

USER agentdock:agentdock

FROM runtime-base AS browser

USER root
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      chromium \
      fonts-liberation \
      fonts-noto-cjk \
    && rm -rf /var/lib/apt/lists/*

COPY examples/browser-runner/package.json examples/browser-runner/package-lock.json /opt/agentdock/browser-runner/
RUN cd /opt/agentdock/browser-runner \
    && npm ci --omit=dev --ignore-scripts \
    && npm cache clean --force
COPY --chmod=0644 examples/browser-runner/browser-runner.js /opt/agentdock/browser-runner/browser-runner.js

ENV AGENTDOCK_BROWSER_ENABLED=true \
    AGENTDOCK_BROWSER_RUNNER_DIR=/opt/agentdock/browser-runner \
    AGENTDOCK_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium

USER agentdock:agentdock

FROM runtime-base AS runtime

FROM golang:1.26.5-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/agentdock ./cmd/agentdock

FROM golang:1.26.5-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    bash \
    ca-certificates \
    curl \
    fd-find \
    git \
    jq \
    make \
    nodejs \
    npm \
    openssh-client \
    python3 \
    python3-pip \
    ripgrep \
    tar \
    unzip \
    wget \
    && rm -rf /var/lib/apt/lists/*

RUN npm install -g pnpm@10.28.0

COPY --from=build /out/agentdock /usr/local/bin/agentdock
COPY docker-entrypoint.sh /usr/local/bin/agentdock-entrypoint

WORKDIR /workspace
EXPOSE 8765
ENTRYPOINT ["agentdock-entrypoint"]

CMD ["agentdock", "--host", "0.0.0.0", "--port", "8765"]

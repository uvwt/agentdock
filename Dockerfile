FROM golang:1.22-bookworm AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/coding-tools-mcp ./cmd/coding-tools-mcp

FROM golang:1.22-bookworm

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

RUN npm install -g corepack \
    && corepack enable \
    && corepack prepare pnpm@latest --activate

COPY --from=build /out/coding-tools-mcp /usr/local/bin/coding-tools-mcp

WORKDIR /workspace
EXPOSE 8765

CMD ["coding-tools-mcp", "--workspace", "/workspace", "--host", "0.0.0.0", "--port", "8765"]


FROM golang:1.22-bookworm AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/coding-tools-mcp ./cmd/coding-tools-mcp

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git ripgrep fd-find \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/coding-tools-mcp /usr/local/bin/coding-tools-mcp

WORKDIR /workspace
EXPOSE 8765

CMD ["coding-tools-mcp", "--workspace", "/workspace", "--host", "0.0.0.0", "--port", "8765"]


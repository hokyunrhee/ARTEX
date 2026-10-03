# syntax=docker/dockerfile:1
#
# Runtime image: install common tools and copy a prebuilt Linux binary; do not compile here.
# The CI binaries job cross-compiles it with pure Go, without QEMU, and places each
# architecture at dist/<TARGETARCH>/artex in the build context. Multi-architecture builds
# then emulate only the apt layer on arm64, avoiding slow emulated Next.js and Go builds.
#
# Before building the image locally, prepare the binary:
#   cd web && npm run build:static && cd ..
#   cp -r web/out server/webui/dist
#   CGO_ENABLED=0 GOARCH=amd64 go build -tags embedui -o dist/amd64/artex ./cmd/artex
#   docker build -t artex:local .
FROM python:3.12-slim-bookworm
ARG TARGETARCH
# Common tools: ripgrep / curl / vim and frequently used reconnaissance tools; adjust as needed.
# Install Node.js 20.x from NodeSource: bookworm provides Node.js 18, but Playwright requires >=20.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
# Install Playwright MCP and CLI globally so runtime npx calls need no downloads.
# @playwright/mcp: run browser MCP with `npx @playwright/mcp`; no -y/@latest is needed.
# @playwright/cli provides playwright-cli; verify it with --help after installation.
# Install playwright for browser management and use --with-deps to preload Chromium
# and system dependencies so MCP/CLI can start immediately without downloading a browser.
RUN npm install -g @playwright/mcp@latest @playwright/cli@latest playwright@latest \
    && playwright-cli --help \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# Prebuilt binary for the target architecture (dist/amd64/artex or dist/arm64/artex).
COPY dist/${TARGETARCH}/artex /app/artex
# The supervisor restarts based on the exit code so UI updates can replace the binary.
# It also forwards SIGTERM to artex: docker stop signals only PID 1. Without forwarding,
# artex cannot shut down gracefully and is killed with SIGKILL after 10 seconds.
COPY start.sh /app/start.sh
RUN chmod +x /app/artex /app/start.sh
COPY skills/ /app/skills/
# Persistent data/ directory (SQLite and jwt.key).
VOLUME ["/app/data"]
EXPOSE 8787 8788
ENTRYPOINT ["/app/start.sh"]
CMD ["-addr", ":8787", "-proxy", ":8788"]

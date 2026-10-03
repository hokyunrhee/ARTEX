#!/usr/bin/env bash
# ARTEX updater: 1) pull and recreate Docker containers; 2) rebuild the local binary.
# Complements install.sh: install handles the initial deployment; update upgrades it.
# Database migrations run automatically: artex reruns schema.sql idempotently at startup
# (including ADD COLUMN/CREATE INDEX IF NOT EXISTS). The pgdata volume, ./data, and ./skills are preserved.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }

# -- Optional: update repository code, Compose configuration, and build scripts --
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { warn "Not a Git checkout; skipping git pull"; return; }
  [ "$(ask 'Fetch the latest code (git pull --ff-only)? (y/n)' y)" = y ] || return
  if ! git pull --ff-only; then
    warn "git pull could not fast-forward (local changes or diverged branches); resolve this manually and retry. Continuing with the current code."
  fi
}

# -- 1) Docker update --
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
    || die "docker / docker compose not found; deploy with ./install.sh first"
  [ -f .env ] || die ".env not found; run ./install.sh for the initial deployment"

  # Optionally select an image tag; otherwise keep ARTEX_TAG from .env, defaulting to latest.
  local tag; tag="$(ask 'Target image tag (Enter keeps .env / latest)' '')"
  if [ -n "$tag" ]; then
    if grep -q '^ARTEX_TAG=' .env; then
      sed -i.bak "s|^ARTEX_TAG=.*|ARTEX_TAG=${tag}|" .env && rm -f .env.bak
    else
      printf '\nARTEX_TAG=%s\n' "$tag" >> .env
    fi
    ok "Set ARTEX_TAG to ${tag}"
  fi

  # Update only artex: postgres is pinned to 16-alpine and does not need an update.
  # Pulling it wastes bandwidth, and major-version changes can break compatibility.
  # depends_on starts postgres when needed; an existing postgres container is kept as is.
  info "Pulling the new artex image..."
  docker compose pull artex
  info "Recreating and starting artex (schema migrations run on startup)..."
  docker compose up -d artex
  ok "Update complete -> http://localhost:8787"
  info "View logs: docker compose logs -f artex"
  info "Remove old images (optional): docker image prune -f"
}

# -- 2) Local build update --
update_local(){
  command -v go >/dev/null 2>&1 || die "Go >=1.26 not found: https://go.dev/dl/"
  [ -f config.json ] || warn "config.json not found; use ./install.sh for an initial deployment"
  ok "Go: $(go version)"

  if command -v npm >/dev/null 2>&1; then
    info "Rebuilding frontend static assets..."
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "Rebuilding the binary with the embedded frontend..."
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm not found: building the backend without an embedded frontend (run npm run dev separately)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "Build complete -> ./artex"
  warn "Restart the running artex process to apply this build (schema migrations run on startup)"
}

echo "=============================="
echo "  ARTEX update"
echo "  1) Docker update (pull the image and recreate containers)"
echo "  2) Local update (rebuild with Go)"
echo "=============================="
case "$(ask 'Select' 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "Invalid selection" ;;
esac

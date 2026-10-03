#!/usr/bin/env bash
# ARTEX update script: 1) Docker update (pull new image and rebuild)  2) local build update (rebuild the binary)
# Mirrors install.sh: install handles the first-time setup, update handles upgrading to a new version.
# DB migrations need no manual step -- artex idempotently re-runs schema.sql (with ADD COLUMN/CREATE
# INDEX IF NOT EXISTS) on every startup, so "restart means migrate." Data (the pgdata volume, ./data, ./skills) is unaffected.
set -euo pipefail
cd "$(cd "$(dirname "$0")" && pwd)"

info(){ printf '\033[36m[*]\033[0m %s\n' "$*"; }
ok(){   printf '\033[32m[+]\033[0m %s\n' "$*"; }
warn(){ printf '\033[33m[!]\033[0m %s\n' "$*"; }
die(){  printf '\033[31m[x]\033[0m %s\n' "$*" >&2; exit 1; }
ask(){  local p="$1" d="${2:-}" a; read -rp "$p${d:+ [$d]}: " a; echo "${a:-$d}"; }

# ── Optional: sync the repo to the latest code (compose/scripts/local build sources all update through it) ───────
sync_repo(){
  [ -d .git ] && command -v git >/dev/null 2>&1 || { warn "Not a git working copy, skipping git pull"; return; }
  [ "$(ask 'Pull the latest code (git pull --ff-only)? (y/n)' y)" = y ] || return
  if ! git pull --ff-only; then
    warn "git pull could not fast-forward (local changes or a diverged branch) -- please resolve it by hand and retry; keeping the current code for now"
  fi
}

# ── 1) Docker update ───────────────────────────────
update_docker(){
  command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1 \
    || die "docker / docker compose not detected, please deploy first with ./install.sh"
  [ -f .env ] || die ".env not found, please run ./install.sh to complete the first-time deployment"

  # Optional: upgrade to a specific version tag (leave blank to keep ARTEX_TAG from .env, which defaults to latest)
  local tag; tag="$(ask 'Target image tag (press Enter to keep .env / latest)' '')"
  if [ -n "$tag" ]; then
    if grep -q '^ARTEX_TAG=' .env; then
      sed -i.bak "s|^ARTEX_TAG=.*|ARTEX_TAG=${tag}|" .env && rm -f .env.bak
    else
      printf '\nARTEX_TAG=%s\n' "$tag" >> .env
    fi
    ok "Set ARTEX_TAG to ${tag}"
  fi

  # Only touch artex: postgres is pinned to 16-alpine and does not need to be upgraded along
  # with it (pulling it just wastes bandwidth, and a major version change also carries
  # compatibility risk). artex declares depends_on postgres, so when up is run with the
  # service name, pg is started automatically if it is not already running; if it is already
  # running it is left as-is and not rebuilt.
  info "Pulling the new image (artex only)…"
  docker compose pull artex
  info "Rebuilding and starting (artex auto-migrates the schema on restart)…"
  docker compose up -d artex
  ok "Update complete → http://localhost:8787"
  info "View logs: docker compose logs -f artex"
  info "Clean up old images (optional): docker image prune -f"
}

# ── 2) local build update ──────────────────────────────
update_local(){
  command -v go >/dev/null 2>&1 || die "Go not detected (>=1.26): https://go.dev/dl/"
  [ -f config.json ] || warn "config.json not found -- for a first-time deployment use ./install.sh instead"
  ok "Go: $(go version)"

  if command -v npm >/dev/null 2>&1; then
    info "Rebuilding frontend static assets…"
    ( cd web && npm ci && npm run build:static )
    rm -rf server/webui/dist && cp -r web/out server/webui/dist
    info "Recompiling the single embedded binary…"
    CGO_ENABLED=0 go build -tags embedui -trimpath -o artex ./cmd/artex
  else
    warn "npm not detected: building the backend **without an embedded frontend** (run the frontend separately with npm run dev)"
    CGO_ENABLED=0 go build -o artex ./cmd/artex
  fi
  ok "Build complete → ./artex"
  warn "Please restart the running artex process to take effect (the schema is auto-migrated on restart)"
}

echo "=============================="
echo "  ARTEX update"
echo "  1) Docker update (pull new image and rebuild)"
echo "  2) Local update (go recompile)"
echo "=============================="
case "$(ask 'Choice' 1)" in
  1) sync_repo; update_docker ;;
  2) sync_repo; update_local ;;
  *) die "Invalid choice" ;;
esac

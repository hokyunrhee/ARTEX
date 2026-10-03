#!/usr/bin/env bash
# Development: run the backend (:8787), traffic proxy (:8788), and Next.js frontend (:5173).
# The frontend proxies /api to the backend; Ctrl-C stops both processes.
#
# For a single binary with the frontend embedded, see the corresponding README section.
set -euo pipefail
cd "$(dirname "$0")"

# Stop all child processes in this process group (backend and frontend) on exit.
cleanup() { kill 0 2>/dev/null || true; }
trap cleanup EXIT INT TERM

# Backend (plain go run, without embedded UI); set worker concurrency in System settings.
go run ./cmd/artex -addr :8787 -proxy 127.0.0.1:8788 &

# Frontend with hot reload (Vite/Next dev server; /api proxies to :8787).
( cd web && npm run dev ) &

echo "[dev] Backend :8787 / proxy :8788 / frontend http://localhost:5173  (Ctrl-C to stop)"
wait

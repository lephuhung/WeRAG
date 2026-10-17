#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/frontend-next"

echo "[verify_frontend_pr] npm run typecheck"
npm run typecheck

echo "[verify_frontend_pr] npm run build"
WERAG_BACKEND_URL="${WERAG_BACKEND_URL:-http://127.0.0.1:8080}" npm run build

echo "[verify_frontend_pr] OK"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SERVER_DIR="$REPO_ROOT/server"

API_PORT="${E2E_API_PORT:-18080}"
E2E_DSN="${TIANSHU_E2E_DSN:-postgres://tianshu:tianshu@127.0.0.1:5432/tianshu_e2e?sslmode=disable}"
UPLOAD_DIR="${E2E_UPLOAD_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/tianshu-e2e-uploads.XXXXXX")}"
SERVER_BIN="${TMPDIR:-/tmp}/tianshu-e2e-server"

if [[ "${E2E_RESET_DB:-1}" == "1" ]]; then
  if [[ "$E2E_DSN" != *e2e* && "$E2E_DSN" != *test* && "${E2E_ALLOW_RESET:-0}" != "1" ]]; then
    echo "refusing to reset a non-E2E database; set E2E_ALLOW_RESET=1 to override" >&2
    exit 1
  fi
  if command -v psql >/dev/null 2>&1; then
    psql "$E2E_DSN" -v ON_ERROR_STOP=1 -q \
      -c 'DROP SCHEMA IF EXISTS public CASCADE' \
      -c 'CREATE SCHEMA public'
  else
    echo "psql not found; running E2E against the existing schema" >&2
  fi
fi

export TIANSHU_APP__ENV="development"
export TIANSHU_SERVER__ADDR="127.0.0.1:${API_PORT}"
export TIANSHU_DATABASE__DSN="$E2E_DSN"
export TIANSHU_DATABASE__AUTO_MIGRATE="true"
export TIANSHU_STORAGE__DRIVER="local"
export TIANSHU_STORAGE__LOCAL_ROOT="$UPLOAD_DIR"
export TIANSHU_AUTH__JWT_SECRET="e2e-only-jwt-secret-at-least-32-bytes"
export TIANSHU_BOOTSTRAP__ADMIN_USERNAME="${E2E_ADMIN_USERNAME:-e2e-admin}"
export TIANSHU_BOOTSTRAP__ADMIN_EMAIL="${E2E_ADMIN_EMAIL:-e2e-admin@example.com}"
export TIANSHU_BOOTSTRAP__ADMIN_DISPLAY_NAME="${E2E_ADMIN_DISPLAY_NAME:-E2E Admin}"
export TIANSHU_BOOTSTRAP__ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:-E2e-Password-123!}"

cd "$SERVER_DIR"
go build -o "$SERVER_BIN" ./cmd/server
exec "$SERVER_BIN"

#!/usr/bin/env bash
# Tianshu Code Review preflight helper
# Usage: bash .agents/skills/tianshu-review/scripts/preflight.sh [--base <ref>] [--mode scope|quick|full]

set -uo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT" || exit 1

BASE_REF=""
MODE="full"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base)
      BASE_REF="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--base <ref>] [--mode scope|quick|full]" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$BASE_REF" ]]; then
  if git rev-parse --verify origin/main >/dev/null 2>&1; then
    BASE_REF="origin/main"
  elif git rev-parse --verify main >/dev/null 2>&1; then
    BASE_REF="main"
  else
    BASE_REF="HEAD"
  fi
fi

CURRENT_BRANCH="$(git branch --show-current 2>/dev/null || echo 'DETACHED')"
MERGE_BASE="$(git merge-base HEAD "$BASE_REF" 2>/dev/null || git rev-parse HEAD)"

echo "=== [1/4] Review Scope & Branch Check ==="
echo "Repo Root      : $REPO_ROOT"
echo "Current Branch : $CURRENT_BRANCH"
echo "Base Ref       : $BASE_REF (merge-base: ${MERGE_BASE:0:8})"
echo "Mode           : $MODE"

EXIT_CODE=0

if [[ "$CURRENT_BRANCH" == "main" ]]; then
  echo "[FAIL] Branch redline: currently on 'main'. Feature branch is required per .trellis/spec/workflow/pr.md."
  EXIT_CODE=1
fi

COMMITTED_FILES="$(git diff --name-only "$MERGE_BASE"...HEAD 2>/dev/null || true)"
STAGED_FILES="$(git diff --cached --name-only 2>/dev/null || true)"
UNSTAGED_FILES="$(git diff --name-only 2>/dev/null || true)"
UNTRACKED_FILES="$(git ls-files --others --exclude-standard 2>/dev/null || true)"

ALL_CHANGED="$(printf "%s\n%s\n%s\n%s\n" "$COMMITTED_FILES" "$STAGED_FILES" "$UNSTAGED_FILES" "$UNTRACKED_FILES" | sed '/^$/d' | sort -u)"
CHANGED_COUNT="$(printf "%s\n" "$ALL_CHANGED" | sed '/^$/d' | wc -l | tr -d ' ')"

echo "Changed Files  : $CHANGED_COUNT"
if [[ "$CHANGED_COUNT" -gt 0 ]]; then
  printf "%s\n" "$ALL_CHANGED" | sed 's/^/  - /'
fi

echo ""
echo "=== [2/4] Repo Hygiene & Prohibited Files ==="
if ! git diff --check "$MERGE_BASE" >/dev/null 2>&1; then
  echo "[FAIL] git diff --check reported whitespace or conflict marker issues:"
  git diff --check "$MERGE_BASE" || true
  EXIT_CODE=1
else
  echo "[PASS] git diff --check clean"
fi

FORBIDDEN_HITS=""
while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  case "$file" in
    .env|.env.*|*.local.toml|config.toml|server/config.toml|server/configs/config.toml|*.pem|*.key|*.crt|web/dist/*|.dsh/*)
      if [[ "$file" != ".env.example" ]]; then
        FORBIDDEN_HITS+="$file"$'\n'
      fi
      ;;
    server/internal/webui/dist/*)
      if [[ "$file" != "server/internal/webui/dist/.gitkeep" ]]; then
        FORBIDDEN_HITS+="$file"$'\n'
      fi
      ;;
  esac
done <<< "$ALL_CHANGED"

if [[ -n "$FORBIDDEN_HITS" ]]; then
  echo "[FAIL] Prohibited files detected in change set:"
  printf "%s" "$FORBIDDEN_HITS" | sed 's/^/  - /'
  EXIT_CODE=1
else
  echo "[PASS] No prohibited secret/build/runtime files in change set"
fi

HAS_BACKEND=0
HAS_SQL=0
HAS_FRONTEND=0
HAS_EMBED=0

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  [[ "$file" == server/* ]] && HAS_BACKEND=1
  [[ "$file" == server/migrations/* || "$file" == */queries.sql || "$file" == */dbgen/* ]] && HAS_SQL=1
  [[ "$file" == web/* ]] && HAS_FRONTEND=1
  [[ "$file" == server/internal/webui/* || "$file" == "Makefile" || "$file" == "server/Makefile" || "$file" == "server/Dockerfile" ]] && HAS_EMBED=1
done <<< "$ALL_CHANGED"

echo "Touched Domains: backend=$HAS_BACKEND sql=$HAS_SQL frontend=$HAS_FRONTEND embed=$HAS_EMBED"

if [[ "$MODE" == "scope" ]]; then
  exit "$EXIT_CODE"
fi

run_step() {
  local label="$1"
  shift
  echo "--> $label"
  if "$@"; then
    echo "[PASS] $label"
  else
    echo "[FAIL] $label"
    EXIT_CODE=1
  fi
}

echo ""
echo "=== [3/4] Backend Gates ==="
if [[ "$HAS_BACKEND" -eq 1 ]]; then
  UNFORMATTED="$(cd server && gofmt -l .)"
  if [[ -n "$UNFORMATTED" ]]; then
    echo "[FAIL] gofmt -l server:"
    printf "%s\n" "$UNFORMATTED" | sed 's/^/  - /'
    EXIT_CODE=1
  else
    echo "[PASS] gofmt -l server"
  fi

  run_step "go vet ./..." bash -c "cd server && go vet ./..."
  run_step "make -C server lint" make -C server lint
  if [[ "$MODE" == "full" ]]; then
    run_step "go test -race ./..." bash -c "cd server && go test -race ./..."
    run_step "go build ./..." bash -c "cd server && go build ./..."
  else
    run_step "go test ./..." bash -c "cd server && go test ./..."
  fi
else
  echo "[SKIP] No server/ changes detected"
fi

echo ""
echo "=== [4/4] Frontend & Embed Gates ==="
if [[ "$HAS_FRONTEND" -eq 1 ]]; then
  run_step "pnpm --dir web typecheck" pnpm --dir web typecheck
  run_step "pnpm --dir web lint" pnpm --dir web lint
  run_step "pnpm --dir web format:check" pnpm --dir web format:check
  if [[ "$MODE" == "full" ]]; then
    run_step "pnpm --dir web test" pnpm --dir web test
    run_step "pnpm --dir web build" pnpm --dir web build
  fi
else
  echo "[SKIP] No web/ changes detected"
fi

if [[ "$MODE" == "full" && "$HAS_EMBED" -eq 1 ]]; then
  run_step "make build (full web-embed + server binary)" make build
fi

exit "$EXIT_CODE"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

read_config_value() {
  local key="$1"
  local default_value="${2:-}"
  local value
  value="$(grep -E "^${key}=" .env.docker 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  value="${value%$'\r'}"
  value="$(printf '%s' "$value" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")"
  if [ -z "$value" ]; then
    printf '%s' "$default_value"
  else
    printf '%s' "$value"
  fi
}

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed." >&2
  exit 1
fi

if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE_CMD=(docker-compose)
else
  echo "docker compose is not available." >&2
  exit 1
fi

ENV_ARGS=()
if [ -f .env.docker ]; then
  ENV_ARGS=(--env-file .env.docker)
fi

MYSQL_MODE="$(read_config_value MYSQL_MODE builtin)"
REDIS_MODE="$(read_config_value REDIS_MODE builtin)"
COMPOSE_FILES=(-f docker-compose.yml)

if [ "$MYSQL_MODE" = "builtin" ]; then
  COMPOSE_FILES+=(-f docker-compose.mysql.yml)
fi
if [ "$REDIS_MODE" = "builtin" ]; then
  COMPOSE_FILES+=(-f docker-compose.redis.yml)
fi

"${COMPOSE_CMD[@]}" "${ENV_ARGS[@]}" "${COMPOSE_FILES[@]}" down



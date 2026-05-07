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

upsert_config_value() {
  local key="$1"
  local value="$2"
  local line="${key}=${value}"

  if grep -qE "^[[:space:]]*${key}=" .env.docker 2>/dev/null; then
    awk -v key="$key" -v line="$line" '
      BEGIN { updated = 0 }
      {
        if ($0 ~ "^[[:space:]]*" key "=") {
          if (!updated) {
            print line
            updated = 1
          }
        } else {
          print $0
        }
      }
      END {
        if (!updated) {
          print line
        }
      }
    ' .env.docker > .env.docker.tmp
    mv .env.docker.tmp .env.docker
  else
    printf '\n%s\n' "$line" >> .env.docker
  fi
}

generate_jwt_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
    return
  fi
  if [ -r /dev/urandom ]; then
    od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
    return
  fi
  echo "unable to generate JWT_SECRET: openssl and /dev/urandom unavailable" >&2
  exit 1
}

ensure_jwt_secret() {
  local current
  current="$(read_config_value JWT_SECRET '')"
  if [ -n "$current" ] && [ "$current" != "change-me-please" ]; then
    return
  fi

  local generated
  generated="$(generate_jwt_secret)"
  upsert_config_value JWT_SECRET "$generated"
  echo "generated JWT_SECRET in deploy/linux/.env.docker"
}

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is not installed. Run ./install-docker.sh or ./quickstart.sh first." >&2
  exit 1
fi

if docker compose version >/dev/null 2>&1; then
  COMPOSE_CMD=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE_CMD=(docker-compose)
else
  echo "docker compose is not available. Run ./install-docker.sh or ./quickstart.sh first." >&2
  exit 1
fi

if [ -d .env.docker ]; then
  echo ".env.docker exists but is a directory; remove or rename deploy/linux/.env.docker first." >&2
  exit 1
fi

if [ ! -f .env.docker ]; then
  cp .env.example .env.docker
  echo "created deploy/linux/.env.docker from template"
fi

ensure_jwt_secret

MYSQL_MODE="$(read_config_value MYSQL_MODE builtin)"
REDIS_MODE="$(read_config_value REDIS_MODE builtin)"
COMPOSE_FILES=(-f docker-compose.yml)

case "$MYSQL_MODE" in
  builtin)
    COMPOSE_FILES+=(-f docker-compose.mysql.yml)
    ;;
  external)
    :
    ;;
  *)
    echo "invalid MYSQL_MODE: $MYSQL_MODE (expected builtin or external)" >&2
    exit 1
    ;;
esac

case "$REDIS_MODE" in
  builtin)
    COMPOSE_FILES+=(-f docker-compose.redis.yml)
    ;;
  external)
    :
    ;;
  *)
    echo "invalid REDIS_MODE: $REDIS_MODE (expected builtin or external)" >&2
    exit 1
    ;;
esac

"${COMPOSE_CMD[@]}" --env-file .env.docker "${COMPOSE_FILES[@]}" up -d --build
"${COMPOSE_CMD[@]}" --env-file .env.docker "${COMPOSE_FILES[@]}" ps

echo
echo "R0RPC deployed."
echo "Compose command: ${COMPOSE_CMD[*]}"
echo "MySQL mode: $MYSQL_MODE"
echo "Redis mode: $REDIS_MODE"
echo "Database bootstrap: automatic inside container startup"
echo "UI: http://YOUR_SERVER_IP:$(read_config_value HTTP_EXPOSE_PORT 9876)"
if [ "$MYSQL_MODE" = "builtin" ]; then
  echo "MySQL 5.7 port: $(read_config_value MYSQL_EXPOSE_PORT 3306)"
else
  echo "MySQL external: $(read_config_value MYSQL_HOST):$(read_config_value MYSQL_PORT 3306)"
fi
if [ "$REDIS_MODE" = "builtin" ]; then
  echo "Redis port: $(read_config_value REDIS_EXPOSE_PORT 6379)"
else
  echo "Redis external: $(read_config_value REDIS_ADDR)"
fi

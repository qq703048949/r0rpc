#!/usr/bin/env sh
set -eu

CONFIG_FILE="/app/r0rpc.conf"

if [ ! -f "$CONFIG_FILE" ]; then
  echo "missing config file: $CONFIG_FILE" >&2
  exit 1
fi

read_config_value() {
  key="$1"
  default_value="${2:-}"
  value="$(grep -E "^${key}=" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | cut -d= -f2- || true)"
  value=$(printf '%s' "$value" | tr -d '\r' | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//")
  if [ -z "$value" ]; then
    printf '%s' "$default_value"
  else
    printf '%s' "$value"
  fi
}

wait_for_tcp() {
  host="$1"
  port="$2"
  name="$3"
  attempt=0

  until nc -z "$host" "$port" >/dev/null 2>&1; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
      echo "timeout waiting for $name at $host:$port" >&2
      exit 1
    fi
    echo "waiting for $name at $host:$port..."
    sleep 2
  done
}

MYSQL_HOST="$(read_config_value MYSQL_HOST mysql)"
MYSQL_PORT="$(read_config_value MYSQL_PORT 3306)"
REDIS_ADDR="$(read_config_value REDIS_ADDR '')"
REDIS_HOST="${REDIS_ADDR%:*}"
REDIS_PORT="${REDIS_ADDR##*:}"

wait_for_tcp "$MYSQL_HOST" "$MYSQL_PORT" "mysql"

if [ -n "$REDIS_ADDR" ]; then
  if [ "$REDIS_HOST" = "$REDIS_ADDR" ]; then
    REDIS_PORT="6379"
  fi
  wait_for_tcp "$REDIS_HOST" "$REDIS_PORT" "redis"
fi

/app/r0rpc-dbinit
exec /app/r0rpc-server

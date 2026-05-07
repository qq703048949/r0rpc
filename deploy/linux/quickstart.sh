#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

chmod +x install-docker.sh deploy.sh stop.sh start.sh

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  :
elif command -v docker-compose >/dev/null 2>&1; then
  :
else
  echo "docker or docker compose not found, installing..."
  ./install-docker.sh
fi

if [ -d .env.docker ]; then
  echo ".env.docker exists but is a directory; remove or rename deploy/linux/.env.docker first." >&2
  exit 1
fi

if [ ! -f .env.docker ]; then
  cp .env.example .env.docker
  echo "created deploy/linux/.env.docker from template"
fi

./deploy.sh

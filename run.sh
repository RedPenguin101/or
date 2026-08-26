#!/usr/bin/env bash
# Build and start the server. Does not run tests — use build.sh for that.
set -euo pipefail
cd "$(dirname "$0")"
[ -f .env ] && . ./.env
go build -o onramp .
[ -d web/dist ] || (cd web && npm run build)
DATABASE_URL="${DATABASE_URL:?set DATABASE_URL, or put it in .env}" exec ./onramp

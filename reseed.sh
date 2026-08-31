#!/usr/bin/env bash
# Drop the database and the Parquet store, then rebuild both from the catalogue
# on disk and the sample extract.
#
# Destructive on purpose, and cheap on purpose. The schema is pre-production and
# edited in place, so a model change is answered by recreating the database
# rather than migrating it — which is why there are no backfills anywhere and no
# compatibility branches in migrations/001_initial_schema.sql. Nothing in
# Postgres is unrecoverable: the mapping comes from migration_types/ and
# targets/, and the data is a file you re-upload.
#
# Usage: bash reseed.sh [-y]        -y skips the confirmation
set -euo pipefail
cd "$(dirname "$0")"
# .env is the default, but an exported DATABASE_URL wins — so a throwaway
# database can be reseeded without editing the file your own server reads.
[ -n "${DATABASE_URL:-}" ] || { [ -f .env ] && . ./.env; }

: "${DATABASE_URL:?set DATABASE_URL, or put it in .env}"
ADDR="${ONRAMP_ADDR:-:8090}"
PORT="${ADDR##*:}"
STORE="${ONRAMP_STORE:-data}"
API="http://localhost:$PORT/api"

# The database name is the last path segment, minus any query string.
DB="${DATABASE_URL##*/}"
DB="${DB%%\?*}"

# Postgres will not drop a database with an open connection, and the server
# holds a pool. Say which process rather than leaving dropdb to fail obscurely.
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "something is listening on :$PORT — stop your server first:" >&2
    lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >&2
    exit 1
fi

if [ "${1:-}" != "-y" ]; then
    echo "This will DROP the database \"$DB\" and delete $STORE/."
    read -r -p "Every project, run and uploaded dataset goes. Continue? [y/N] " ok
    [ "$ok" = "y" ] || { echo "nothing done"; exit 1; }
fi

echo "==> dropping $DB"
dropdb --if-exists "$DB"
createdb "$DB"

# Orphaned without the datasets table, and a stale file left beside a re-ingested
# one in the same content-hash directory is a confusing thing to find later.
echo "==> clearing $STORE/"
rm -rf "${STORE:?}/"

echo "==> building"
go build -o onramp .

# Migrations run at boot; the catalogue is copied per project at creation.
echo "==> starting a server on :$PORT to seed through the API"
DATABASE_URL="$DATABASE_URL" ONRAMP_ADDR="$ADDR" ONRAMP_STORE="$STORE" ./onramp >/tmp/onramp-reseed.log 2>&1 &
SERVER=$!
trap 'kill $SERVER 2>/dev/null || true' EXIT

for _ in $(seq 1 100); do
    curl -sf "$API/migration-types" >/dev/null 2>&1 && break
    sleep 0.1
done
if ! curl -sf "$API/migration-types" >/dev/null 2>&1; then
    echo "server did not come up:" >&2
    cat /tmp/onramp-reseed.log >&2
    exit 1
fi

echo "==> creating the FAS demo project"
PID=$(curl -sf -X POST "$API/projects" -H 'content-type: application/json' \
    -d '{"name":"FAS demo","migration_type":"FAS","firm":"PERSUS","actor":"joe"}' \
    | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

echo "==> uploading testdata/fas_account_master.csv"
curl -sf -X POST "$API/projects/$PID/datasets" \
    -F source_system=FAS -F schema_name=ACCOUNT-MASTER -F actor=joe \
    -F file=@testdata/fas_account_master.csv \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print("    %d rows, %d withheld at coercion" % (d["rows"], d["rejected"]))'

echo "==> first run"
curl -sf -X POST "$API/projects/$PID/runs" -H 'content-type: application/json' \
    -d '{"reason":"seeded from a fresh database","actor":"joe"}' \
    | python3 -c '
import sys, json
r = json.load(sys.stdin)["run"]
i, t = r["inputs"], r["totals"]
print("    mapping %s  data %s" % (i["mapping"]["digest"], i["data"]["digest"]))
print("    run     %s" % i["digest"])
print("    %d emitted, %d accepted, %d withheld" % (t["emitted"], t["accepted"], t["rejected"]))'

echo
echo "OK — project $PID"
echo "   http://localhost:5173/p/$PID   (once you start run.sh and npm run dev)"

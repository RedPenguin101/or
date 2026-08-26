#!/usr/bin/env bash
# Full pass: format, test, build both halves.
set -euo pipefail
cd "$(dirname "$0")"

echo "==> gofmt"
gofmt -w .

echo "==> go vet"
go vet ./...

echo "==> go test"
# The suite loads Qniform's OpenAPI spec and fails loudly without it: a silently
# skipped spec check is how "Qniform-shaped" output drifts into being a mock.
go test ./...

echo "==> go build"
go build -o onramp .

echo "==> web build"
(cd web && npm run build)

echo "OK"

#!/bin/sh
set -eu
if [ -z "${TEST_DATABASE_URL:-}" ]; then
  echo 'TEST_DATABASE_URL is required: integration tests must not be silently skipped.' >&2
  exit 1
fi
formatting="$(gofmt -l cmd internal migrations web/embed.go)"
if [ -n "$formatting" ]; then printf '%s\n' "$formatting"; exit 1; fi
go mod verify
go vet ./...
go test -race -count=1 -coverprofile=/tmp/stockflow-coverage.out ./...
go tool cover -func=/tmp/stockflow-coverage.out

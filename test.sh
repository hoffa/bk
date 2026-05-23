#!/bin/sh
# Run unit tests and enforce a minimum total statement coverage.
set -e

MIN_COVERAGE=75

go test -coverprofile=coverage.out ./...

total=$(go tool cover -func=coverage.out | awk '/^total:/ {print substr($3, 1, length($3)-1)}')
echo "total coverage: ${total}% (minimum ${MIN_COVERAGE}%)"
awk "BEGIN { exit (${total} < ${MIN_COVERAGE}) ? 1 : 0 }" ||
	{ echo "coverage below minimum"; exit 1; }

.PHONY: check fix build

MIN_COVERAGE := 75

build:
	go build -o bk .

check:
	test -z "$$(gofmt -l .)"
	go vet .
	go test -coverprofile=coverage.out ./...
	@total=$$(go tool cover -func=coverage.out | awk '/^total:/ {print substr($$3, 1, length($$3)-1)}'); \
	echo "total coverage: $$total% (minimum $(MIN_COVERAGE)%)"; \
	awk "BEGIN { exit ($$total < $(MIN_COVERAGE)) ? 1 : 0 }" || { echo "coverage below minimum"; exit 1; }
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run .

fix:
	gofmt -w .
	go mod tidy
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --fix .

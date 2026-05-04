.PHONY: build run dev test lint clean sqlc migrate web web-dev

# Binary name
BINARY=hubplay
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

## build: Build frontend + backend binary
build: web
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/hubplay

## build-go: Build only Go backend (assumes web/dist exists)
build-go:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/hubplay

## run: Build and run
run: build
	./bin/$(BINARY) --config hubplay.example.yaml

## dev: Run Go backend with hot-reload (requires air)
dev:
	air -- --config hubplay.example.yaml

## web: Build frontend for production
web:
	cd web && pnpm install --frozen-lockfile && pnpm run build

## web-dev: Start frontend dev server with HMR
web-dev:
	cd web && pnpm run dev

## test: Run all tests
test:
	go test -race -count=1 ./...

## test-cover: Run tests with coverage
test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint:
	golangci-lint run ./...

## sqlc: Generate Go code from SQL queries
##
## DO NOT RUN unless you know what you're doing. The committed
## internal/db/sqlc/*.sql.go files are the source of truth — running
## sqlc generate on the current queries with sqlc 1.27/1.29/1.31 corrupts
## the output (em-dashes in comments + parameter detection inside
## `NOT (...)` clauses). The committed files were produced by an older
## sqlc build that didn't have these bugs. See
## docs/memory/conventions.md ("sqlc regeneration is locked").
##
## When you genuinely need to add a new query, start a dedicated
## migration session (see the "sqlc lockdown" section in conventions.md
## for the playbook). The guard below makes accidental triggering hard;
## set HUBPLAY_REGEN_SQLC=1 to bypass when you've read the playbook and
## know what you're doing.
sqlc:
	@if [ "$(HUBPLAY_REGEN_SQLC)" != "1" ]; then \
		echo ""; \
		echo "  refusing to run 'sqlc generate'."; \
		echo ""; \
		echo "  the committed internal/db/sqlc/*.sql.go files are hand-validated;"; \
		echo "  current sqlc versions corrupt the output. see"; \
		echo "  docs/memory/conventions.md  ->  'sqlc regeneration is locked'."; \
		echo ""; \
		echo "  to bypass when you genuinely know what you're doing:"; \
		echo "    HUBPLAY_REGEN_SQLC=1 make sqlc"; \
		echo ""; \
		exit 1; \
	fi
	sqlc generate

## sqlc-check: Verify queries are valid (CI)
sqlc-check:
	sqlc compile

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html
	# Preserve web/dist/.gitkeep so the go:embed directive keeps compiling.
	find web/dist -mindepth 1 ! -name '.gitkeep' -delete 2>/dev/null || true

## docker: Build Docker image
docker:
	docker build -t hubplay:$(VERSION) .

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'

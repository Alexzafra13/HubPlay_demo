.PHONY: build run dev test lint clean sqlc migrate web web-dev sqlc-install sqlc-verify

# Binary name
BINARY=hubplay
VERSION?=dev
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

# sqlc version pin. Bumping this is a deliberate decision — every release
# of sqlc has historically introduced subtle changes to the generated
# code (param detection, NULL-type handling, comment placement). After
# bumping, regenerate locally, run the full test suite, and inspect the
# diff carefully before committing. See docs/memory/conventions.md
# section "Regeneración sqlc".
SQLC_VERSION=v1.31.1

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

## sqlc-install: Install the pinned sqlc version into $GOPATH/bin
##
## Idempotent — go install is a no-op when the version is already there.
## Used as a prerequisite by `sqlc` and `sqlc-verify` so contributors
## don't need to manage their own sqlc installation.
sqlc-install:
	@command -v sqlc >/dev/null 2>&1 && [ "$$(sqlc version)" = "$(SQLC_VERSION)" ] || \
		(echo "installing sqlc $(SQLC_VERSION)..." && \
		 go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION))

## sqlc: Generate Go code from SQL queries (uses pinned $(SQLC_VERSION))
##
## After running this you should have NO diff in internal/db/sqlc/. If
## you do, either you edited a .sql file (commit the regen) or your
## sqlc binary doesn't match $(SQLC_VERSION) (run `make sqlc-install`).
sqlc: sqlc-install
	sqlc generate

## sqlc-verify: Regenerate, fail if it produces a diff (used by CI)
##
## This is the drift guard — if the committed *.sql.go files no longer
## match what the pinned sqlc produces from the current queries, CI
## fails with the diff. Catches three kinds of regression:
##   1. someone edited a query without running `make sqlc`
##   2. someone introduced a parser-hostile pattern (em-dashes in
##      comments, `?` inside NOT(...), etc.) — the regen will silently
##      corrupt the output and the diff surfaces it
##   3. someone bumped $(SQLC_VERSION) without re-baselining
sqlc-verify: sqlc-install
	@sqlc generate
	@if ! git diff --quiet internal/db/sqlc/; then \
		echo ""; \
		echo "  sqlc drift detected — committed internal/db/sqlc/*.sql.go does not"; \
		echo "  match what the pinned sqlc $(SQLC_VERSION) produces from the queries."; \
		echo ""; \
		echo "  if you edited a query: run 'make sqlc' and commit the regen."; \
		echo "  if you didn't:         the parser may have hit a bug; see"; \
		echo "                         docs/memory/conventions.md 'Regeneración sqlc'."; \
		echo ""; \
		echo "  drift:"; \
		git --no-pager diff --stat internal/db/sqlc/; \
		exit 1; \
	fi

## sqlc-check: Validate query syntax against schema (lighter than full regen)
sqlc-check: sqlc-install
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

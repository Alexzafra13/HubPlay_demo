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
sqlc:
	sqlc generate

## sqlc-check: Verify queries are valid (CI)
sqlc-check:
	sqlc compile

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html web/dist

## docker: Build Docker image
docker:
	docker build -t hubplay:$(VERSION) .

## help: Show this help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //' | column -t -s ':'

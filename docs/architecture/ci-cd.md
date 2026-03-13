# CI/CD Pipeline — Design Document

## Overview

GitHub Actions para CI. Goreleaser para releases. Docker Hub para imágenes. Todo automático desde push hasta release.

---

## 1. Pipeline Overview

```
Push to branch / PR opened
    │
    ├── lint (Go + Frontend)           ~1 min
    ├── test-unit (Go + Vitest)        ~30s
    ├── test-integration (SQLite)      ~1 min
    ├── test-frontend (Vitest + MSW)   ~1 min
    ├── sqlc-check (verify queries)    ~10s
    ├── build (Go + Frontend)          ~2 min
    │
    ▼ (all pass)
PR mergeable ✓
    │
    ▼ (merge to main)
    ├── test-e2e (Playwright)          ~5 min
    ├── docker-build (multi-arch)      ~5 min
    │
    ▼ (tag pushed: v1.x.x)
Release
    ├── goreleaser (binaries)
    ├── docker push (latest + tag)
    └── GitHub Release (changelog)
```

---

## 2. PR Pipeline (on every push)

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  lint-frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: cd web && npm ci --no-audit
      - run: cd web && npm run lint
      - run: cd web && npm run typecheck

  test-backend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Unit tests
        run: go test ./... -short -race -count=1
      - name: Integration tests
        run: go test ./... -race -count=1 -tags=integration
      - name: Upload coverage
        run: |
          go test ./... -coverprofile=coverage.out -tags=integration
          go tool cover -func=coverage.out

  test-frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: npm
          cache-dependency-path: web/package-lock.json
      - run: cd web && npm ci --no-audit
      - run: cd web && npm run test -- --reporter=verbose

  sqlc-check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: sqlc-dev/setup-sqlc@v4
      - run: sqlc compile
      - name: Verify generated code is up-to-date
        run: |
          sqlc generate
          git diff --exit-code internal/db/sqlc/

  build:
    runs-on: ubuntu-latest
    needs: [lint, lint-frontend, test-backend, test-frontend, sqlc-check]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: actions/setup-node@v4
        with:
          node-version: 20
          cache: npm
          cache-dependency-path: web/package-lock.json
      - name: Build frontend
        run: cd web && npm ci --no-audit && npm run build
      - name: Build backend
        run: CGO_ENABLED=0 go build -tags embed -ldflags "-s -w" -o hubplay ./cmd/hubplay
      - name: Verify binary runs
        run: ./hubplay --version
```

---

## 3. E2E Tests (on main merge)

```yaml
  test-e2e:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    needs: [build]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - name: Install Playwright
        run: cd web && npm ci && npx playwright install --with-deps chromium
      - name: Build backend
        run: |
          cd web && npm run build
          CGO_ENABLED=0 go build -tags embed -o hubplay ./cmd/hubplay
      - name: Run E2E tests
        run: cd web && npm run test:e2e
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: playwright-report
          path: web/playwright-report/
```

---

## 4. Release Pipeline

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags: ["v*"]

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - name: Build frontend
        run: cd web && npm ci --no-audit && npm run build
      - uses: goreleaser/goreleaser-action@v6
        with:
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}
      - uses: docker/build-push-action@v6
        with:
          push: true
          platforms: linux/amd64,linux/arm64
          tags: |
            hubplay/hubplay:${{ github.ref_name }}
            hubplay/hubplay:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

---

## 5. Goreleaser Config

```yaml
# .goreleaser.yml
version: 2
before:
  hooks:
    - go mod tidy
builds:
  - main: ./cmd/hubplay
    binary: hubplay
    env:
      - CGO_ENABLED=0
    flags:
      - -tags=embed
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.ShortCommit}}
      - -X main.date={{.Date}}
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64

archives:
  - format: tar.gz
    name_template: "hubplay_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"
```

---

## 6. Version Info

```go
// cmd/hubplay/main.go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

func printVersion() {
    fmt.Printf("hubplay %s (commit %s, built %s)\n", version, commit, date)
}
```

---

## 7. Development Workflow

```
feature branch → PR → CI checks → review → merge to main → E2E + docker

Para release:
git tag v1.0.0
git push origin v1.0.0
→ Goreleaser crea GitHub Release con binarios
→ Docker image pushed con tag + latest
```

### Branch Naming

```
main              ← stable, deployed
feature/xxx       ← new features
fix/xxx           ← bug fixes
docs/xxx          ← documentation only
```

---

## 8. Makefile (Developer Commands)

```makefile
.PHONY: dev build test lint docker

dev:               ## Start dev environment (backend + frontend)
	@echo "Starting backend (air)..."
	air &
	@echo "Starting frontend (vite)..."
	cd web && npm run dev

build:             ## Production build
	cd web && npm ci && npm run build
	CGO_ENABLED=0 go build -tags embed -ldflags "-s -w" -o hubplay ./cmd/hubplay

test:              ## Run all tests
	go test ./... -race -tags=integration
	cd web && npm run test

lint:              ## Lint everything
	golangci-lint run
	cd web && npm run lint && npm run typecheck

sqlc:              ## Regenerate sqlc code
	sqlc generate

proto:             ## Regenerate gRPC code
	protoc --go_out=. --go-grpc_out=. proto/*.proto

docker:            ## Build Docker image
	docker build -t hubplay:dev .

clean:             ## Clean build artifacts
	rm -f hubplay
	rm -rf web/dist
	rm -rf internal/db/sqlc/  # Regenerated by sqlc
```

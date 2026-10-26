GO ?= go
NPM ?= npm
VERSION ?= 2.2.0
GOVULNCHECK_VERSION := v1.1.4
GITLEAKS_VERSION := v8.30.1
TRIVY_IMAGE := aquasec/trivy:0.74.0@sha256:62b1e65e8869bc4b4c6aa4fa2b21595256c7c2f6018a9d9ad61caf87187c1969

.PHONY: build web dev check e2e security compose-check image-check image-security release-check

web:
	$(NPM) --prefix web ci --no-audit --no-fund
	$(NPM) --prefix web run build

build: web
	$(GO) build -tags production -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/ingest ./cmd/ingest

dev: web
	$(GO) run ./cmd/ingest serve

check: web
	$(NPM) --prefix web run format:check
	$(NPM) --prefix web test
	$(GO) test -race ./...
	$(GO) -C torznab test -race ./...
	$(GO) vet ./...
	$(GO) -C torznab vet ./...

# Uses a prebuilt production binary and a mandatory disposable PostgreSQL DSN.
# Keep browser execution separate from the existing database-optional checks.
e2e:
	$(NPM) --prefix web run e2e

# Full Git history must be available. Findings fail closed; no blanket allowlists.
security: web
	$(GO) run github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION) git --redact --log-opts="--all" .
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) -tags production ./...
	$(GO) -C torznab run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	$(NPM) --prefix web audit

# Explicit files and an empty env file never load a developer's private overrides.
compose-check:
	POSTGRES_PASSWORD=config-only docker compose --env-file /dev/null -f compose.yaml config --quiet
	POSTGRES_PASSWORD=config-only docker compose --env-file /dev/null -f compose.yaml -f compose.build.yaml config --quiet

# These images are local only; this target never pushes or retags a release.
image-check:
	docker buildx build --load --build-arg VERSION=$(VERSION) --tag ingest-local:check .
	docker run --rm --network none ingest-local:check version
	docker run --rm --network none ingest-local:check help
	docker buildx build --platform linux/amd64,linux/arm64 --build-arg VERSION=$(VERSION) --tag ingest-local:check --output type=cacheonly .
	$(MAKE) image-security

# Scan only the built image archive: never expose the Docker socket or private state.
image-security:
	@set -eu; \
	scan_dir=$$(mktemp -d); \
	trap 'rm -rf "$$scan_dir"' EXIT HUP INT TERM; \
	docker image save --output "$$scan_dir/image.tar" ingest-local:check; \
	docker run --rm --mount type=bind,src="$$scan_dir",dst=/scan,readonly \
		$(TRIVY_IMAGE) image --input /scan/image.tar --scanners vuln \
		--severity HIGH,CRITICAL --exit-code 1 --no-progress

# Requires a disposable TEST_DATABASE_URL and installed Playwright Chromium.
# Runs the release gates locally without publishing or touching a Compose deployment.
release-check: check security build compose-check
	E2E_BINARY=../bin/ingest $(MAKE) e2e
	$(MAKE) image-check

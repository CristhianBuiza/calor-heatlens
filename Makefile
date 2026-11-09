SHELL := /bin/bash
export GOTOOLCHAIN ?= local

# Local defaults; override on the command line or in the environment.
CALOR_ADMIN_KEY ?= dev-admin-key
API_PORT        ?= 8080
DASH_PORT       ?= 3000
DEMO_PORT       ?= 5500
DB              ?= $(CURDIR)/data/calor.db
CHROME_PATH     ?= $(shell command -v google-chrome || command -v chromium || command -v chromium-browser)

API_ENV = CALOR_ADMIN_KEY=$(CALOR_ADMIN_KEY) CALOR_ADDR=:$(API_PORT) CALOR_DB_PATH=$(DB) \
	CALOR_TRACKER_DIR=$(CURDIR)/tracker/dist GOMEMLIMIT=$${GOMEMLIMIT:-192MiB}
DASH_ENV = CALOR_API_URL=http://127.0.0.1:$(API_PORT) CALOR_PUBLIC_API_URL=http://localhost:$(API_PORT) \
	CALOR_ADMIN_KEY=$(CALOR_ADMIN_KEY) PORT=$(DASH_PORT) HOSTNAME=0.0.0.0
E2E_ENV = CALOR_API=http://127.0.0.1:$(API_PORT) DASHBOARD=http://localhost:$(DASH_PORT) \
	DEMO_PORT=$(DEMO_PORT) CALOR_ADMIN_KEY=$(CALOR_ADMIN_KEY) CHROME_PATH=$(CHROME_PATH)

.PHONY: help deps build build-api build-tracker build-dashboard test run-api run-demo run-dashboard dev start \
	seed e2e screenshots loadtest measure clean

help: ## Show targets
	@grep -E '^[a-z0-9-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

deps: ## Install Go modules and npm packages
	cd api && go mod download
	cd tracker && npm ci --no-audit --no-fund
	cd dashboard && npm ci --no-audit --no-fund
	cd e2e && npm ci --no-audit --no-fund

build: build-api build-tracker build-dashboard ## Build everything

build-api: ## Build the static Go binary (CGO disabled)
	cd api && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../bin/calor-api ./cmd/calor-api
	cd api && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../bin/calor-loadtest ./cmd/calor-loadtest

build-tracker: ## Build tracker/dist/calor.min.js
	cd tracker && npm run build

build-dashboard: ## Build the Next.js standalone server
	cd dashboard && npm run build

test: ## Run Go vet + tests
	cd api && go vet ./... && go test ./...

run-api: build-tracker ## Run the API (go run)
	@mkdir -p $(dir $(DB))
	cd api && $(API_ENV) go run ./cmd/calor-api

run-demo: build-tracker ## Serve the three demo sites on :5500
	cd tracker && DEMO_PORT=$(DEMO_PORT) CALOR_ENDPOINT=http://localhost:$(API_PORT)/api/events node demo/serve.mjs

run-dashboard: ## Run the dashboard in dev mode
	cd dashboard && $(DASH_ENV) npx next dev -p $(DASH_PORT)

dev: ## API + demo sites + dashboard (dev) together; Ctrl-C stops all
	$(MAKE) -j3 run-api run-demo run-dashboard

start: build ## Production-style: built API binary + standalone dashboard + demo
	@mkdir -p $(dir $(DB))
	trap 'kill 0' EXIT; \
	$(API_ENV) ./bin/calor-api & \
	(cd tracker && DEMO_PORT=$(DEMO_PORT) CALOR_ENDPOINT=http://localhost:$(API_PORT)/api/events node demo/serve.mjs) & \
	(cd dashboard && $(DASH_ENV) node --max-old-space-size=192 .next/standalone/server.js) & \
	wait

seed: ## Seed 3 demo sites with 30 days of traffic (needs API + demo running)
	cd e2e && $(E2E_ENV) node seed.mjs

e2e: ## Drive real browser sessions on the demo sites and capture tracker traffic
	cd e2e && $(E2E_ENV) node live.mjs

screenshots: ## Capture dashboard screenshots into e2e/output (or SHOTS_DIR)
	cd e2e && $(E2E_ENV) SHOTS_DIR=$${SHOTS_DIR:-output} node screenshots.mjs

loadtest: ## Ingest + query load test against a running API (samples its RSS)
	./bin/calor-loadtest -api http://127.0.0.1:$(API_PORT) -key $(CALOR_ADMIN_KEY) -pid $$(pgrep -x calor-api || echo 0)

measure: ## Tracker size, dashboard JS per route, API/dashboard RSS (needs the stack running)
	cd e2e && $(E2E_ENV) node measure.mjs

clean: ## Remove build output and the local database
	rm -rf bin tracker/dist dashboard/.next data e2e/output

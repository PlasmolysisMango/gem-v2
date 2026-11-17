GO      ?= go
PNPM    ?= pnpm
BIN     ?= bin
ADDR    ?= :8080
DB      ?= gem.db
SECRET  ?= dev-secret-change-me

# arena defaults: make arena A=master B=medium GAMES=500 PARALLEL=8 WORKERS=6
A        ?= master
B        ?= medium
PLAYERS  ?= 2
GAMES    ?= 100
HARD_MS  ?= 300
MASTER_MS ?= 1500
PARALLEL ?= 1
WORKERS  ?= 0

.PHONY: all dev dev-server dev-web web-deps web test test-go test-web build build-server build-cli build-arena arena docker clean

all: build

## dev: run the Go server (:8080) and the Vite dev server (:5173, proxies /api and /ws)
dev: web-deps
	@trap 'kill 0' INT TERM EXIT; \
	$(GO) run ./cmd/gem-server -addr $(ADDR) -db $(DB) -secret $(SECRET) & \
	cd web && $(PNPM) dev

dev-server:
	$(GO) run ./cmd/gem-server -addr $(ADDR) -db $(DB) -secret $(SECRET)

dev-web: web-deps
	cd web && $(PNPM) dev

web-deps: web/node_modules/.modules.yaml

web/node_modules/.modules.yaml: web/package.json web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile

## web: build the React client into web/dist
web: web-deps
	cd web && $(PNPM) build

## test: Go tests (engine, AI, auth, store, WS integration, CLI) + web unit tests
test: test-go test-web

test-go:
	$(GO) vet ./...
	$(GO) test ./...

test-web: web-deps
	cd web && $(PNPM) test

## build: web client first, then a single gem-server binary with web/dist embedded
build: build-server build-cli build-arena

build-server: web
	$(GO) build -tags embed -trimpath -o $(BIN)/gem-server ./cmd/gem-server

build-cli:
	$(GO) build -trimpath -o $(BIN)/gem-cli ./cmd/gem-cli

build-arena:
	$(GO) build -trimpath -o $(BIN)/gem-arena ./cmd/gem-arena

## arena: AI self-play evaluation
arena:
	$(GO) run ./cmd/gem-arena -a $(A) -b $(B) -players $(PLAYERS) -games $(GAMES) -hard-ms $(HARD_MS) -master-ms $(MASTER_MS) -parallel $(PARALLEL) -workers $(WORKERS)

docker:
	docker build -t gem-server .

clean:
	rm -rf $(BIN) web/dist

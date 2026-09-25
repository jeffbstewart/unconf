.PHONY: build dev test test-go test-web web clean

BIN := bin/unconf

# One static binary with the frontend embedded.
build: web
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/unconf

web: web/node_modules
	cd web && npm run build
	@touch web/dist/.gitkeep  # vite empties dist; keep the embed target present

# Go server on :8080 plus the Vite dev server (hot reload) proxying /api and /ws.
# Ctrl-C stops both.
dev: web/node_modules
	@trap 'kill 0' EXIT; \
	go run ./cmd/unconf & \
	cd web && npm run dev

test: test-go test-web

test-go:
	go vet ./...
	go test ./...

test-web: web/node_modules
	cd web && npm run typecheck && npm test

web/node_modules: web/package.json web/package-lock.json
	cd web && npm ci
	@touch $@

clean:
	rm -rf bin
	find web/dist -mindepth 1 ! -name .gitkeep -delete

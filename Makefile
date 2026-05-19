.PHONY: build dashboard dashboard-dev test

# Build Go binary
build:
	go build -o github-mcp .

# Install JS deps and build the dashboard (produces dashboard/dist/)
dashboard:
	cd dashboard && npm ci && VITE_API_BASE_URL=$(VITE_API_BASE_URL) npm run build

# Run the Vite dev server (hot reload, proxies API to localhost:6666)
dashboard-dev:
	cd dashboard && npm install && npm run dev

# Run all tests (Go + dashboard TypeScript check)
test:
	go test ./...
	cd dashboard && npm ci && npx tsc --noEmit

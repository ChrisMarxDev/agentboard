.PHONY: build build-frontend dev test test-go test-frontend test-integration clean

build-frontend:
	cd frontend && npm install && npm run build

build: build-frontend
	go build -ldflags="-s -w" -o agentboard ./cmd/agentboard

dev:
	@echo "Start frontend: cd frontend && npm run dev"
	@echo "Start backend:  go run ./cmd/agentboard --dev --no-open"

test: test-go test-frontend

test-go:
	go test ./...

test-frontend:
	cd frontend && npm run test

test-integration: build
	./scripts/integration-test.sh

clean:
	rm -f agentboard
	rm -rf frontend/dist

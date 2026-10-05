.PHONY: all web wasm run test release-patch release-minor release-major

all: web wasm
	go build ./...

wasm:
	mkdir -p web
	GOOS=js GOARCH=wasm go build -o web/collab.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/

web:
	npm ci
	npm run build:web

run: web wasm
	go run ./cmd/collabd -mem -dev

test: web wasm
	go vet ./...
	go test -race ./...
	node --test web/src/editor_test.mjs

test-nocache: web wasm
	go vet ./...
	go test -race ./... -count=1
	node --test web/src/editor_test.mjs

release-patch:
	./scripts/release.sh patch

release-minor:
	./scripts/release.sh minor

release-major:
	./scripts/release.sh major

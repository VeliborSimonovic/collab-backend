.PHONY: wasm run test release-patch release-minor release-major

wasm:
	mkdir -p web
	GOOS=js GOARCH=wasm go build -o web/collab.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/

run: wasm
	go run ./cmd/collabd -mem -dev

test: wasm
	go vet ./...
	go test -race ./...

test-nocache: wasm
	go vet ./...
	go test -race ./... -count=1

release-patch:
	./scripts/release.sh patch

release-minor:
	./scripts/release.sh minor

release-major:
	./scripts/release.sh major

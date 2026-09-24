.PHONY: wasm run test

wasm:
	mkdir -p web
	GOOS=js GOARCH=wasm go build -o web/collab.wasm ./wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/

run: wasm
	go run ./cmd/collabd -mem

test: wasm
	go vet ./...
	go test -race ./...

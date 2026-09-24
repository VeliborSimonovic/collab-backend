# Changelog

## v0.1.0

- A YATA-based CRDT for plain text in Go, with offline edits that merge on reconnect
- A WebAssembly browser client and a demo page (open it in two tabs and type)
- Ed25519 signed tokens (JWT, `EdDSA`) with editor and viewer roles
- Live cursors for everyone in a document
- An HTTP API to read and edit documents from your backend
- SQLite storage, a single static binary, and a 17.6 MB Docker image
- Load test on Docker with 2 CPUs and 1 GB: 200 connections across 20 documents for 60 seconds, all documents converged, p50 latency 0.7 ms, p95 15 ms, about 67 MiB of memory
- `PROTOCOL.md` documents the wire format, with test vectors checked by `crdt/vectors_test.go`

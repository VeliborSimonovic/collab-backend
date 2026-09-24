# Changelog

## v0.1.1

- Demo page: open any document as any user with `?doc=<id>&token=<jwt>`; the name and role come from the token. Without a token it still runs in dev mode (`?doc=` optional, defaults to `demo`)
- Demo page: viewers get a read-only editor, and the status line and tab title show the document, name and role
- Fix: viewers on the demo page no longer get disconnected in a loop. The page answered the server's handshake with an empty Update, which the server refuses from viewers; viewers now skip that reply, stay connected, receive edits live and show up in the user list
- `scripts/run-token-mode.sh`: runs the engine in token mode with `COLLAB_PUBLIC_KEY` from `.env` and the in-memory store
- `scripts/tokens.sh`: prints browser links for a test matrix of users and documents (editors, a viewer, and one link that must be refused).

## v0.1.0

- A YATA-based CRDT for plain text in Go, with offline edits that merge on reconnect
- A WebAssembly browser client and a demo page (open it in two tabs and type)
- Ed25519 signed tokens (JWT, `EdDSA`) with editor and viewer roles
- Live cursors for everyone in a document
- An HTTP API to read and edit documents from your backend
- SQLite storage, a single static binary, and a 17.6 MB Docker image
- Load test on Docker with 2 CPUs and 1 GB: 200 connections across 20 documents for 60 seconds, all documents converged, p50 latency 0.7 ms, p95 15 ms, about 67 MiB of memory
- `PROTOCOL.md` documents the wire format, with test vectors checked by `crdt/vectors_test.go`

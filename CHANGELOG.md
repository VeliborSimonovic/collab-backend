# Changelog

## v0.1.4

- Edits are written to SQLite in batches, one transaction per `COLLAB_FLUSH` interval (default `10ms`), instead of one commit per edit. An edit is now broadcast before it is on disk for at most one interval; see "Durability" in the README. Reading a document flushes pending edits first, and shutdown flushes once more
- A failed batch write is not lost: the edits go back into the queue (in order, before newer ones) and are retried on the next tick, and the error is logged. Until a write succeeds, saving keeps returning the error, so clients are still disconnected when the disk is broken
- A room loads outside the hub lock: a slow document no longer blocks other documents, and concurrent opens of the same document load it once. A failed load reaches every waiter and is retried on the next open
- `/healthz` no longer waits for busy rooms (room sizes and connection counts are atomic), and reports `writeQueue`, the number of edits not yet on disk. A `writeQueue` that keeps growing means the disk can't keep up
- Loading a 30,000-op document: replaying the ops takes about 8 ms and reading them from SQLite about 26 ms (Apple M3, `BenchmarkReplay30k`, `BenchmarkSQLiteLoad30k`). The 2.9 s seen in the Docker load test was therefore not the load itself but the `Load` waiting behind thousands of queued write transactions on the single database connection, which batching removes
- Load tester: `-rate` sets the time between keystrokes (default `100ms`), and clients that are dropped reconnect after 1 s like a browser. The report shows `kicks` and `failed reconnects`, so `NOT CONVERGED` means a real bug again
- `COLLAB_PPROF` / `-pprof`: an optional Go profiling server on its own address, never on the public port
- Load tests on Docker with 2 CPUs and 1 GB, SQLite (results in the README): 1000 connections across 200 documents at one keystroke per second, p50 0.5 ms, p95 1.5 ms, no kicks, all documents converged, under 130 MiB of memory and under 50% of the CPU budget; the same after a restart. At 10 keystrokes per second (5 to 10 times real typing) the two CPUs are saturated (p95 1.8 s, 29 of 1000 clients dropped and reconnected), and all documents still converged. Before this release the same stress test froze `/healthz` for 41 s and dropped about 780 clients

## v0.1.3

- Small refactoring
- `Largest()` now takes biggest values that cna be from different rooms
- Goroutine in `HandleWS()` now uses `case <- ctx.Done()` insead of `case <- done`, `cancel()` is called after pinging instead of being defered



## v0.1.2

- Empty Updates are ignored for every role, so any viewer client can complete the handshake
- A failed save disconnects the client instead of being silently dropped
- Dev mode must be switched on explicitly with `COLLAB_DEV=1` / `-dev`, and the server refuses to start with no key and no dev flag (`-dev` is ignored, with a warning, when a key is set; `make run` passes `-dev`)
- The server pings every connection every 30 s and drops dead ones
- `/healthz` reports `maxItems` and `maxLoadMs`

## v0.1.1

- Demo page: open any document as any user with `?doc=<id>&token=<jwt>`; the name and role come from the token. Without a token it works only when the engine runs in dev mode (`?doc=` optional, defaults to `demo`)
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

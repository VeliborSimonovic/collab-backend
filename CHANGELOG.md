# Changelog

## Unreleased

- Shared types: a document is now a set of containers (Text, Array, Map) that can nest. A root container has a name and a kind, a nested one is created by an item and named by its ID (PROTOCOL.md section 1)
- New `Doc` calls in `crdt`: `TextInsert`, `TextDelete`, `ArrayInsertJSON`, `ArrayInsertType`, `ArrayDelete`, `MapSetJSON`, `MapSetType`, `MapDelete`, `MapChild`, `ToJSON` and `JSON`. Maps keep one sequence per key, and setting a key again keeps the older items
- Inserts carry a parent and a key, and their content can be a codepoint, JSON (at most 64 KiB) or a container kind. Ops that do not fit their container, or whose origins belong to another parent or key, are rejected like duplicates
- Wire format: new insert tag 3 for inserts with a parent, a key or typed content. An insert into the default text with a codepoint still uses tag 1, so data written by v0.3.0 loads and edits unchanged and old clients keep working. Decoding garbage returns `ErrBadMessage`, never a panic
- `reader.bytes` returns a copy of the bytes, so reusing the read buffer cannot change a decoded op
- WebAssembly bindings for the shared types
- Tests: round trip of random shared-type ops, tag-3 garbage in `TestDecodeGarbage`, `TestLegacyStaysTag1`, and a new test vector for a map set (PROTOCOL.md section 10)
- Checked by hand: the current build opens a database written by v0.3.0, loads an old document and edits it normally

## v0.3.0

- CodeMirror 6 editor replaces the textarea: line numbers, markdown highlighting, line wrapping
- Presence carets with names, drawn as editor decorations
- The character limit is enforced by a change filter: an edit that would exceed it never enters the document and no op is sent
- New `make web` step builds `web/editor.js` with esbuild; the bundle is committed and CI fails when it is stale

## v0.2.0

- Connections close with 4001 when their token expires
- Ephemeral mode (`COLLAB_EPHEMERAL` / `-ephemeral`, `-mem` only): a document is dropped when its last client leaves
- Limits against junk: `COLLAB_MAX_TEXT`, `COLLAB_MAX_OPS`, `COLLAB_RATE` / `COLLAB_BURST` and `COLLAB_MAX_MESSAGE`, with close codes 4002 (document full) and 4003 (too many edits)
- The public demo: rooms with 6-digit codes (`/demo/config`, `/demo/rooms`, `/demo/rooms/join`), `COLLAB_DEMO_KEY`, `COLLAB_DEMO_TTL` and `COLLAB_TRUST_PROXY`. One room per IP per hour (`COLLAB_DEMO_ROOMS_PER_IP`, `COLLAB_DEMO_ROOM_WINDOW`), and a limit on wrong codes
- The demo landing page (start / join a room), with the room code and invite link, a countdown, and people and character counters
- `deploy/` templates and `DEPLOY.md`

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

# collab

A small real-time collaboration engine for plain text, written in Go. Several people edit the same document at once, and edits made offline merge cleanly when a client reconnects. It runs comfortably on 2 vCPU / 1 GB.

It is **not** a product: there are no user accounts, no billing and no UI beyond a demo page. Your app brings the users and issues the tokens; this engine keeps the documents in sync.

## Quickstart

```sh
git clone https://github.com/VeliborSimonovic/collab-backend.git
cd collab-backend
docker build -t collab .
docker run -p 8080:8080 -v collab-data:/data -e COLLAB_DEV=1 collab
```

Open <http://localhost:8080> in two tabs and type.

**Dev mode** must be explicitly set using `-e COLLAB_DEV=1` flag

> On Docker Desktop for Mac, prefer a named volume (`-v collab-data:/data`) over a bind mount such as `-v ./data:/data`. Edits are committed to SQLite in batches (see [Durability](#durability)), and fsync on a macOS bind mount is so slow that latency climbed to tens of seconds under load when every edit was committed on its own. Batching should help a lot, but a named volume is still the safer choice. On a Linux server a bind mount is fine.

## Configuration

Every setting can be an environment variable or, where noted, a flag. A flag beats the environment, which beats the default.

| Variable | Default | Meaning |
|---|---|---|
| `COLLAB_ADDR` (`-addr`) | `:8080` | Listen address. |
| `COLLAB_DB` (`-db`) | `collab.db` (`/data/collab.db` in the Docker image) | SQLite file. |
| `-mem` (flag only) | off | Keep everything in memory; nothing is saved. |
| `COLLAB_PUBLIC_KEY` | empty | Base64 Ed25519 public key. Required unless dev mode is on. |
| `COLLAB_ORIGINS` | empty | Comma-separated browser origins allowed to open a WebSocket, as host or host:port without `https://`, e.g. `app.example.com,localhost:3000`. Empty = same-origin only. |
| `COLLAB_IDLE` | `5m` | How long an unused document stays in memory (Go duration: `5m`, `30s`). |
| `COLLAB_MAX_CLIENTS` | `100` | Connections per document. |
| `COLLAB_MAX_ITEMS` | `1000000` | Characters per document, including deleted ones. |
| `COLLAB_FLUSH` (`-flush`) | `10ms` | How often buffered edits are written to SQLite, in one transaction (Go duration). Longer means fewer writes but a wider [durability window](#durability). |
| `COLLAB_PPROF` (`-pprof`) | empty (off) | Address for a separate Go profiling server, e.g. `127.0.0.1:6060`. Never on the public port; do not expose it to the internet. In Docker use `-e COLLAB_PPROF=:6060 -p 127.0.0.1:6060:6060`. |
| `COLLAB_DEV` / `-dev` | off | Dev mode: no auth, anyone can edit. Only used when no key is set (ignored with a warning otherwise). Local testing only. |
| `COLLAB_EPHEMERAL` / `-ephemeral` | off | Drop a document when its last client leaves. Needs `-mem`. |
| `COLLAB_DEMO_KEY` / `-demo-key` | empty (demo off) | PEM file with the demo's private key. Needs `-mem`, turns ephemeral mode on, and its public half must equal `COLLAB_PUBLIC_KEY`. See [Public demo](#public-demo). |
| `COLLAB_DEMO_TTL` / `-demo-ttl` | `5m` | How long a demo room lasts. |
| `COLLAB_DEMO_ROOMS_PER_IP` | `1` | Rooms one IP may start per window. |
| `COLLAB_DEMO_ROOM_WINDOW` | `1h` | The window for the setting above. |
| `COLLAB_TRUST_PROXY` / `-trust-proxy` | off | Take the client IP from the first `X-Forwarded-For` entry. Turn on only behind a reverse proxy, otherwise everyone looks like the proxy's IP. |
| `COLLAB_MAX_TEXT` | `0` (no limit) | Characters per document (visible ones); a bigger insert closes the connection with 4002. Also shown to the demo page. |
| `COLLAB_MAX_OPS` | `0` (no limit) | Operations per document. |
| `COLLAB_RATE` | `0` (no limit) | Operations per second per connection. |
| `COLLAB_BURST` | `0` | Burst size for `COLLAB_RATE`. With a rate set and burst 0, it becomes the larger of `COLLAB_MAX_TEXT` and 100, so pasting a full document works. |
| `COLLAB_MAX_MESSAGE` | `8388608` | Largest WebSocket message in bytes (8 MiB). |

An invalid value (for example `COLLAB_MAX_CLIENTS=abc`) stops the server at startup with a message naming the variable.

## Public demo

With `COLLAB_DEMO_KEY` set, opening `/` shows a landing page where anyone can start a shared room or join one with a 6-digit code. A room holds a few people and disappears after `COLLAB_DEMO_TTL`; everybody's session ends together when it does.

- It uses its own key pair: run `collabd keygen`, save the PEM (for example as `demo.pem`) and set `COLLAB_PUBLIC_KEY` to the matching public key. Do not reuse your production key.
- It needs `-mem`. Nothing is stored, and a document is dropped when its last client leaves.
- Starting rooms is limited per IP (see `COLLAB_DEMO_ROOMS_PER_IP`), and wrong codes are limited per IP, so codes can't be guessed. Behind a reverse proxy set `COLLAB_TRUST_PROXY=1`.

```
COLLAB_PUBLIC_KEY=<public key> COLLAB_DEMO_KEY=demo.pem COLLAB_DEMO_TTL=2m \
COLLAB_MAX_TEXT=500 COLLAB_MAX_OPS=5000 COLLAB_RATE=30 go run ./cmd/collabd -mem
```

## Durability

Edits are written to SQLite in batches, once per `COLLAB_FLUSH` interval, not one commit per edit. An edit is therefore broadcast before it is on disk, for at most one flush interval.

If the server crashes in that window, the ops are still in the clients' copies, and the reconnect handshake sends them back. Nothing is lost as long as a client that had them reconnects.

## How tokens work

1. `collabd keygen` prints a key pair. The public key goes to the engine (`COLLAB_PUBLIC_KEY`); the private key stays in your app.
2. Your app signs a JWT (alg `EdDSA`) with `sub`, `doc`, `role` (`editor` or `viewer`), `name`, `color` and `exp`.
3. Browsers connect to `/ws?doc=<id>&token=<jwt>`.
4. Servers call the HTTP API with `Authorization: Bearer <jwt>`.
5. For testing, `collabd token -key private.pem -doc demo` prints a token.

`collabd` is `go run ./cmd/collabd`, or the binary if you built one.

## Performance

On Docker with `--cpus=2 --memory=1g` and SQLite, with the load tester (`go run ./cmd/loadtest`) on the same laptop, for 60 seconds. `-rate` is the time between keystrokes of each client; a client that gets dropped reconnects, like a browser.

| Test | Load tester flags | p50 | p95 | Kicks | Result |
|---|---|---|---|---|---|
| Realistic | `-conns 1000 -docs 200 -rate 1s` | 0.5 ms | 1.5 ms | 0 | all documents converged |
| Restart (server stopped and started on the same data, then the same test) | `-conns 1000 -docs 200 -rate 1s` | 0.5 ms | 1.2 ms | 0 | all documents converged |
| One hot document | `-conns 50 -docs 1 -rate 1s` | 1.1 ms | 2.7 ms | 0 | all documents converged |
| Stress (5 to 10 times real typing speed) | `-conns 1000 -docs 200 -rate 100ms` | 422 ms | 1.8 s | 29 | all documents converged |

Memory stayed under 130 MiB and CPU under 50% of the 200% the container may use in the realistic, restart and hot-document runs. `/healthz` answered without gaps in those runs. In the stress run the two CPUs were saturated: 29 of 1000 clients were dropped and reconnected on their own, and `/healthz` answered in 0.35 s at worst when asked from inside the container. Its latency numbers are inflated because the load tester shares the machine.

## Protocol

The wire format is documented in [PROTOCOL.md](PROTOCOL.md).

## License

MIT, see [LICENSE](LICENSE).

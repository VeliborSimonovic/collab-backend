# collab

A small real-time collaboration engine for plain text, written in Go. Several people edit the same document at once, and edits made offline merge cleanly when a client reconnects. It runs comfortably on 2 vCPU / 1 GB.

It is **not** a product: there are no user accounts, no billing and no UI beyond a demo page. Your app brings the users and issues the tokens; this engine keeps the documents in sync.

## Quickstart

```sh
git clone https://github.com/VeliborSimonovic/collab-backend.git
cd collab-backend
docker build -t collab .
docker run -p 8080:8080 -v collab-data:/data collab
```

Open <http://localhost:8080> in two tabs and type.

With no `COLLAB_PUBLIC_KEY` set the engine runs in **dev mode**: anyone can connect and edit. Don't expose that to the internet.

> On Docker Desktop for Mac, prefer a named volume (`-v collab-data:/data`) over a bind mount such as `-v ./data:/data`. Every edit is committed to SQLite, and fsync on a macOS bind mount is so slow that latency climbed to tens of seconds under load. On a Linux server a bind mount is fine.

## Configuration

Every setting can be an environment variable or, where noted, a flag. A flag beats the environment, which beats the default.

| Variable | Default | Meaning |
|---|---|---|
| `COLLAB_ADDR` (`-addr`) | `:8080` | Listen address. |
| `COLLAB_DB` (`-db`) | `collab.db` (`/data/collab.db` in the Docker image) | SQLite file. |
| `-mem` (flag only) | off | Keep everything in memory; nothing is saved. |
| `COLLAB_PUBLIC_KEY` | empty | Base64 Ed25519 public key. Empty = dev mode (anyone can edit). |
| `COLLAB_ORIGINS` | empty | Comma-separated browser origins allowed to open a WebSocket, as host or host:port without `https://`, e.g. `app.example.com,localhost:3000`. Empty = same-origin only. |
| `COLLAB_IDLE` | `5m` | How long an unused document stays in memory (Go duration: `5m`, `30s`). |
| `COLLAB_MAX_CLIENTS` | `100` | Connections per document. |
| `COLLAB_MAX_ITEMS` | `1000000` | Characters per document, including deleted ones. |

An invalid value (for example `COLLAB_MAX_CLIENTS=abc`) stops the server at startup with a message naming the variable.

## How tokens work

1. `collabd keygen` prints a key pair. The public key goes to the engine (`COLLAB_PUBLIC_KEY`); the private key stays in your app.
2. Your app signs a JWT (alg `EdDSA`) with `sub`, `doc`, `role` (`editor` or `viewer`), `name`, `color` and `exp`.
3. Browsers connect to `/ws?doc=<id>&token=<jwt>`.
4. Servers call the HTTP API with `Authorization: Bearer <jwt>`.
5. For testing, `collabd token -key private.pem -doc demo` prints a token.

`collabd` is `go run ./cmd/collabd`, or the binary if you built one.

## Performance

On Docker with `--cpus=2 --memory=1g`, 200 WebSocket connections typing 10 characters per second across 20 documents for 60 seconds (`go run ./cmd/loadtest`): all documents converged, p50 latency 0.7 ms, p95 15 ms, about 67 MiB of memory.

## Protocol

The wire format is documented in PROTOCOL.md.

## License

MIT, see [LICENSE](LICENSE).

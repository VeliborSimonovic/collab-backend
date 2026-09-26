# Deploying

Two engines behind one Caddy, on one server:

- `engine`: token mode, SQLite in the `engine-data` volume. Your app signs tokens for it.
- `engine-demo`: the [public demo](README.md#public-demo). In-memory, its own key pair, small limits.
- `caddy`: the only thing that listens on the internet (80 and 443). It gets the HTTPS certificates and proxies to the engines, and it removes `?token=` from its access log.

The files in `deploy/` are templates with no secrets. The real values live only in `/srv/collab/.env` and `/srv/collab/keys/` on the server. Both are git-ignored.

## 1. DNS

Point an A record for each domain at the server's IP, and wait until they resolve:

```
engine.example.com  A  <server IP>
demo.example.com    A  <server IP>
```

Caddy can't get certificates until this works.

## 2. Docker and firewall

```
curl -fsSL https://get.docker.com | sh
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

The engines publish no ports, so only Caddy is reachable from outside. (Docker edits the firewall itself for published ports and bypasses `ufw`, which is one more reason to publish nothing else.)

## 3. Get the code

```
sudo mkdir -p /srv/collab && sudo chown $USER /srv/collab
git clone <repo url> /srv/collab
cd /srv/collab
```

## 4. Production keys (on your laptop)

```
go run ./cmd/collabd keygen
```

The first line is the public key. The PEM that follows is the private key: it stays in your app and never goes to this server. Only the public key goes into `COLLAB_PUBLIC_KEY` below.

## 5. Demo key (on the server)

The demo signs its own tokens, so its private key has to live on the server. Make it there, readable only by you:

```
cd /srv/collab
docker build -t collab:latest .
mkdir -p keys
( umask 077; docker run --rm collab:latest keygen > keys/keygen.txt )
tail -n +2 keys/keygen.txt > keys/demo.pem
head -1 keys/keygen.txt          # COLLAB_PUBLIC_KEY=<this is DEMO_PUBLIC_KEY>
rm keys/keygen.txt
chmod 600 keys/demo.pem
```

Copy the value after `COLLAB_PUBLIC_KEY=` (everything, including a trailing `=`) for `DEMO_PUBLIC_KEY` in the next step. Don't reuse the production key here.

## 6. Environment

```
cp deploy/.env.example .env
nano .env
```

| Variable | Value |
|---|---|
| `ENGINE_DOMAIN` | e.g. `engine.example.com` |
| `DEMO_DOMAIN` | e.g. `demo.example.com` |
| `COLLAB_PUBLIC_KEY` | production public key (step 4) |
| `DEMO_PUBLIC_KEY` | demo public key (step 5) |
| `COLLAB_ORIGINS` | browser origins allowed to open a WebSocket, e.g. `app.example.com`. Empty = same origin only. |

## 7. Start

```
docker compose -f deploy/docker-compose.yml --env-file .env up -d --build
```

`--env-file .env` makes Compose read `/srv/collab/.env` instead of looking inside `deploy/`.

## 8. Check

```
curl https://engine.example.com/healthz
curl https://demo.example.com/healthz
```

Both should answer JSON like `{"rooms":0,"conns":0,"heapMB":2.1}`. Then open `https://demo.example.com` and start a room. If something is wrong:

```
docker compose -f deploy/docker-compose.yml --env-file .env logs caddy engine engine-demo
```

The demo engine logs `demo mode, rooms last 5m0s` when it started correctly.

## 9. Backups

`deploy/backup.sh` copies the engine's SQLite database to `/srv/backups/collab-YYYY-MM-DD.db` and deletes copies older than 14 days.

```
/srv/collab/deploy/backup.sh
ls -l /srv/backups
```

Then run it every night (`sudo crontab -e`):

```
0 3 * * * /srv/collab/deploy/backup.sh >> /var/log/collab-backup.log 2>&1
```

**Test a restore before you need one.** Check that the latest backup opens and is intact:

```
docker run --rm -v /srv/backups:/backups alpine sh -c \
  "apk add --no-cache sqlite >/dev/null && sqlite3 /backups/collab-$(date +%F).db 'PRAGMA integrity_check; .tables'"
```

To actually restore, stop the engine, put the file back and start it again:

```
docker compose -f deploy/docker-compose.yml --env-file .env stop engine
docker run --rm -v collab_engine-data:/data -v /srv/backups:/backups alpine \
  sh -c "cp /backups/collab-YYYY-MM-DD.db /data/collab.db && rm -f /data/collab.db-wal /data/collab.db-shm"
docker compose -f deploy/docker-compose.yml --env-file .env start engine
```

The demo has nothing to back up.

## 10. Monitoring

Point an external uptime checker (UptimeRobot, Better Stack, ...) at both `/healthz` URLs, with an alert to your email or phone. Checking from outside also catches DNS and certificate problems that a check on the server can't see.

## 11. Updating

```
cd /srv/collab
git pull
docker compose -f deploy/docker-compose.yml --env-file .env up -d --build
```

Both engines restart. Open demo rooms end when the demo engine restarts (nothing is stored), and clients of the main engine reconnect on their own.

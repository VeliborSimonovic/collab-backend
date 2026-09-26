#!/usr/bin/env bash
# Backs up the engine's SQLite database to /srv/backups and prunes old copies.
# The engine image is `scratch` and has no sqlite3, so a throwaway alpine container does the work.
set -euo pipefail

VOLUME="${VOLUME:-collab_engine-data}"
DEST="${DEST:-/srv/backups}"
KEEP_DAYS=14
FILE="collab-$(date +%F).db"

mkdir -p "$DEST"

docker run --rm \
  -v "$VOLUME":/data \
  -v "$DEST":/backups \
  alpine sh -c "
    set -e
    apk add --no-cache sqlite >/dev/null
    sqlite3 /data/collab.db \".backup '/backups/$FILE.tmp'\"
    [ \"\$(sqlite3 /backups/$FILE.tmp 'PRAGMA integrity_check;')\" = ok ]
    mv /backups/$FILE.tmp /backups/$FILE
  "

find "$DEST" -name 'collab-*.db' -mtime +"$KEEP_DAYS" -delete
echo "backup written: $DEST/$FILE"

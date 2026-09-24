#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
HOST="${1:-localhost:8080}"

go build -o /tmp/collabd ./cmd/collabd

mint() { /tmp/collabd token -key private.pem -sub "$1" -name "$2" -color "$3" -doc "$4" -role "$5" -ttl 8h; }

while read -r sub name color doc role; do
  printf '%-6s %-5s %-7s http://%s/?doc=%s&token=%s\n' \
    "$name" "$doc" "$role" "$HOST" "$doc" "$(mint "$sub" "$name" "$color" "$doc" "$role")"
done <<'ROWS'
alice Alice #e11d48 doc1 editor
alice Alice #e11d48 doc2 editor
dave  Dave  #d97706 doc1 editor
bob   Bob   #2563eb doc1 viewer
carol Carol #16a34a doc2 editor
ROWS

echo
echo "Should be REFUSED (403): Carol's doc2 token used on doc1"
echo "DENIED http://$HOST/?doc=doc1&token=$(mint carol Carol '#16a34a' doc2 editor)"

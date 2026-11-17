#!/bin/sh
# Daily custom-format dumps of the scout database.
# A failed dump is removed and retried in five minutes.
# Dumps older than 14 days are deleted after a successful dump.

set -u

while true; do
  stamp=$(date -u +%Y%m%dT%H%M%SZ)
  target="/backups/scout-${stamp}.dump"
  if pg_dump --format=custom --file="$target"; then
    find /backups -name 'scout-*.dump' -mtime +14 -delete
    sleep 86400
  else
    rm -f "$target"
    sleep 300
  fi
done

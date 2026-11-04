#!/bin/sh
# Hand the cache volume and the output dir to TVPL_UID:TVPL_GID (the
# caller's uid via scripts/tvpl/run.sh, so output files on the host are
# theirs), then run the converter as that user with no capabilities. Never
# as root.
set -eu
uid="${TVPL_UID:-10001}"
gid="${TVPL_GID:-10001}"
case "$uid$gid" in ''|*[!0-9]*) echo "TVPL_UID/TVPL_GID must be numeric" >&2; exit 2;; esac
# numeric compare: "00" is root too
if [ "$uid" -eq 0 ] || [ "$gid" -eq 0 ]; then echo "refusing to run as root" >&2; exit 2; fi
mkdir -p "$TVPL_CACHE_DIR" "$TVPL_OUT_DIR"
# one run at a time per cache volume, taken BEFORE re-owning it so a second
# caller cannot take the files from a run in progress; fd 9 stays open in
# the converter (Python closes it for the browser)
exec 9>"$TVPL_CACHE_DIR/.run.lock"
flock -n 9 || { echo "STOP: another TVPL run is using the cache; wait for it to finish" >&2; exit 1; }
chown -R "$uid:$gid" "$TVPL_CACHE_DIR"
chmod 700 "$TVPL_CACHE_DIR"
# only the directory: never re-own a whole host tree given by mistake
chown "$uid:$gid" "$TVPL_OUT_DIR"
export HOME=/data
drop="setpriv --reuid=$uid --regid=$gid --clear-groups --no-new-privs --bounding-set=-all --inh-caps=-all"
if [ "${1:-}" = test ]; then
  shift
  exec $drop python3 -I -m unittest discover -v -s /app -p "test_*.py" "$@"
fi
exec $drop python3 -I /app/tvpl_push.py "$@"

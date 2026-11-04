#!/bin/sh
# Run the TVPL converter in its container (compose service "tvpl").
#   scripts/tvpl/run.sh --login 123/2020/NĐ-CP      # push to the review queue
#   scripts/tvpl/run.sh --dry-run 78/2021/TT-BTC    # files only
#   scripts/tvpl/run.sh test                        # unit tests
# Env passed through (never written anywhere): WERAG_API_KEY or WERAG_TOKEN,
# WERAG_API (default http://host.docker.internal:18080/api/v1), TVPL_USER,
# TVPL_PASS. Output: $TVPL_OUT_DIR (default ./data/tvpl-out), owned by you.
set -eu
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
# compose also fills these from .env: secrets must come from the shell only
if [ -f .env ] && grep -Eq '^[[:space:]]*(export[[:space:]]+)?(TVPL_PASS|TVPL_USER|WERAG_TOKEN|WERAG_API_KEY)[[:space:]]*=' .env; then
  echo "STOP: remove TVPL_PASS/TVPL_USER/WERAG_TOKEN/WERAG_API_KEY from .env (secrets come from your shell only)" >&2
  exit 2
fi
# under sudo (docker needs it for some users) the files still belong to you
export TVPL_UID="${SUDO_UID:-$(id -u)}" TVPL_GID="${SUDO_GID:-$(id -g)}"
export TVPL_OUT_DIR="$(realpath -m "${TVPL_OUT_DIR:-$root/data/tvpl-out}")"
if [ "$(id -u)" -eq 0 ]; then
  setpriv --reuid="$TVPL_UID" --regid="$TVPL_GID" --clear-groups mkdir -p "$TVPL_OUT_DIR"
else
  mkdir -p "$TVPL_OUT_DIR"
fi
exec docker compose --profile tvpl run --rm --build tvpl "$@"

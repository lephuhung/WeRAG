#!/bin/sh
# Starts the Next.js server on loopback and nginx in front of it. Either
# process exiting stops the container so Docker's restart policy kicks in.
set -eu

FILE_MB=${MAX_FILE_SIZE_MB:-50}
SKILL_MB=${MAX_SKILL_BUNDLE_SIZE_MB:-256}
if [ "$SKILL_MB" -lt "$FILE_MB" ] 2>/dev/null; then
  SKILL_MB=$FILE_MB
fi
if [ "$SKILL_MB" -gt 512 ] 2>/dev/null; then
  SKILL_MB=512
fi

# Two body limits on purpose: skill zip uploads get the larger one, every
# other endpoint (KB uploads included) keeps MAX_FILE_SIZE.
export MAX_FILE_SIZE=${FILE_MB}M
export MAX_SKILL_BUNDLE_SIZE=${SKILL_MB}M
export APP_HOST=${APP_HOST:-app}
export APP_PORT=${APP_PORT:-8080}
export APP_SCHEME=${APP_SCHEME:-http}
export NEXT_PORT=${NEXT_PORT:-3000}
envsubst '${MAX_FILE_SIZE} ${MAX_SKILL_BUNDLE_SIZE} ${APP_HOST} ${APP_PORT} ${APP_SCHEME} ${NEXT_PORT}' \
  < /etc/nginx/templates/default.conf.template > /etc/nginx/http.d/default.conf

HOSTNAME=127.0.0.1 PORT="$NEXT_PORT" node /app/server.js &
next_pid=$!
nginx -g 'daemon off;' &
nginx_pid=$!

trap 'kill -TERM "$next_pid" "$nginx_pid" 2>/dev/null' TERM INT
# Wait for whichever exits first, then take the other one down too.
while kill -0 "$next_pid" 2>/dev/null && kill -0 "$nginx_pid" 2>/dev/null; do
  sleep 2
done
kill -TERM "$next_pid" "$nginx_pid" 2>/dev/null || true
wait
exit 1

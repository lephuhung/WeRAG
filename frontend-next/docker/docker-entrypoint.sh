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
export ASR_URL=${ASR_URL:-http://sherpa-asr:8110}
# Docker's embedded DNS; the /asr/ location resolves its upstream per request.
export DNS_RESOLVER=${DNS_RESOLVER:-127.0.0.11}
export ONLYOFFICE_URL=${ONLYOFFICE_URL:-http://documentserver}
envsubst '${MAX_FILE_SIZE} ${MAX_SKILL_BUNDLE_SIZE} ${APP_HOST} ${APP_PORT} ${APP_SCHEME} ${NEXT_PORT} ${ASR_URL} ${DNS_RESOLVER} ${ONLYOFFICE_URL}' \
  < /etc/nginx/templates/default.conf.template > /etc/nginx/http.d/default.conf

# REAL_IP_FROM: comma-separated IPs/CIDRs of a load balancer in front of
# this container. Its X-Forwarded-For is then trusted for $remote_addr,
# which nginx passes on to the backend (per-IP limits). Empty: nginx is the
# edge and the TCP peer is the client.
REAL_IP_CONF=/etc/nginx/http.d/01-werag-real-ip.conf
rm -f "$REAL_IP_CONF"
if [ -n "${REAL_IP_FROM:-}" ]; then
  for cidr in $(echo "$REAL_IP_FROM" | tr ',' ' '); do
    echo "set_real_ip_from $cidr;" >> "$REAL_IP_CONF"
  done
  echo "real_ip_header X-Forwarded-For;" >> "$REAL_IP_CONF"
  echo "real_ip_recursive on;" >> "$REAL_IP_CONF"
fi

# X-Forwarded-Proto sent upstream: behind a trusted load balancer (which
# terminates TLS) its header wins, otherwise this nginx's own scheme.
PROTO_CONF=/etc/nginx/http.d/02-werag-forwarded-proto.conf
if [ -n "${REAL_IP_FROM:-}" ]; then
  cat > "$PROTO_CONF" <<'EOF'
map $http_x_forwarded_proto $werag_forwarded_proto {
    default $http_x_forwarded_proto;
    ''      $scheme;
}
EOF
else
  cat > "$PROTO_CONF" <<'EOF'
map $scheme $werag_forwarded_proto {
    default $scheme;
}
EOF
fi

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

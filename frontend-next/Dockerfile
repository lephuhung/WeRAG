# frontend-next production image: Next.js standalone server behind nginx.
# nginx proxies /api, /files and /r straight to the Go backend and everything
# else to Next on 127.0.0.1:3000 (see docker/nginx.conf).

FROM node:22-alpine AS builder
WORKDIR /app
ARG NPM_REGISTRY=
COPY package.json package-lock.json ./
RUN if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi \
 && npm ci --no-audit --no-fund
COPY . .
# Rewrites are baked into the build. In the container nginx serves /api and
# /files itself, so this only matters for requests that reach Next directly.
ARG WERAG_BACKEND_URL=http://app:8080
ARG NODE_MAX_OLD_SPACE_SIZE=4096
ENV WERAG_BACKEND_URL=${WERAG_BACKEND_URL} \
    NEXT_OUTPUT_STANDALONE=1 \
    NEXT_TELEMETRY_DISABLED=1 \
    NODE_OPTIONS=--max-old-space-size=${NODE_MAX_OLD_SPACE_SIZE}
RUN npm run build

FROM node:22-alpine
RUN apk add --no-cache nginx gettext
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1
COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public
COPY docker/nginx.conf /etc/nginx/templates/default.conf.template
# Outside templates/ on purpose: envsubst would eat $host and the other
# nginx variables in them.
COPY docker/nginx-api-proxy.conf /etc/nginx/api-proxy.conf
COPY docker/nginx-http.conf /etc/nginx/http.d/00-werag-http.conf
COPY docker/docker-entrypoint.sh /docker-entrypoint.sh
ENV MAX_FILE_SIZE_MB=50
EXPOSE 80
ENTRYPOINT ["/docker-entrypoint.sh"]

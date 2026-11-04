# TVPL → WeRAG converter (scripts/tvpl) with a pinned Lightpanda browser.
# Run on demand, never resident:  scripts/tvpl/run.sh --login 123/2020/NĐ-CP
# The session cookies live in the tvpl_cache volume; the TVPL password is
# never stored (env only, see scripts/tvpl/README.md).
FROM python:3.12-slim

ARG LIGHTPANDA_VERSION=1.0.2
# GitHub publishes no checksums: this is the hash of the binary downloaded
# and tested on 2026-11-04 (trust on first use); change both together
ARG LIGHTPANDA_SHA256=ae7d01d261efb9e480e18ce3424533332883863bcf70b12a4321b5733f37e19e

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl \
 && curl -fsSL -o /usr/local/bin/lightpanda \
      "https://github.com/lightpanda-io/browser/releases/download/${LIGHTPANDA_VERSION}/lightpanda-x86_64-linux" \
 && echo "${LIGHTPANDA_SHA256}  /usr/local/bin/lightpanda" | sha256sum -c - \
 && chmod 755 /usr/local/bin/lightpanda \
 && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

RUN pip install --no-cache-dir beautifulsoup4==4.13.4

RUN useradd --system --uid 10001 --home-dir /data tvpl \
 && mkdir -p /data/cache /data/out && chown -R tvpl /data && chmod 700 /data/cache

WORKDIR /app
COPY --from=tvplsrc tvpl_fetch.py tvpl_convert.py tvpl_push.py test_tvpl_convert.py ./
COPY entrypoint.sh ./

ENV TVPL_CACHE_DIR=/data/cache \
    TVPL_OUT_DIR=/data/out \
    LIGHTPANDA_BIN=/usr/local/bin/lightpanda \
    LIGHTPANDA_DISABLE_TELEMETRY=true \
    PYTHONDONTWRITEBYTECODE=1

# starts as root only to hand the mounted dirs to TVPL_UID, then drops to it
ENTRYPOINT ["sh", "/app/entrypoint.sh"]

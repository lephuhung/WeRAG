# Đồng bộ văn bản của Đảng từ hai cổng dangcongsan theo lịch (supercronic):
# tải trang danh sách mới → phân loại → dựng HTML → đẩy lên WeRAG.
# Build context là thư mục gốc repo (cần scripts/dangcongsan).
FROM python:3.12-slim-bookworm
ARG SUPERCRONIC_URL=https://github.com/aptible/supercronic/releases/download/v0.2.49/supercronic-linux-amd64
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata p7zip-full \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL -o /usr/local/bin/supercronic "$SUPERCRONIC_URL" \
    && chmod 0755 /usr/local/bin/supercronic \
    && pip install --no-cache-dir pymupdf==1.26.* beautifulsoup4==4.13.* lxml==6.* olefile==0.47

COPY scripts/dangcongsan/dcs_fetch.py scripts/dangcongsan/dcs_classify.py scripts/dangcongsan/dcs_text.py \
     scripts/dangcongsan/dcs_build.py scripts/dangcongsan/dcs_push.py /app/
COPY deploy/dcs-sync/sync.sh deploy/dcs-sync/entrypoint.sh /usr/local/bin/
RUN chmod 0755 /usr/local/bin/sync.sh /usr/local/bin/entrypoint.sh

ENV TZ=Asia/Ho_Chi_Minh PYTHONDONTWRITEBYTECODE=1

# Healthy khi lần đồng bộ thành công gần nhất trong vòng 1 ngày.
HEALTHCHECK --interval=30m --timeout=10s --start-period=1h \
    CMD find "$DCS_WORK_DIR/.sync.ok" -mmin -${DCS_HEALTH_MAX_AGE_MIN:-1440} | grep -q . || [ ! -f "$DCS_WORK_DIR/.sync.started" ]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

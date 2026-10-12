# Đồng bộ danh mục văn bản pháp luật từ vbpl.vn theo lịch (supercronic):
# crawl cập nhật thêm → import vào Postgres → đẩy dữ liệu crawl lên MinIO.
# Build context là thư mục gốc repo (cần cmd/ và scripts/).
FROM golang:1.26-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
# go.mod replaces anydoc with ./third_party/anydoc-go (as in docker/Dockerfile.app).
COPY third_party/anydoc-go/go.mod third_party/anydoc-go/go.mod
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# internal/utils dùng pg_query (cgo), nên importer build có CGO như image app;
# internal/types nạp từ điển gojieba lúc init, nên mang theo thư mục dict.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/legal-catalog-import ./cmd/legal-catalog-import \
    && cp -r "$(go list -m -f '{{.Dir}}' github.com/yanyiwu/gojieba)/deps/cppjieba/dict" /out/jieba-dict
# dl.min.io no longer serves community mc binaries (410): build it from source.
ARG MC_VERSION=latest
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOBIN=/out go install -trimpath -ldflags="-s -w" github.com/minio/mc@${MC_VERSION}

FROM debian:12.12-slim
ARG SUPERCRONIC_URL=https://github.com/aptible/supercronic/releases/download/v0.2.49/supercronic-linux-amd64
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 ca-certificates curl tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL -o /usr/local/bin/supercronic "$SUPERCRONIC_URL" \
    && chmod 0755 /usr/local/bin/supercronic

COPY --from=builder /out/legal-catalog-import /out/mc /usr/local/bin/
COPY --from=builder /out/jieba-dict /usr/share/jieba-dict
COPY scripts/legal_catalog/crawl_vbpl.py /app/crawl_vbpl.py
COPY deploy/vbpl-sync/sync.sh deploy/vbpl-sync/entrypoint.sh /usr/local/bin/
RUN chmod 0755 /usr/local/bin/sync.sh /usr/local/bin/entrypoint.sh

ENV HOME=/work MC_CONFIG_DIR=/tmp/.mc TZ=Asia/Ho_Chi_Minh JIEBA_DICT_DIR=/usr/share/jieba-dict
WORKDIR /work

# Healthy khi lần đồng bộ thành công gần nhất trong vòng 2 ngày.
HEALTHCHECK --interval=30m --timeout=10s --start-period=1h \
    CMD find /work/.sync.ok -mmin -${VBPL_HEALTH_MAX_AGE_MIN:-2880} | grep -q . || [ ! -f /work/.sync.started ]

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

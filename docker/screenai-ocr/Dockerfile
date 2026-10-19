# Chrome Screen AI OCR behind an OpenAI-compatible API (see README.md).
# The Google component is NOT part of the image: the entrypoint downloads it
# into the /component volume on first start.
FROM python:3.12-slim AS stub
RUN apt-get update && apt-get install -y --no-install-recommends g++ && rm -rf /var/lib/apt/lists/*
RUN printf 'namespace threadlogger { void EnableThreadedLogging(int) {} }\n' > /tmp/stub.cc \
 && g++ -shared -fPIC -O2 -o /tmp/libthreadlogger_stub.so /tmp/stub.cc

FROM python:3.12-slim
RUN pip install --no-cache-dir fastapi==0.146.0 "uvicorn[standard]==0.54.0" pillow==13.0.0 numpy==2.5.4 requests==2.34.2
RUN useradd --system --uid 10001 --home-dir /app screenai \
 && mkdir -p /component && chown screenai /component
COPY --from=stub /tmp/libthreadlogger_stub.so /opt/screenai/libthreadlogger_stub.so
WORKDIR /app
COPY server.py screenai.py fetch_component.py entrypoint.sh ./
ENV SCREENAI_COMPONENT_DIR=/component \
    SCREENAI_STUB=/opt/screenai/libthreadlogger_stub.so \
    SCREENAI_WORKERS=2
USER screenai
EXPOSE 8004
HEALTHCHECK --interval=30s --timeout=5s --start-period=180s --retries=3 \
  CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8004/health', timeout=4)"
ENTRYPOINT ["sh", "/app/entrypoint.sh"]

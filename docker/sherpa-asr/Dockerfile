FROM python:3.12-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir sherpa-onnx==1.13.8 fastapi==0.143.3 "uvicorn[standard]==0.54.0" httpx python-multipart numpy onnxruntime==1.31.0 tokenizers==0.23.3
WORKDIR /app
COPY server.py capu.py ./
EXPOSE 8110
CMD ["uvicorn", "server:app", "--host", "0.0.0.0", "--port", "8110", "--no-access-log"]

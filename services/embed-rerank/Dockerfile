# Reuse the already-deployed CUDA/PyTorch runtime to avoid storing another
# multi-gigabyte CUDA image alongside the OCR model server.
FROM vllm/vllm-openai:v0.27.1

COPY services/embed-rerank/requirements.txt /tmp/embed-rerank-requirements.txt
RUN /usr/bin/python3 -m pip install --no-cache-dir -r /tmp/embed-rerank-requirements.txt \
    && rm /tmp/embed-rerank-requirements.txt

WORKDIR /app/embed-rerank
COPY services/embed-rerank/app.py /app/embed-rerank/app.py
COPY docker/adapters/openai_embed_rerank.py /adapter/openai_adapter.py

ENV PYTHONPATH=/app/embed-rerank
EXPOSE 8090

ENTRYPOINT ["/usr/bin/python3", "-m", "uvicorn"]
CMD ["app:app", "--host", "0.0.0.0", "--port", "8090"]

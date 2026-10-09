FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app/ app/
COPY proto/ proto/
COPY main.py .
# Baked-in bundles so the service works standalone; mount a volume over
# /app/models (or set MINIO_ENDPOINT) to serve newer versions.
COPY models/ models/

ENV MODEL_DIR=/app/models GRPC_PORT=50051 HTTP_PORT=8000 PYTHONUNBUFFERED=1
EXPOSE 50051 8000

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:8000/health').status==200 else 1)"

CMD ["python", "main.py"]

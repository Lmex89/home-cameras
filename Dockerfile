# syntax=docker/dockerfile:1

# ===== Builder stage: create wheels =====
FROM python:3.12-alpine AS builder

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

RUN apk add --no-cache \
    build-base

WORKDIR /build

COPY requirements.txt ./requirements.txt

RUN pip install --upgrade pip setuptools wheel && \
    pip wheel --disable-pip-version-check --no-cache-dir \
        --wheel-dir /wheels \
        -r requirements.txt

# ===== Final runtime stage =====
FROM python:3.12-alpine

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN apk add --no-cache ffmpeg && \
    adduser -D -s /sbin/nologin appuser

WORKDIR /app

COPY --from=builder /wheels /wheels
COPY requirements.txt /tmp/requirements.txt

RUN pip install --no-cache-dir --no-compile --no-index \
    --find-links=/wheels -r /tmp/requirements.txt && \
    rm -rf /wheels /tmp/requirements.txt

COPY --chown=appuser:appuser . /app

RUN mkdir -p /app/data /app/data/snapshots && \
    chown -R appuser:appuser /app/data

EXPOSE 8000

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]

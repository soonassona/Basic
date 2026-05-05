# syntax=docker/dockerfile:1.7
# Phase 1 image is intentionally slim — no torch/onnxruntime yet. Phase 3
# adds an `ml` stage that installs the optional ML deps on top.

FROM python:3.13-slim AS base
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY pyproject.toml ./
RUN pip install --upgrade pip && pip install ".[dev]"

COPY app ./app
COPY tests ./tests

EXPOSE 8000
USER 1000:1000
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8000"]

# VisionLoop ML Service

FastAPI inference service. Phase 1 boots the surface area (health,
models registry, inference stub returning 501); Phase 3 wires SAM 2.1
and YOLOv11x.

## Local development

```bash
python -m venv .venv && source .venv/bin/activate
pip install -e ".[dev]"
uvicorn app.main:app --reload --port 8000

# Tests
pytest

# Lint / typecheck
ruff check .
mypy app
```

"""Smoke tests for the Phase 1 surface area.

Verifies the service boots and that contract surfaces (health, models,
infer-stub) match what the API gateway expects."""

from __future__ import annotations

from fastapi.testclient import TestClient

from app.main import create_app


def make_client() -> TestClient:
    return TestClient(create_app())


def test_liveness_returns_ok() -> None:
    with make_client() as client:
        response = client.get("/health")
    assert response.status_code == 200
    body = response.json()
    assert body["status"] == "ok"
    assert body["service"] == "visionloop-ai"
    assert "uptime_seconds" in body


def test_readiness_returns_ready() -> None:
    with make_client() as client:
        response = client.get("/ready")
    assert response.status_code == 200
    assert response.json() == {"status": "ready"}


def test_models_registry_contains_sam_and_yolo() -> None:
    with make_client() as client:
        response = client.get("/v1/models")
    assert response.status_code == 200
    families = {m["family"] for m in response.json()["models"]}
    assert families == {"sam", "yolo"}


def test_infer_returns_501_with_typed_envelope() -> None:
    with make_client() as client:
        response = client.post(
            "/v1/infer",
            json={
                "job_id": "00000000-0000-0000-0000-000000000001",
                "job_type": "auto",
                "image_url": "https://example.test/x.png",
            },
        )
    assert response.status_code == 501
    body = response.json()
    assert body["error"]["code"] == "not_implemented"
    assert body["error"]["phase"] == 3

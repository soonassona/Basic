"""Runtime configuration for the AI service.

Section 18: validate at service startup, fail fast on mismatch. Pydantic
settings raises a ValidationError on first instantiation if a required
value is missing — we let that propagate.
"""

from __future__ import annotations

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore", case_sensitive=False)

    http_port: int = Field(default=8000, alias="AI_HTTP_PORT")
    log_level: str = Field(default="info", alias="AI_LOG_LEVEL")
    rabbitmq_url: str = Field(default="amqp://localhost:5672/", alias="RABBITMQ_URL")
    redis_url: str = Field(default="redis://localhost:6379/1", alias="REDIS_URL")

    api_callback_url: str = Field(
        default="http://api:8080/internal/jobs/callback",
        alias="API_CALLBACK_URL",
    )
    api_callback_token: str = Field(default="changeme", alias="API_CALLBACK_TOKEN")

    # Worker → API callback auth (matches apps/api JOB_CALLBACK_TOKEN /
    # JOB_CALLBACK_HMAC_SECRET, see ADR-0004). Defaults match apps/api/.env
    # so docker-compose works out of the box.
    job_callback_token: str = Field(
        default="dev-only-callback-token-32-bytes-padding-aa",
        alias="JOB_CALLBACK_TOKEN",
    )
    job_callback_hmac_secret: str = Field(
        default="dev-only-callback-hmac-32-bytes-padding-bb",
        alias="JOB_CALLBACK_HMAC_SECRET",
    )

    model_dir: str = Field(default="/models", alias="MODEL_DIR")
    onnx_providers: str = Field(default="CPUExecutionProvider", alias="ONNX_PROVIDERS")

    otlp_endpoint: str = Field(default="", alias="OTEL_EXPORTER_OTLP_ENDPOINT")
    service_name: str = Field(default="visionloop-ai", alias="OTEL_SERVICE_NAME")


def load_settings() -> Settings:
    return Settings()

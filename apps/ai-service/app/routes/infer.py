"""Phase 1 placeholder for the inference endpoint.

Returns 501 with a typed envelope so consumers can detect the pending
implementation without parsing free-form error strings. Phase 3 swaps
this for the real SAM 2.1 / YOLOv11 router."""

from __future__ import annotations

from fastapi import APIRouter
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

router = APIRouter(tags=["inference"])


class InferenceRequest(BaseModel):
    job_id: str = Field(..., description="UUID of the job submitted via the API gateway.")
    job_type: str = Field(..., description="auto | box | points | polygon | detect")
    image_url: str = Field(..., description="Presigned R2 URL the worker can fetch.")


@router.post("/infer", status_code=501)
def infer(_: InferenceRequest) -> JSONResponse:
    return JSONResponse(
        status_code=501,
        content={
            "error": {
                "code": "not_implemented",
                "message": "Inference is wired in Phase 3 (see implementation phases).",
                "phase": 3,
            }
        },
    )

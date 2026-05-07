"""Inference endpoint — wired in Phase 3 (spec §5 Model Routing Logic).

POST /v1/infer dispatches to the model router:
  auto    → YOLOv11 → SAM 2.1
  box     → SAM 2.1
  points  → SAM 2.1
  polygon → manual (no AI)
  detect  → YOLOv11 only
  default → SAM 2.1
"""

from __future__ import annotations

from fastapi import APIRouter
from pydantic import BaseModel, Field

from ..workers.model_router import run_routed_inference

router = APIRouter(tags=["inference"])


class InferenceRequest(BaseModel):
    job_id: str = Field(..., description="UUID of the job submitted via the API gateway.")
    job_type: str = Field(..., description="auto | box | points | polygon | detect")
    image_url: str = Field(..., description="Presigned R2 URL the worker can fetch.")


@router.post("/infer")
def infer(req: InferenceRequest) -> dict:
    envelope = {
        "job_id": req.job_id,
        "type": req.job_type,
        "image_url": req.image_url,
    }
    return run_routed_inference(envelope)

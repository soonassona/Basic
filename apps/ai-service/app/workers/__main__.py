"""Entrypoint for `python -m app.workers` — runs the jobs consumer."""

from __future__ import annotations

from .jobs_consumer import run

if __name__ == "__main__":
    run()

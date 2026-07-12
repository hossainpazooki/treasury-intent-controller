"""FastAPI service: POST /ml/evaluate + GET /healthz (CONTRACT-SCORER §S.1/§S.4).

Service errors are still evaluations: for a well-formed request every failure —
unknown criterion, missing fact, resolver failure, internal exception — answers
200 with UNEVALUABLE. Non-2xx is reserved for malformed requests (FastAPI's 422)
and infrastructure; the Go client maps those to Unevaluable anyway, so both
paths fail closed (spec invariant 2 across the network).
"""

import asyncio

from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

from .evaluator import evaluate
from .facts import DEMO_FACTS, FactSource, StaticFactSource
from .models import EvalRequest, EvalResponse
from .resolver import ArtifactResolver, NullResolver

SKIP_NOTE = "resolver=null: verification skipped; "


def create_app(
    facts: FactSource | None = None,
    resolver: ArtifactResolver | None = None,
) -> FastAPI:
    fact_source: FactSource = (
        facts if facts is not None else StaticFactSource(DEMO_FACTS)
    )
    artifact_resolver: ArtifactResolver = (
        resolver if resolver is not None else NullResolver()
    )

    app = FastAPI(title="treasury-intent-scorer")

    @app.get("/healthz")
    def healthz() -> PlainTextResponse:
        return PlainTextResponse("ok")

    @app.post("/ml/evaluate", response_model=EvalResponse, response_model_exclude_none=True)
    async def ml_evaluate(req: EvalRequest) -> EvalResponse:
        try:
            note = ""
            if req.rule_artifact_hash or req.intent_spec_hash:
                if isinstance(artifact_resolver, NullResolver):
                    # No resolver on this host: skip, but say so on the wire.
                    note = SKIP_NOTE
                else:
                    # verify() holds the GIL through crypto in the wheel-backed
                    # impl: run it off the event loop so a concurrent
                    # /ml/evaluate is never stalled (recorded GIL caveat).
                    ok = await asyncio.get_running_loop().run_in_executor(
                        None,
                        artifact_resolver.verify,
                        req.rule_artifact_hash,
                        req.intent_spec_hash,
                    )
                    if not ok:
                        return EvalResponse(
                            result="UNEVALUABLE", basis="artifact verification failed"
                        )
            resp = evaluate(req, fact_source.get(req.criterion, req.intent_id))
            if note:
                resp = EvalResponse(result=resp.result, basis=note + (resp.basis or ""))
            return resp
        except Exception as exc:  # noqa: BLE001 — the catch-all IS the contract:
            # every internal failure is an evaluation that answers UNEVALUABLE.
            return EvalResponse(
                result="UNEVALUABLE", basis=f"internal error: {type(exc).__name__}"
            )

    return app

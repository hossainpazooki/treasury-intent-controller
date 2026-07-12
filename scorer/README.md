# scorer — the intent layer's Python service

The Python side of the intent layer's scoring seam: one service = **resolver**
(artifact verify, injected) + **scorer** (fact vs threshold), answering
`POST /ml/evaluate` for the Go authorization gate at the root of this repo.

**The contract is the source of truth, not this README**: `../CONTRACT-SCORER.md`
(amending `CONTRACT.md` + `CONTRACT-DURABILITY.md`). The golden wire fixtures in
`../contract/scorer/` are the ONLY surface shared with the Go side — neither
side imports the other.

## Behavior (spec invariant 2, fail-closed, across the network)

Tri-state closed set `PASS | FAIL | UNEVALUABLE`. For a well-formed request the
service ALWAYS answers `200` — unknown criterion, missing fact, resolver
failure, internal exception are all evaluations that answer `UNEVALUABLE`.
Non-2xx is reserved for malformed requests (`422`) and infrastructure; the Go
client maps those to `Unevaluable` anyway. An outage can only ever make the
gate refuse.

`basis` is observability only: free text, never allowed into the gate's audit
log, durable feed, or any hash.

## Run / test

```bash
cd scorer
python -m venv .venv && .venv/Scripts/python -m pip install -e ".[dev]"
.venv/Scripts/python -m pytest          # unit + service matrix + wire fixtures
.venv/Scripts/python -m tis             # serve on 127.0.0.1:8000 (TIS_HOST/TIS_PORT)
```

Fixture tests find `../contract/scorer` by default (`TIC_CONTRACT_DIR`
overrides). Absent fixtures SKIP visibly — a skip is NOT a pass.

## Implemented vs planned

- **[built]** `EvalRequest`/`EvalResponse` models, evaluator, `StaticFactSource`,
  `NullResolver`, FastAPI app + service fail-closed matrix, wire-fixture
  byte-agreement tests.
- **[planned — next slice]** the wheel-backed `KeArtifactResolver`
  (`ke-artifact-py`, Linux/CI only; verify runs in an executor for the GIL
  caveat). Its pytest lane exists and skips visibly until the wheel is present.
- **[planned — later slice]** a live fact source. `StaticFactSource` is the
  demo configuration and is labeled as such; do not fake a live one.

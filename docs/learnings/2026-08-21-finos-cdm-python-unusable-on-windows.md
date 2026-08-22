# finos-cdm 7.1.0 cannot construct a WorkflowStep with an eventIdentifier, and that is NOT Windows-specific — Linux reproduces it byte-identically

ts: 2026-08-21T23:13:02Z
commit: f93148cfc8958092aaecbd2e4c186f98edd9a4aa + uncommitted working tree
session: 192b819d
status: verified
fact: The six-check `cdm_spike.py` probe was run under a throwaway venv on
both Linux (WSL, Python 3.12.3) and Windows (Python 3.14.2, short path
`C:\Users\hossa\AppData\Local\Temp\cdmv`), against `finos-cdm==7.1.0`. The
two runs produced the SAME two PASS / four FAIL pattern, with
byte-identical exception text on both failing checks that carry their own
error body (c2 and c5). This is decisive: the WorkflowStep/eventIdentifier
construction failure recorded 2026-08-21 as "Caveat C7" is NOT a
Windows/Python-3.14 artifact — it is a property of the `finos-cdm` 7.1.0
Python distribution itself. `c5` (`TradeState`) also FAILED identically on
both platforms, but that failure is NOT the distribution's — it is the
spike's own FIXTURE, which nests `product`/`tradeLot`/`counterparty` under
the pre-CDM-7 name `tradableProduct` while CDM 7's `Trade` requires them at
the top level (`Trade` required fields, measured directly: `product`,
`tradeLot`, `counterparty`, `tradeIdentifier`, `tradeDate`). "Field
required" is therefore the model correctly rejecting a stale-shaped
fixture; a distribution that ACCEPTED it would be the defect. `c5` is a
known-bad fixture and must not be counted as a package property, nor
re-derived as one by a later reader.
`c1` (bundle-first import) and `c6` (`Qualify_*` signature) PASS on both
platforms; `c3` and `c4` FAIL as a cascade of `c2` never setting `c2.ws`
(expected, not a new defect). Decision: the CDM-validated test lane for
Tasks 10-11 stays SKIPPED on both Linux and Windows — six-PASS is required
and neither platform reaches it.
basis: Linux (WSL bash, `python3 --version` -> "Python 3.12.3"; venv Python
also 3.12.3, built via `python3 -m venv --without-pip /tmp/cdmv2 && pip3
--python /tmp/cdmv2/bin/python3 install -q finos-cdm==7.1.0` after the
brief's literal `python3 -m venv` command failed with "ensurepip is not
available" — no passwordless sudo to install `python3.12-venv` in this WSL
image, so pip was targeted at the venv interpreter instead of bootstrapped
inside it; same effective throwaway venv, package installed the same way):
```
PASS bundle-first import
FAIL WorkflowStep.model_validate with eventIdentifier :: 1 validation error for WorkflowStep
eventIdentifier.0.assignedIdentifier
  Input should be None [type=none_required, input_value=[{'identifier': 'per-acto...run:report-submit:abc'}], input_type=list]

FAIL rune_serialize / rune_deserialize roundtrip :: AttributeError("'function' object has no attribute 'ws'")
FAIL validate_model :: AttributeError("'function' object has no attribute 'ws'")
FAIL TradeState with a UTI tradeIdentifier :: 4 validation errors for finos_cdm_event_common_TradeState
trade.product
  Field required [type=missing, input_value={'tradeIdentifier': [{'id...[], 'counterparty': []}}, input_type=dict]
    For furth
PASS Qualify_* signature
```
Windows (`C:/Users/hossa/AppData/Local/Temp/cdmv/Scripts/python --version`
-> "Python 3.14.2", same pinned `finos-cdm==7.1.0` venv referenced by the
original 2026-08-21 baseline):
```
PASS bundle-first import
FAIL WorkflowStep.model_validate with eventIdentifier :: 1 validation error for WorkflowStep
eventIdentifier.0.assignedIdentifier
  Input should be None [type=none_required, input_value=[{'identifier': 'per-acto...run:report-submit:abc'}], input_type=list]

FAIL rune_serialize / rune_deserialize roundtrip :: AttributeError("'function' object has no attribute 'ws'")
FAIL validate_model :: AttributeError("'function' object has no attribute 'ws'")
FAIL TradeState with a UTI tradeIdentifier :: 4 validation errors for finos_cdm_event_common_TradeState
trade.product
  Field required [type=missing, input_value={'tradeIdentifier': [{'id...[], 'counterparty': []}}, input_type=dict]
    For furth
PASS Qualify_* signature
```
The two blocks are identical line-for-line, including the truncated
exception text (the spike truncates each exception's `repr()` to 200
chars, which is why both bodies cut off at "For furth").
Controller's independent reattribution evidence for `c5`, measured on Linux
(python 3.12.3, finos-cdm 7.1.0), verbatim:
```
TradeState fields: {'trade': 'REQ', 'state': 'opt', 'resetHistory': 'opt', 'transferHistory': 'opt', 'observationHistory': 'opt', 'valuationHistory': 'opt'}
Trade REQUIRED fields: ['product', 'tradeLot', 'counterparty', 'tradeIdentifier', 'tradeDate']
```
re-verify: `C:/Users/hossa/AppData/Local/Temp/cdmv/Scripts/python C:/Users/hossa/AppData/Local/Temp/claude/C--Users-hossa-dev/741f21a6-e1b7-46b8-9287-ee646e37376d/scratchpad/cdm_spike.py`
lesson: A single-platform "Windows-specific" hypothesis for a third-party
package failure should be treated as unverified until a second platform is
actually measured — the original 2026-08-21 note filed the WorkflowStep
failure as Windows/py3.14 Caveat C7 without a cross-platform check, and the
natural next step (assume Linux fixes it, unlock the CDM-validated lane)
would have been wrong. The counterpart discipline, learned in this same
run, is that a FAIL must be ATTRIBUTED before it is counted. This entry first
recorded `c5` as a second package defect when it is a bad fixture (above),
and the controller's own first Linux attempt produced six
`ModuleNotFoundError` FAILs from a venv that never installed anything
(`ensurepip` absent) — six FAILs that measured the environment, not the
package, and were discarded rather than reported. An unattributed FAIL can
manufacture a finding as easily as an unmeasured hypothesis can hide one.
The CDM-validated test lane for Tasks 10-11 must stay
SKIPPED with this entry as the reason; Tasks 10-11 proceed on their pure,
dict-shaped, stdlib-only path regardless, per the task's decision gate.

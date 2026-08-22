"""Regulatory-reporting adapter (CONTRACT.md section 2.7, "Regulatory-
reporting adapter"; claim 18 of the published SDK's section 5.4 table --
claim numbers differ between the two repos by design; section 2.7 resolves
in both).

Gates an agent's regulatory-report SUBMISSIONS through the plane. Stdlib-
only: this module adds NO dependency exception (the LangChain and MCP
adapters are the two sanctioned ones). The plane is PAYLOAD-BLIND by design,
so the idempotency key is a function of the report's regulatory IDENTITY --
never its content. The duplicate class this adapter exists to refuse is
"the same logical report submitted again with different bytes": a second
valuation for one UTI and one date is a silent data-quality defect a trade
repository will NOT reject, and it is exactly what a retry after a timeout
produces.

Key recipe (section 2.7 table): sorted-key compact JSON over EXACTLY the
fields the action type keys on, then `derive_key(scope, run_id,
"report-submit", bytes)`. A discriminator the action type does NOT key on
that is non-empty is REFUSED before any declaration: dropping it would let
two spellings of one report fork; keying it would let one report key twice.
"""

from __future__ import annotations

import datetime
import json
import unicodedata
import uuid
from dataclasses import dataclass
from typing import Callable, Mapping

from client import Client
from declare import Request, derive_key, intent_id
from gating import IntentRefused, require_fresh_proceed

# The tool name is a constant with no ':' in it: identity fields only ever
# enter the key through the sha256, so the key format's separator is never
# ambiguous here.
TOOL_NAME = "report-submit"

# CLOSED action-type table (section 2.7): action type -> the discriminators it
# keys on, beyond the five base fields. EMIR-Refit-shaped; the meanings are
# the author's reading and must be verified against the current ESMA
# validation rules before any production claim. Adding a type amends
# CONTRACT.md first, then this table.
ACTION_TYPES: dict[str, tuple[str, ...]] = {
    "NEWT": (),
    "MODI": ("event_ref",),
    "CORR": ("event_ref", "prior_ref"),
    "EROR": ("prior_ref",),
    "REVI": ("event_ref",),
    "TERM": ("event_ref",),
    "VALU": ("as_of",),
    "COLU": ("as_of",),
    "POSC": ("as_of",),
    "MARU": ("as_of",),
}

# Always keyed. on_behalf_of is keyed even when "" (self-reporting): delegated
# reporting for another entity is a different obligation, not a spelling.
_BASE_KEYED = ("reporting_entity", "uti", "action_type", "rule_set", "on_behalf_of")
_DISCRIMINATORS = ("as_of", "event_ref", "prior_ref")


class ReportUnkeyable(ValueError):
    """The identity cannot be keyed honestly. Raised BEFORE any declaration:
    nothing declared, nothing submitted, no durable record."""


@dataclass(frozen=True)
class ReportIdentity:
    """The regulatory identity of ONE report message. Content is not here on
    purpose (module docstring)."""

    reporting_entity: str   # LEI whose obligation this discharges
    uti: str
    action_type: str        # a key of ACTION_TYPES
    rule_set: str            # e.g. "ESMA-EMIR-REFIT-VR-1.4.0"; keyed on purpose (section 2.7)
    on_behalf_of: str = ""  # LEI under delegated reporting; "" when self-reporting
    as_of: str = ""          # ISO date for the daily action types
    event_ref: str = ""      # the firm's stable lifecycle-event id
    prior_ref: str = ""      # the submission being corrected / erased


def _is_iso_date(s: str) -> bool:
    """True only for a real calendar date in strict YYYY-MM-DD form.

    BOTH gates are load-bearing. The shape gate alone (length and dash
    positions) accepts impossible dates like "2026-13-45". The calendar gate
    alone (date.fromisoformat, 3.11+) accepts ISO 8601 BASIC ("20260801") and
    WEEK ("2026-W31-6") spellings, which name the same day as "2026-08-01"
    yet canonicalize to different bytes -- one report, three idempotency
    keys, which is the fail-OPEN fork this module exists to refuse (section
    2.7). Together they admit exactly one spelling of one real date."""
    if len(s) != 10 or s[4] != "-" or s[7] != "-":
        return False
    try:
        datetime.date.fromisoformat(s)
    except ValueError:
        return False
    return True


def canonical_identity(identity: ReportIdentity) -> bytes:
    """The bytes the idempotency key is derived from: sorted-key compact JSON
    over exactly the fields `identity.action_type` keys on. Raises
    ReportUnkeyable for anything that cannot be keyed honestly."""
    extra = ACTION_TYPES.get(identity.action_type)
    if extra is None:
        raise ReportUnkeyable(
            "unknown action_type %r; the closed set is %s"
            % (identity.action_type, sorted(ACTION_TYPES))
        )
    keyed = _BASE_KEYED + extra
    payload: dict[str, str] = {}
    for name in keyed:
        value = getattr(identity, name)
        if not isinstance(value, str):
            raise ReportUnkeyable("%r must be a string, got %s" % (name, type(value).__name__))
        if name != "on_behalf_of" and value == "":
            raise ReportUnkeyable(
                "%s requires %r; an absent identity field cannot be keyed honestly"
                % (identity.action_type, name)
            )
        # Identity fields are keyed VERBATIM (section 2.7): this module does
        # NOT normalize them on the caller's behalf. Leading/trailing
        # whitespace and non-NFC spellings are refused, not silently
        # accepted, because either would derive a DIFFERENT key from a
        # spelling a real system treats as the same identifier -- a fork,
        # and a fork here is fail-OPEN. Letter CASE is deliberately NOT
        # checked here: closing that is an operator ruling, not a library
        # default (CONTRACT.md section 2.7, RECORDED RESIDUAL -- case-
        # folding could merge two identifiers a scheme legitimately treats
        # as distinct, and a merge bricks a report forever since
        # reservations never expire).
        if value != value.strip():
            raise ReportUnkeyable(
                "%s has leading or trailing whitespace; identity fields are keyed "
                "verbatim, so a value must arrive already normalized (a fork here "
                "is fail-OPEN)" % name
            )
        if unicodedata.normalize("NFC", value) != value:
            raise ReportUnkeyable(
                "%s is not Unicode NFC; identity fields are keyed verbatim, so a "
                "value must arrive already normalized (a fork here is fail-OPEN)"
                % name
            )
        payload[name] = value
    for name in _DISCRIMINATORS:
        if name in keyed:
            continue
        value = getattr(identity, name)
        if not isinstance(value, str) or value != "":
            raise ReportUnkeyable(
                "%s does not key on %r; refusing rather than silently dropping it "
                "(dropping it would let two spellings of one report fork)"
                % (identity.action_type, name)
            )
    if "as_of" in keyed and not _is_iso_date(identity.as_of):
        raise ReportUnkeyable("as_of must be an ISO date (YYYY-MM-DD), got %r" % identity.as_of)
    return json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")


def report_key(identity: ReportIdentity, *, scope: str, run_id: str) -> str:
    return derive_key(scope, run_id, TOOL_NAME, canonical_identity(identity))


class SpecMappingError(LookupError):
    """No attested spec hash for this action type. Raised BEFORE any
    declaration: an action type the operator did not attest a spec for is
    not declared under a neighbour's spec by accident."""


@dataclass
class Submitted:
    key: str
    intent_id: str
    result: object   # whatever the caller's submit() returned (the TR's ack)


def _spec_hash_for(intent_spec_hash, action_type: str) -> str:
    if isinstance(intent_spec_hash, str):
        return intent_spec_hash
    try:
        return intent_spec_hash[action_type]
    except KeyError:
        raise SpecMappingError(
            "no attested spec hash mapped for action_type %r (mapped: %s)"
            % (action_type, sorted(intent_spec_hash))
        ) from None


def gate_submission(identity: ReportIdentity, submit: Callable[[], object], client: Client, *,
                    intent_spec_hash: str | Mapping[str, str], scope: str, run_id: str) -> Submitted:
    """Declare ONE report submission and run `submit()` exactly once, only on
    a fresh synchronous Proceed. `submit` takes no arguments on purpose: the
    caller closes over the report bytes, and this module never sees them.

    Order of refusals, all BEFORE any declaration: unkeyable identity
    (ReportUnkeyable), unmapped spec (SpecMappingError). After declaring,
    every non-Proceed outcome raises IntentRefused (gating.py) with submit
    never invoked. Each invocation declares under its OWN fresh intent: the
    episode seed is the key plus a per-invocation nonce (section 2.7).
    """
    payload = canonical_identity(identity)
    spec = _spec_hash_for(intent_spec_hash, identity.action_type)
    key = derive_key(scope, run_id, TOOL_NAME, payload)
    seed = key + "-" + uuid.uuid4().hex
    res = client.declare(Request(
        episode_seed=seed,
        idempotency_key=key,
        intent_spec_hash=spec,
        idempotency_scope=scope,
    ))
    require_fresh_proceed(res)
    return Submitted(key=key, intent_id=intent_id(seed), result=submit())


@dataclass
class BatchOutcome:
    identity: ReportIdentity
    submitted: Submitted | None
    refused: Exception | None   # ReportUnkeyable | SpecMappingError | IntentRefused

    @property
    def ok(self) -> bool:
        return self.submitted is not None


def gate_batch(items: list[tuple[ReportIdentity, Callable[[], object]]], client: Client, *,
              intent_spec_hash, scope: str, run_id: str) -> list[BatchOutcome]:
    """Gate a list of (identity, submit) pairs, each INDEPENDENTLY, in order.

    This promises NO batch atomicity: a regulatory file with per-record
    ACK/NACK is neither ACHIEVED nor REFUSED as a whole, so each record is
    its own intent with its own key, and the result is one outcome per
    record. File-level acknowledgement handling stays the caller's concern.
    Only gate outcomes (ReportUnkeyable, SpecMappingError, IntentRefused) are
    captured per record; any other exception from submit() propagates. On
    such a propagation, the outcomes of records already executed are NOT
    lost -- they are recoverable via `reconcile` against the FEED (the
    authority), never from this function's own return value.
    """
    out: list[BatchOutcome] = []
    for identity, submit in items:
        try:
            done = gate_submission(identity, submit, client, intent_spec_hash=intent_spec_hash,
                                   scope=scope, run_id=run_id)
        except (ReportUnkeyable, SpecMappingError, IntentRefused) as exc:
            out.append(BatchOutcome(identity, None, exc))
        else:
            out.append(BatchOutcome(identity, done, None))
    return out


@dataclass
class Reconciliation:
    """Keys, sorted, per class. `matched` is the bijection; everything else
    is a defect the auditor reads by class, never by parsing prose."""

    matched: list[str]
    achieved_without_submission: list[str]   # the feed says it happened; the log does not
    submissions_without_achieved: list[str]  # the log says it happened; the plane never authorized it
    duplicate_submissions: list[str]         # one key, >1 log entries: the class this adapter prevents
    duplicate_achieved: list[str]            # one key, >1 ACHIEVED: the gate's invariant broken (verifier territory)

    @property
    def ok(self) -> bool:
        return not (self.achieved_without_submission or self.submissions_without_achieved
                    or self.duplicate_submissions or self.duplicate_achieved)


def reconcile(achieved_records, submissions, *, scope: str, run_id: str) -> Reconciliation:
    """Recompute every logged submission's key from its identity and check
    the bijection against the feed's ACHIEVED records for this scope, run
    and tool. Records of other tools or runs are ignored; non-ACHIEVED
    records are ignored. A logged submission that cannot be keyed raises
    ReportUnkeyable: that is a finding about the log, not a row to skip."""
    prefix = scope + ":" + run_id + ":" + TOOL_NAME + ":"
    achieved: dict[str, int] = {}
    for rec in achieved_records:
        if rec.get("type") != "ACHIEVED":
            continue
        key = rec.get("idempotency_key", "")
        if not key.startswith(prefix):
            continue
        achieved[key] = achieved.get(key, 0) + 1
    logged: dict[str, int] = {}
    for identity in submissions:
        key = report_key(identity, scope=scope, run_id=run_id)
        logged[key] = logged.get(key, 0) + 1
    return Reconciliation(
        matched=sorted(k for k in achieved if k in logged and achieved[k] == 1 and logged[k] == 1),
        achieved_without_submission=sorted(k for k in achieved if k not in logged),
        submissions_without_achieved=sorted(k for k in logged if k not in achieved),
        duplicate_submissions=sorted(k for k, n in logged.items() if n > 1),
        duplicate_achieved=sorted(k for k, n in achieved.items() if n > 1),
    )

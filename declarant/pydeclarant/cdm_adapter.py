"""CDM bridge (CONTRACT.md section 2.7, "CDM bridge"): FINOS Common Domain
Model `WorkflowStep` <-> the reporting adapter's `ReportIdentity`, in both
directions, as PLAIN DICTS in CDM 7.1.0's JSON shape. This module imports
no CDM package: `finos-cdm` is a test-time oracle for the emitted shape
only. Stdlib-only, tree-local.

Why dict-shaped: the plane is payload-blind and the adapters are stdlib-
only by contract; a CDM desk already has the typed models and validates
what this emits with them (test_cdm_adapter.py shows how). Why a default
action mapping with an override: CDM's ActionEnum (New / Correct / Cancel)
is a MESSAGE-level action; EMIR's action types are a different vocabulary,
so the default is a convenience the caller is expected to override for
MODI / REVI / TERM / the daily types.
"""

from __future__ import annotations

from client import Client
from gating import IntentRefused
from reporting_adapter import ACTION_TYPES, ReportIdentity, ReportUnkeyable, gate_submission, report_key

CDM_VERSION = "7.1.0"  # the JSON shape this module targets; a re-map is one edit per direction

ACTION_FOR_CDM = {"New": "NEWT", "Correct": "CORR", "Cancel": "EROR"}
CDM_ACTION_FOR = {
    "NEWT": "New", "MODI": "New", "REVI": "New", "TERM": "New",
    "VALU": "New", "COLU": "New", "POSC": "New", "MARU": "New",
    "CORR": "Correct", "EROR": "Cancel",
}
_UTI_TYPE = "UniqueTransactionIdentifier"


def _first_identifier(identifiers) -> str:
    """eventIdentifier[0].assignedIdentifier[0].identifier, or ''."""
    try:
        return str(identifiers[0]["assignedIdentifier"][0]["identifier"])
    except (IndexError, KeyError, TypeError):
        return ""


def _trade_states(step: dict):
    """Every TradeState dict reachable from businessEvent.after[] or
    proposedEvent.instruction[].before."""
    out = []
    be = step.get("businessEvent") or {}
    for ts in be.get("after") or []:
        if isinstance(ts, dict):
            out.append(ts)
    pe = step.get("proposedEvent") or {}
    for instr in pe.get("instruction") or []:
        before = (instr or {}).get("before")
        if isinstance(before, dict):
            out.append(before)
    return out


def _utis(step: dict) -> set[str]:
    found = set()
    for ts in _trade_states(step):
        for tid in ((ts.get("trade") or {}).get("tradeIdentifier") or []):
            if (tid or {}).get("identifierType") == _UTI_TYPE:
                value = _first_identifier([tid])
                if value:
                    found.add(value)
    return found


def identity_from_workflow_step(step: dict, *, reporting_entity: str, rule_set: str,
                                action_type: str | None = None, on_behalf_of: str = "") -> ReportIdentity:
    """Read a CDM WorkflowStep dict into the identity the reporting adapter
    keys on. Refuses (ReportUnkeyable) a step with no UTI, two different
    UTIs, no eventIdentifier, an unknown CDM action with no explicit
    action_type override, an explicit action_type not in the closed table,
    or a step missing the discriminator its resolved action type keys on
    (no previousWorkflowStep for Correct/Cancel, no eventDate for a daily
    type) -- refused HERE, in CDM vocabulary, rather than handed onward as
    an identity the reporting adapter would only refuse later in its own
    vocabulary (CONTRACT.md section 2.7)."""
    utis = _utis(step)
    if not utis:
        raise ReportUnkeyable("CDM step carries no UTI (identifierType %r) on any trade state" % _UTI_TYPE)
    if len(utis) > 1:
        raise ReportUnkeyable("CDM step carries two different UTIs %s; one report keys one UTI" % sorted(utis))
    event_ref = _first_identifier(step.get("eventIdentifier") or [])
    if not event_ref:
        raise ReportUnkeyable("CDM step carries no eventIdentifier; the event cannot be referenced")
    if action_type is None:
        cdm_action = step.get("action")
        if cdm_action not in ACTION_FOR_CDM:
            raise ReportUnkeyable("unknown CDM action %r and no explicit action_type given" % (cdm_action,))
        action_type = ACTION_FOR_CDM[cdm_action]
    elif action_type not in ACTION_TYPES:
        raise ReportUnkeyable(
            "action_type %r is not in the closed table %s" % (action_type, sorted(ACTION_TYPES)))
    prior_ref = _first_identifier((step.get("previousWorkflowStep") or {}).get("eventIdentifier") or [])
    as_of = str(((step.get("businessEvent") or step.get("proposedEvent") or {}).get("eventDate")) or "")
    # Only the discriminators the action type keys on are populated; the
    # reporting adapter refuses any other non-empty one, so an emitted
    # identity is keyable by construction or refused loudly, never both.
    # But first: refuse here, in CDM vocabulary, if a discriminator the
    # resolved action type keys on could not be extracted from the step at
    # all -- rather than build an identity canonical_identity would only
    # refuse later in reporting-adapter vocabulary (prior_ref, as_of).
    keyed = ACTION_TYPES[action_type]
    if "prior_ref" in keyed and not prior_ref:
        raise ReportUnkeyable(
            "%s requires 'previousWorkflowStep' with an eventIdentifier; "
            "the CDM step carries none" % action_type)
    if "as_of" in keyed and not as_of:
        raise ReportUnkeyable(
            "%s requires 'eventDate' on businessEvent or proposedEvent; "
            "the CDM step carries none" % action_type)
    return ReportIdentity(
        reporting_entity=reporting_entity,
        uti=next(iter(utis)),
        action_type=action_type,
        rule_set=rule_set,
        on_behalf_of=on_behalf_of,
        as_of=as_of if "as_of" in keyed else "",
        event_ref=event_ref if "event_ref" in keyed else "",
        prior_ref=prior_ref if "prior_ref" in keyed else "",
    )


def workflow_step_for(identity: ReportIdentity, *, key: str, outcome: str, reason: str = "",
                      timestamp_utc: str, previous_event_ref: str = "") -> dict:
    """Emit the plane's outcome for one report as a CDM-shaped WorkflowStep.
    The idempotency KEY is the step's eventIdentifier so a CDM consumer can
    recompute the plane's record by it. `businessEvent` is present ONLY for
    ACHIEVED; every other outcome is `rejected: true` with the class and
    reason in messageInformation.messageId, and carries the declaration as
    `proposedEvent` (CDM: the two are mutually exclusive)."""
    try:
        cdm_action = CDM_ACTION_FOR[identity.action_type]
    except KeyError:
        raise ReportUnkeyable(
            "action_type %r has no CDM action mapping (closed table: %s)"
            % (identity.action_type, sorted(CDM_ACTION_FOR))) from None
    trade_state = {"trade": {"tradeIdentifier": [
        {"identifierType": _UTI_TYPE, "assignedIdentifier": [{"identifier": identity.uti}]}]}}
    event_date = identity.as_of or timestamp_utc[:10]
    step: dict = {
        "action": cdm_action,
        "timestamp": [{"dateTime": timestamp_utc, "qualification": "eventCreationDateTime"}],
        "eventIdentifier": [{"assignedIdentifier": [{"identifier": key}]}],
    }
    if previous_event_ref:
        step["previousWorkflowStep"] = {"eventIdentifier": [{"assignedIdentifier": [{"identifier": previous_event_ref}]}]}
    if outcome == "ACHIEVED":
        step["businessEvent"] = {"eventDate": event_date, "after": [trade_state]}
    else:
        step["rejected"] = True
        step["proposedEvent"] = {"eventDate": event_date, "instruction": [{"before": trade_state}]}
        step["messageInformation"] = {"messageId": "%s:%s" % (outcome, reason) if reason else outcome}
    return step


def gate_cdm_event(step: dict, submit, client: Client, *, reporting_entity: str, rule_set: str, intent_spec_hash,
                   scope: str, run_id: str, action_type: str | None = None, qualifier=None,
                   timestamp_utc: str) -> dict:
    """Gate ONE CDM step's submission and return the outcome as a CDM-shaped
    WorkflowStep (ACHIEVED with a businessEvent, or rejected with the class
    and reason). Gate refusals are RETURNED as steps, not raised: the step
    is the record a CDM consumer stores. Pre-declaration refusals raise
    ReportUnkeyable, including an intent/qualification mismatch: when a
    `qualifier(step) -> event name | None` is supplied (a CDM desk passes
    its Qualify_* dispatch), the step's declared `intent` must equal what
    the instruction QUALIFIES as, else the agent said one thing and proposed
    another and nothing is declared. The qualifier is caller-supplied so
    this module stays CDM-free; its result is compared as a string.
    """
    identity = identity_from_workflow_step(step, reporting_entity=reporting_entity, rule_set=rule_set,
                                           action_type=action_type)
    if qualifier is not None:
        declared = ((step.get("businessEvent") or step.get("proposedEvent") or {}).get("intent"))
        qualified = qualifier(step)
        if qualified is None:
            raise ReportUnkeyable("the CDM instruction qualifies as nothing; an unqualifiable event is not declared")
        if str(declared) != str(qualified):
            raise ReportUnkeyable("declared intent %r but the instruction qualifies as %r; nothing declared"
                                  % (declared, qualified))
    key = report_key(identity, scope=scope, run_id=run_id)
    prior = identity.prior_ref
    try:
        gate_submission(identity, submit, client, intent_spec_hash=intent_spec_hash, scope=scope, run_id=run_id)
    except IntentRefused as refused:
        return workflow_step_for(identity, key=key, outcome=refused.class_, reason=refused.reason,
                                 timestamp_utc=timestamp_utc, previous_event_ref=prior)
    return workflow_step_for(identity, key=key, outcome="ACHIEVED", timestamp_utc=timestamp_utc,
                             previous_event_ref=prior)

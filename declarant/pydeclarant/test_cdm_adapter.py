"""Tests for the CDM bridge (CONTRACT.md section 2.7, "CDM bridge"). Two
lanes: the pure lane (dict in, dict out; nothing skips) and the
CDM-VALIDATED lane, which imports finos.cdm as an ORACLE for the emitted
shape and skips VISIBLY where the package is absent or cannot construct a
WorkflowStep (the 2026-08-21 learning). A skipped oracle is never a green one.
"""

from __future__ import annotations

import pytest

from cdm_adapter import (
    ACTION_FOR_CDM,
    CDM_ACTION_FOR,
    CDM_VERSION,
    identity_from_workflow_step,
    workflow_step_for,
)
from reporting_adapter import ReportIdentity, ReportUnkeyable, report_key

LEI = "5493001KJTIIGC8Y1R12"
UTI = "1030E2F8BB0C4D6A8B9F1E2D3C4B5A69"
RULES = "ESMA-EMIR-REFIT-VR-1.4.0"
SCOPE, RUN_ID = "per-actor", "run-cdm-1"
TS = "2026-08-21T09:00:00Z"


def _step(action="New", uti=UTI, event_ref="evt-1", prior_ref="", where="businessEvent"):
    trade_state = {"trade": {"tradeIdentifier": [
        {"identifierType": "UniqueTransactionIdentifier", "assignedIdentifier": [{"identifier": uti}]}]}}
    step = {
        "action": action,
        "timestamp": [{"dateTime": TS, "qualification": "eventCreationDateTime"}],
        "eventIdentifier": [{"assignedIdentifier": [{"identifier": event_ref}]}],
    }
    if where == "businessEvent":
        step["businessEvent"] = {"intent": "CashFlow", "eventDate": "2026-08-21", "after": [trade_state]}
    else:
        step["proposedEvent"] = {"intent": "CashFlow", "eventDate": "2026-08-21",
                                 "instruction": [{"before": trade_state}]}
    if prior_ref:
        step["previousWorkflowStep"] = {"eventIdentifier": [{"assignedIdentifier": [{"identifier": prior_ref}]}]}
    return step


def test_version_and_default_mappings():
    assert CDM_VERSION == "7.1.0"
    assert ACTION_FOR_CDM == {"New": "NEWT", "Correct": "CORR", "Cancel": "EROR"}
    assert CDM_ACTION_FOR == {"NEWT": "New", "MODI": "New", "REVI": "New", "TERM": "New", "VALU": "New",
                              "COLU": "New", "POSC": "New", "MARU": "New", "CORR": "Correct", "EROR": "Cancel"}


def test_identity_from_new_step_is_a_newt_keyed_on_the_uti():
    ident = identity_from_workflow_step(_step(), reporting_entity=LEI, rule_set=RULES)
    assert ident == ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="NEWT", rule_set=RULES)


def test_identity_from_correct_step_carries_event_and_prior_refs():
    ident = identity_from_workflow_step(_step(action="Correct", prior_ref="evt-0"), reporting_entity=LEI, rule_set=RULES)
    assert ident.action_type == "CORR" and ident.event_ref == "evt-1" and ident.prior_ref == "evt-0"


def test_identity_from_cancel_step_is_an_eror_on_the_prior_ref():
    ident = identity_from_workflow_step(_step(action="Cancel", prior_ref="evt-0"), reporting_entity=LEI, rule_set=RULES)
    assert ident.action_type == "EROR" and ident.prior_ref == "evt-0" and ident.event_ref == ""


def test_explicit_action_type_overrides_the_default_mapping_and_keys_correctly():
    ident = identity_from_workflow_step(_step(action="New"), reporting_entity=LEI, rule_set=RULES, action_type="MODI")
    assert ident.action_type == "MODI" and ident.event_ref == "evt-1"
    report_key(ident, scope=SCOPE, run_id=RUN_ID)  # keyable: MODI keys on event_ref


def test_uti_is_found_on_the_proposed_event_path_too():
    ident = identity_from_workflow_step(_step(where="proposedEvent"), reporting_entity=LEI, rule_set=RULES)
    assert ident.uti == UTI


@pytest.mark.parametrize(
    "mutate,needle",
    [
        (lambda s: s["businessEvent"]["after"][0]["trade"].__setitem__("tradeIdentifier", []), "no UTI"),
        (lambda s: s["businessEvent"]["after"].append({"trade": {"tradeIdentifier": [
            {"identifierType": "UniqueTransactionIdentifier", "assignedIdentifier": [{"identifier": "OTHER"}]}]}}), "two different UTIs"),
        (lambda s: s.__setitem__("action", "Amend"), "unknown CDM action"),
        (lambda s: s.__setitem__("eventIdentifier", []), "no eventIdentifier"),
    ],
)
def test_unreadable_steps_are_unkeyable(mutate, needle):
    step = _step()
    mutate(step)
    with pytest.raises(ReportUnkeyable) as exc:
        identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES)
    assert needle in str(exc.value)


def test_cancel_with_no_previous_workflow_step_is_unkeyable_at_the_bridge():
    # action="Cancel" defaults to EROR, which keys on prior_ref; no
    # previousWorkflowStep means the bridge cannot extract prior_ref, and
    # must refuse here rather than build an identity canonical_identity
    # would only refuse later.
    step = _step(action="Cancel")  # prior_ref="" (default)
    with pytest.raises(ReportUnkeyable) as exc:
        identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES)
    assert "previousWorkflowStep" in str(exc.value)


def test_correct_with_no_previous_workflow_step_is_unkeyable_at_the_bridge():
    step = _step(action="Correct")  # prior_ref="" (default)
    with pytest.raises(ReportUnkeyable) as exc:
        identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES)
    assert "previousWorkflowStep" in str(exc.value)


def test_valu_override_with_no_event_date_is_unkeyable_at_the_bridge():
    # action_type="VALU" overridden explicitly; the step's businessEvent
    # carries no eventDate, so the bridge cannot extract as_of.
    step = _step(action="New")
    del step["businessEvent"]["eventDate"]
    with pytest.raises(ReportUnkeyable) as exc:
        identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES, action_type="VALU")
    assert "eventDate" in str(exc.value)


def test_bogus_explicit_action_type_is_unkeyable_at_the_bridge():
    step = _step(action="New")
    with pytest.raises(ReportUnkeyable) as exc:
        identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES, action_type="BOGUS")
    assert "action_type" in str(exc.value) and "BOGUS" in str(exc.value)


def test_workflow_step_for_refuses_out_of_table_action_type():
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="BOGUS", rule_set=RULES)
    with pytest.raises(ReportUnkeyable) as exc:
        workflow_step_for(ident, key="k", outcome="ACHIEVED", timestamp_utc=TS)
    assert "BOGUS" in str(exc.value)


def test_achieved_outcome_emits_business_event_with_key_as_event_identifier():
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="VALU", rule_set=RULES, as_of="2026-08-21")
    key = report_key(ident, scope=SCOPE, run_id=RUN_ID)
    step = workflow_step_for(ident, key=key, outcome="ACHIEVED", timestamp_utc=TS)
    assert step["action"] == "New"
    assert step["eventIdentifier"] == [{"assignedIdentifier": [{"identifier": key}]}]
    assert step["timestamp"] == [{"dateTime": TS, "qualification": "eventCreationDateTime"}]
    assert "rejected" not in step
    assert step["businessEvent"]["eventDate"] == "2026-08-21"
    assert step["businessEvent"]["after"][0]["trade"]["tradeIdentifier"][0]["assignedIdentifier"][0]["identifier"] == UTI
    assert "proposedEvent" not in step  # CDM: businessEvent and proposedEvent are mutually exclusive


def test_refused_outcome_emits_rejected_proposed_event_and_no_business_event():
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="EROR", rule_set=RULES, prior_ref="evt-0")
    key = report_key(ident, scope=SCOPE, run_id=RUN_ID)
    step = workflow_step_for(ident, key=key, outcome="HUMAN_JUDGMENT",
                             reason="unevaluable:human-judgment:erasure-approval", timestamp_utc=TS,
                             previous_event_ref="evt-0")
    assert step["action"] == "Cancel"
    assert step["rejected"] is True
    assert "businessEvent" not in step and "proposedEvent" in step
    assert step["previousWorkflowStep"] == {"eventIdentifier": [{"assignedIdentifier": [{"identifier": "evt-0"}]}]}
    assert step["messageInformation"]["messageId"] == "HUMAN_JUDGMENT:unevaluable:human-judgment:erasure-approval"


def test_roundtrip_identity_through_emitted_step():
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="CORR", rule_set=RULES, event_ref="evt-1", prior_ref="evt-0")
    key = report_key(ident, scope=SCOPE, run_id=RUN_ID)
    step = workflow_step_for(ident, key=key, outcome="ACHIEVED", timestamp_utc=TS, previous_event_ref="evt-0")
    back = identity_from_workflow_step(step, reporting_entity=LEI, rule_set=RULES, action_type="CORR")
    # event_ref on the emitted step IS the key (so a CDM consumer recomputes by it); everything else round-trips
    assert back.uti == UTI and back.action_type == "CORR" and back.prior_ref == "evt-0" and back.event_ref == key


def test_adapter_import_graph_is_stdlib_and_tree_local():
    import pathlib
    src = (pathlib.Path(__file__).parent / "cdm_adapter.py").read_text(encoding="utf-8")
    allowed = {"__future__", "json", "uuid", "dataclasses", "typing", "client", "declare", "gating", "reporting_adapter"}
    for line in src.splitlines():
        s = line.strip()
        if s.startswith("import ") or s.startswith("from "):
            assert s.split()[1].split(".")[0] in allowed, "unexpected import: %s" % s


# --- the CDM-validated lane: finos.cdm as an ORACLE, skipping visibly -----------------


def _cdm_workflow_step_or_skip():
    pytest.importorskip(
        "finos.cdm",
        reason=(
            "finos-cdm not installed (test-time oracle only). NOTE: installing it does NOT "
            "make this lane run -- 7.1.0 cannot construct a WorkflowStep carrying an "
            "eventIdentifier, measured on BOTH Linux (3.12.3) and Windows (3.14.2) with "
            "byte-identical errors, so the guard below skips too. See the 2026-08-21 "
            "learning in the testing monorepo."
        ),
    )
    import finos._bundle  # noqa: F401  bundle-first: direct leaf import hits a circular import
    from finos.cdm.event.workflow.WorkflowStep import WorkflowStep
    try:
        WorkflowStep.model_validate({"action": "New",
                                     "timestamp": [{"dateTime": TS, "qualification": "eventCreationDateTime"}],
                                     "eventIdentifier": [{"assignedIdentifier": [{"identifier": "probe"}]}]})
    except Exception as exc:  # noqa: BLE001
        pytest.skip("finos-cdm %s cannot construct a WorkflowStep with an eventIdentifier here: %r" % (CDM_VERSION, exc))
    return WorkflowStep


def test_emitted_achieved_step_validates_against_cdm():
    WorkflowStep = _cdm_workflow_step_or_skip()
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="VALU", rule_set=RULES, as_of="2026-08-21")
    step = workflow_step_for(ident, key=report_key(ident, scope=SCOPE, run_id=RUN_ID), outcome="ACHIEVED", timestamp_utc=TS)
    ws = WorkflowStep.model_validate(step)
    ws.validate_model()


def test_emitted_refused_step_validates_against_cdm():
    WorkflowStep = _cdm_workflow_step_or_skip()
    ident = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="EROR", rule_set=RULES, prior_ref="evt-0")
    step = workflow_step_for(ident, key=report_key(ident, scope=SCOPE, run_id=RUN_ID), outcome="HUMAN_JUDGMENT",
                             reason="unevaluable:human-judgment:erasure-approval", timestamp_utc=TS, previous_event_ref="evt-0")
    ws = WorkflowStep.model_validate(step)
    ws.validate_model()


# --- gate_cdm_event --------------------------------------------------------------------

from client import Client  # noqa: E402
from cdm_adapter import gate_cdm_event  # noqa: E402
from _gate_double import _Script, _serve  # noqa: E402

SPEC_HASH = "ab" * 32
ACHIEVED_BODY = {"terminal": "ACHIEVED", "reason": ""}


class _Counter:
    def __init__(self):
        self.n = 0

    def submit(self):
        self.n += 1
        return "ack"


def test_gate_cdm_event_submits_once_and_returns_an_achieved_step():
    c = _Counter()
    script = _Script([(200, ACHIEVED_BODY)], key_aware=True)
    server, url = _serve(script)
    try:
        out = gate_cdm_event(_step(), c.submit, Client(url), reporting_entity=LEI, rule_set=RULES,
                             intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, timestamp_utc=TS)
        again = gate_cdm_event(_step(), c.submit, Client(url), reporting_entity=LEI, rule_set=RULES,
                               intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, timestamp_utc=TS)
    finally:
        server.shutdown()
    assert c.n == 1
    assert "businessEvent" in out and "rejected" not in out
    assert again["rejected"] is True and again["messageInformation"]["messageId"].startswith("ALREADY_RESERVED:")
    assert out["eventIdentifier"] == again["eventIdentifier"]  # same key, two steps: the record of the refusal


def test_qualifier_mismatch_is_refused_before_declaring():
    c = _Counter()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    step = _step()
    step["businessEvent"]["intent"] = "Novation"
    try:
        with pytest.raises(ReportUnkeyable) as exc:
            gate_cdm_event(step, c.submit, Client(url), reporting_entity=LEI, rule_set=RULES,
                           intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, timestamp_utc=TS,
                           qualifier=lambda s: "PartialTermination")
    finally:
        server.shutdown()
    assert "declared intent 'Novation'" in str(exc.value) and "qualifies as 'PartialTermination'" in str(exc.value)
    assert script.calls == [] and c.n == 0


def test_qualifier_agreement_proceeds_and_qualifier_none_is_refused():
    c = _Counter()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    step = _step()
    step["businessEvent"]["intent"] = "CashFlow"
    try:
        out = gate_cdm_event(step, c.submit, Client(url), reporting_entity=LEI, rule_set=RULES,
                             intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, timestamp_utc=TS,
                             qualifier=lambda s: "CashFlow")
        with pytest.raises(ReportUnkeyable) as exc:
            gate_cdm_event(_step(uti="OTHER1"), c.submit, Client(url), reporting_entity=LEI, rule_set=RULES,
                           intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID, timestamp_utc=TS,
                           qualifier=lambda s: None)
    finally:
        server.shutdown()
    assert c.n == 1 and "businessEvent" in out
    assert "qualifies as nothing" in str(exc.value)

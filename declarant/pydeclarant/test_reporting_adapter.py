"""Tests for the regulatory-reporting adapter (CONTRACT.md section 2.7,
"Regulatory-reporting adapter"; claim 18 of the published SDK's section 5.4
table -- claim numbers differ between the two repos by design; section 2.7
resolves in both).

Stdlib-only lane: nothing here skips. Every declaration is asserted against
an in-process gate double that CAPTURES method, path and POST body, so
"declares nothing" is a POST count of zero, never an inference.
"""

from __future__ import annotations

import json

import pytest

from declare import derive_key
from reporting_adapter import (
    ACTION_TYPES,
    TOOL_NAME,
    ReportIdentity,
    ReportUnkeyable,
    canonical_identity,
    report_key,
)

SCOPE = "per-actor"
RUN_ID = "run-rep-1"
LEI = "5493001KJTIIGC8Y1R12"
UTI = "1030E2F8BB0C4D6A8B9F1E2D3C4B5A69"
RULES = "ESMA-EMIR-REFIT-VR-1.4.0"
_NFD_TAIL = "e" + "\u0301"  # "e" + combining acute accent (U+0301): renders
# identically to the precomposed accented e but is a different code-point
# sequence -- an NFD spelling, exactly the fork the NFC refusal must catch.
# Built from a \u escape on purpose so no editor/encoding step can silently
# re-normalize it back to NFC before the test even runs.
# spelling that renders identically to the precomposed NFC character but is
# NOT byte-equal to it -- exactly the fork the NFC refusal exists to catch.


def _valu(**over):
    base = dict(reporting_entity=LEI, uti=UTI, action_type="VALU", rule_set=RULES, as_of="2026-08-21")
    base.update(over)
    return ReportIdentity(**base)


# --- the closed table ---------------------------------------------------------


def test_action_type_table_is_the_contract_table():
    assert ACTION_TYPES == {
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


def test_tool_name_has_no_colon():
    assert TOOL_NAME == "report-submit"
    assert ":" not in TOOL_NAME


# --- canonical bytes ------------------------------------------------------------


def test_canonical_identity_is_sorted_compact_json_over_keyed_fields_only():
    got = canonical_identity(_valu())
    assert got == json.dumps(
        {
            "action_type": "VALU",
            "as_of": "2026-08-21",
            "on_behalf_of": "",
            "reporting_entity": LEI,
            "rule_set": RULES,
            "uti": UTI,
        },
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def test_report_key_is_derive_key_over_canonical_identity():
    ident = _valu()
    assert report_key(ident, scope=SCOPE, run_id=RUN_ID) == derive_key(
        SCOPE, RUN_ID, TOOL_NAME, canonical_identity(ident)
    )


def test_same_identity_built_from_dicts_in_different_orders_is_one_key():
    a = ReportIdentity(**{"reporting_entity": LEI, "uti": UTI, "action_type": "VALU", "rule_set": RULES, "as_of": "2026-08-21"})
    b = ReportIdentity(**{"as_of": "2026-08-21", "rule_set": RULES, "action_type": "VALU", "uti": UTI, "reporting_entity": LEI})
    assert report_key(a, scope=SCOPE, run_id=RUN_ID) == report_key(b, scope=SCOPE, run_id=RUN_ID)


@pytest.mark.parametrize(
    "field,other",
    [
        ("rule_set", "ESMA-EMIR-REFIT-VR-1.5.0"),  # a new rule set is a NEW action, deliberately (section 2.7)
        ("as_of", "2026-08-22"),
        ("on_behalf_of", "529900T8BM49AURSDO55"),
        ("uti", UTI[:-1] + "A"),
        ("reporting_entity", "529900T8BM49AURSDO55"),
    ],
)
def test_keyed_fields_change_the_key(field, other):
    assert report_key(_valu(), scope=SCOPE, run_id=RUN_ID) != report_key(
        _valu(**{field: other}), scope=SCOPE, run_id=RUN_ID
    )


def test_modi_keys_on_event_ref_and_two_same_day_events_differ():
    a = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="MODI", rule_set=RULES, event_ref="evt-1")
    b = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="MODI", rule_set=RULES, event_ref="evt-2")
    assert report_key(a, scope=SCOPE, run_id=RUN_ID) != report_key(b, scope=SCOPE, run_id=RUN_ID)


# --- strictness: refuse rather than drop or invent -----------------------------


@pytest.mark.parametrize(
    "identity,needle",
    [
        (_valu(action_type="XXXX"), "unknown action_type"),
        (_valu(uti=""), "requires 'uti'"),
        (_valu(reporting_entity=""), "requires 'reporting_entity'"),
        (_valu(rule_set=""), "requires 'rule_set'"),
        (_valu(as_of=""), "requires 'as_of'"),
        (_valu(as_of="21/08/2026"), "ISO date"),
        (_valu(as_of="2026-13-45"), "ISO date"),  # controller's fix: reject impossible calendar dates, not just wrong shape
        (_valu(as_of="20260801"), "ISO date"),  # ISO 8601 BASIC spelling of a real date; fromisoformat (3.11+) accepts it, which would fork the key from "2026-08-01"
        (_valu(as_of="2026-W31-6"), "ISO date"),  # ISO 8601 WEEK-DATE spelling of a real date; fromisoformat (3.11+) accepts it, which would fork the key from "2026-08-01"
        (_valu(event_ref="evt-1"), "does not key on 'event_ref'"),  # non-keyed discriminator present: refused, not dropped
        (ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="NEWT", rule_set=RULES, as_of="2026-08-21"), "does not key on 'as_of'"),
        (ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="CORR", rule_set=RULES, event_ref="evt-1"), "requires 'prior_ref'"),
        (ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="EROR", rule_set=RULES), "requires 'prior_ref'"),
        # identity fields are keyed VERBATIM (CONTRACT.md section 2.7): a fork
        # here is fail-OPEN (controller's fix, adversarial review 2026-08-21)
        (_valu(reporting_entity=LEI + " "), "whitespace"),          # trailing space on reporting_entity
        (_valu(uti=" " + UTI), "whitespace"),                       # leading space on uti
        (_valu(reporting_entity=LEI + _NFD_TAIL), "Unicode NFC"),   # NFD spelling of a field that renders identically in NFC
    ],
)
def test_unkeyable_identities_are_refused_with_a_named_reason(identity, needle):
    with pytest.raises(ReportUnkeyable) as exc:
        canonical_identity(identity)
    assert needle in str(exc.value)


def test_case_only_difference_is_a_recorded_residual_not_yet_closed():
    """PINS a known, documented residual (CONTRACT.md section 2.7,
    "RECORDED RESIDUAL"): letter case is NOT normalized by this module, so
    two spellings of the SAME identity field that differ only in case
    derive DIFFERENT keys today, and both can execute. This is not an
    endorsement of the fork -- it is a trip-wire. If someone later
    implements case-folding, this assertion flips and CONTRACT.md's
    RECORDED RESIDUAL paragraph must be updated (or removed) in the same
    change."""
    lower = _valu(reporting_entity=LEI.lower())
    upper = _valu(reporting_entity=LEI.upper())
    assert report_key(lower, scope=SCOPE, run_id=RUN_ID) != report_key(upper, scope=SCOPE, run_id=RUN_ID)


# --- gate_submission -------------------------------------------------------------

from client import Client  # noqa: E402
from declare import (  # noqa: E402
    ALREADY_RESERVED,
    INDETERMINATE,
    POLICY_DENIED,
    SHADOW_RECORDED,
    UNEVALUABLE,
    UNKNOWN,
    HUMAN_JUDGMENT,
)
from gating import IntentRefused  # noqa: E402
from reporting_adapter import SpecMappingError, Submitted, gate_submission  # noqa: E402
from _gate_double import _Script, _serve  # noqa: E402

SPEC_HASH = "ab" * 32
EROR_SPEC_HASH = "cd" * 32
ACHIEVED_BODY = {"terminal": "ACHIEVED", "reason": ""}


class _Repository:
    """Stands in for a trade repository: counts submissions and remembers the
    bytes. A counter, not a TR -- the observable is the count."""

    def __init__(self):
        self.submissions = []

    def submitter(self, payload: bytes):
        def submit():
            self.submissions.append(payload)
            return "ack-%d" % len(self.submissions)
        return submit


def _posted_bodies(script):
    return [json.loads(c[2]) for c in script.calls if c[0] == "POST"]


def test_proceed_submits_exactly_once_and_returns_the_ack():
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        out = gate_submission(_valu(), repo.submitter(b"<valu/>"), Client(url),
                              intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert isinstance(out, Submitted)
    assert out.result == "ack-1"
    assert repo.submissions == [b"<valu/>"]
    body = _posted_bodies(script)[0]
    assert body["idempotency_key"] == out.key == report_key(_valu(), scope=SCOPE, run_id=RUN_ID)
    assert body["intent_spec_hash"] == SPEC_HASH
    assert body["spec"] == {"idempotency_scope": SCOPE}
    assert body["episode_seed"].startswith(out.key + "-")  # fresh intent per invocation, never the bare key


@pytest.mark.parametrize(
    "status,body,want_class,want_retry",
    [
        (200, {"terminal": "FAILED", "reason": "balance"}, POLICY_DENIED, True),
        (200, {"terminal": "FAILED_AT_DISPATCH", "reason": "idempotency-collision"}, ALREADY_RESERVED, False),
        (200, {"terminal": "FAILED", "reason": "unevaluable:balance"}, UNEVALUABLE, True),
        (200, {"terminal": "FAILED", "reason": "unevaluable:human-judgment:erasure-approval"}, HUMAN_JUDGMENT, True),  # key not reserved; retry after a human resolves it
        (200, {"terminal": "SHADOW_RECORDED", "reason": ""}, SHADOW_RECORDED, True),
        (500, None, INDETERMINATE, False),  # empty feed for this intent -> INDETERMINATE stands
        (200, {"terminal": "SOMETHING_NEW", "reason": "x"}, UNKNOWN, False),  # out-of-vocab: the fail-closed pin
    ],
)
def test_refusals_never_submit(status, body, want_class, want_retry):
    repo = _Repository()
    script = _Script([(status, body if body is not None else {})])
    server, url = _serve(script)
    try:
        with pytest.raises(IntentRefused) as exc:
            gate_submission(_valu(), repo.submitter(b"<valu/>"), Client(url),
                            intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert repo.submissions == []  # refused means NEVER submitted
    assert exc.value.class_ == want_class
    assert exc.value.same_key_retry_safe is want_retry


def test_same_identity_different_content_is_one_key_and_submits_once():
    """The defect class this adapter exists for: a second valuation for the
    same UTI and date, with different bytes, is the SAME logical report.
    Asserted against the KEY-AWARE double so this is non-duplication, not key
    equality (the key-blind default could not see the difference)."""
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY)], key_aware=True)
    server, url = _serve(script)
    try:
        client = Client(url)
        first = gate_submission(_valu(), repo.submitter(b"<valu value=100/>"), client,
                                intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
        with pytest.raises(IntentRefused) as exc:
            gate_submission(_valu(), repo.submitter(b"<valu value=101/>"), client,
                            intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert exc.value.class_ == ALREADY_RESERVED
    assert repo.submissions == [b"<valu value=100/>"]  # executed ONCE
    assert script.reserved == [first.key]
    seeds = [b["episode_seed"] for b in _posted_bodies(script)]
    assert len(set(seeds)) == 2  # same key, two fresh episodes


@pytest.mark.parametrize("field,other", [("as_of", "2026-08-22"), ("rule_set", "ESMA-EMIR-REFIT-VR-1.5.0")])
def test_negative_control_different_identity_submits_twice(field, other):
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY), (200, ACHIEVED_BODY)], key_aware=True)
    server, url = _serve(script)
    try:
        client = Client(url)
        gate_submission(_valu(), repo.submitter(b"a"), client, intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
        gate_submission(_valu(**{field: other}), repo.submitter(b"b"), client, intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert repo.submissions == [b"a", b"b"]
    assert len(set(script.reserved)) == 2


def test_unkeyable_identity_declares_nothing():
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        with pytest.raises(ReportUnkeyable):
            gate_submission(_valu(event_ref="evt-1"), repo.submitter(b"x"), Client(url),
                            intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert script.calls == []  # zero POSTs: nothing declared
    assert repo.submissions == []


def test_spec_mapping_declares_each_action_type_under_its_own_hash():
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY), (200, {"terminal": "FAILED", "reason": "unevaluable:human-judgment:erasure-approval"})])
    server, url = _serve(script)
    mapping = {"VALU": SPEC_HASH, "EROR": EROR_SPEC_HASH}
    eror = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="EROR", rule_set=RULES, prior_ref="sub-1")
    try:
        client = Client(url)
        gate_submission(_valu(), repo.submitter(b"v"), client, intent_spec_hash=mapping, scope=SCOPE, run_id=RUN_ID)
        with pytest.raises(IntentRefused) as exc:
            gate_submission(eror, repo.submitter(b"e"), client, intent_spec_hash=mapping, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert exc.value.class_ == HUMAN_JUDGMENT
    assert repo.submissions == [b"v"]  # the erasure never ran: the GATE abstained
    hashes = [b["intent_spec_hash"] for b in _posted_bodies(script)]
    assert hashes == [SPEC_HASH, EROR_SPEC_HASH]


def test_spec_mapping_without_the_action_type_declares_nothing():
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        with pytest.raises(SpecMappingError):
            gate_submission(_valu(), repo.submitter(b"v"), Client(url),
                            intent_spec_hash={"EROR": EROR_SPEC_HASH}, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert script.calls == []
    assert repo.submissions == []


def test_submit_is_zero_arg_and_adapter_never_sees_content():
    """The adapter's signature is the claim: submit takes no arguments, so the
    report bytes never pass through this module."""
    import inspect
    sig = inspect.signature(gate_submission)
    assert list(sig.parameters) == ["identity", "submit", "client", "intent_spec_hash", "scope", "run_id"]


# --- gate_batch --------------------------------------------------------------------

from reporting_adapter import BatchOutcome, gate_batch  # noqa: E402


def test_batch_runs_records_independently_and_preserves_order():
    repo = _Repository()
    script = _Script([(200, ACHIEVED_BODY), (200, {"terminal": "FAILED", "reason": "balance"})])
    server, url = _serve(script)
    items = [
        (_valu(as_of="2026-08-19"), repo.submitter(b"d19")),
        (_valu(event_ref="evt-1"), repo.submitter(b"bad")),      # unkeyable: declares nothing
        (_valu(as_of="2026-08-20"), repo.submitter(b"d20")),     # scripted refusal
    ]
    try:
        out = gate_batch(items, Client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()
    assert [o.identity for o in out] == [i for i, _ in items]
    assert [o.ok for o in out] == [True, False, False]
    assert isinstance(out[1].refused, ReportUnkeyable)
    assert isinstance(out[2].refused, IntentRefused) and out[2].refused.class_ == POLICY_DENIED
    assert out[0].submitted.result == "ack-1"
    assert repo.submissions == [b"d19"]
    assert len(_posted_bodies(script)) == 2  # the unkeyable record never reached the wire


def test_batch_promises_no_atomicity_docstring_says_so():
    assert "NO batch atomicity" in gate_batch.__doc__


def test_batch_propagates_non_gate_exceptions():
    """A submit() that blows up is the caller's bug, not a gate outcome; it
    must not be laundered into a BatchOutcome."""
    def boom():
        raise RuntimeError("TR connection reset")
    script = _Script([(200, ACHIEVED_BODY)])
    server, url = _serve(script)
    try:
        with pytest.raises(RuntimeError):
            gate_batch([(_valu(), boom)], Client(url), intent_spec_hash=SPEC_HASH, scope=SCOPE, run_id=RUN_ID)
    finally:
        server.shutdown()


# --- reconcile ---------------------------------------------------------------------

from reporting_adapter import Reconciliation, reconcile  # noqa: E402


def _achieved(key, seq):
    return {"seq": seq, "intent_seq": 3, "intent_id": "%016x" % seq, "type": "ACHIEVED",
            "idempotency_key": key, "intent_spec_hash": SPEC_HASH, "trajectory_hash": "ff" * 32}


def test_reconcile_clean_bijection_is_ok():
    subs = [_valu(as_of="2026-08-19"), _valu(as_of="2026-08-20")]
    keys = [report_key(s, scope=SCOPE, run_id=RUN_ID) for s in subs]
    feed = [_achieved(keys[0], 1), _achieved(keys[1], 2),
            {"seq": 3, "intent_seq": 0, "intent_id": "x", "type": "DECLARED"}]  # non-ACHIEVED rows are ignored
    rec = reconcile(feed, subs, scope=SCOPE, run_id=RUN_ID)
    assert isinstance(rec, Reconciliation)
    assert rec.ok
    assert rec.matched == sorted(keys)
    assert rec.achieved_without_submission == rec.submissions_without_achieved == []
    assert rec.duplicate_submissions == rec.duplicate_achieved == []


def test_reconcile_reports_each_defect_class():
    a, b, c, d = (_valu(as_of="2026-08-1%d" % i) for i in range(1, 5))
    ka, kb, kc, kd = (report_key(x, scope=SCOPE, run_id=RUN_ID) for x in (a, b, c, d))
    feed = [_achieved(ka, 1), _achieved(kb, 2), _achieved(kb, 3), _achieved(kd, 4)]
    subs = [a, c, c]  # c logged twice: the defect this adapter prevents; d never logged; b achieved twice
    rec = reconcile(feed, subs, scope=SCOPE, run_id=RUN_ID)
    assert not rec.ok
    assert rec.matched == [ka]
    assert rec.achieved_without_submission == sorted([kb, kd])
    assert rec.submissions_without_achieved == [kc]
    assert rec.duplicate_submissions == [kc]
    assert rec.duplicate_achieved == [kb]


def test_reconcile_ignores_other_tools_and_other_runs():
    subs = [_valu()]
    k = report_key(subs[0], scope=SCOPE, run_id=RUN_ID)
    other_tool = SCOPE + ":" + RUN_ID + ":sample.transfer:" + "0" * 64
    other_run = report_key(subs[0], scope=SCOPE, run_id="run-other")
    rec = reconcile([_achieved(k, 1), _achieved(other_tool, 2), _achieved(other_run, 3)], subs, scope=SCOPE, run_id=RUN_ID)
    assert rec.ok and rec.matched == [k]


def test_reconcile_refuses_an_unkeyable_logged_submission():
    """A logged submission that cannot be keyed is a FINDING about the log,
    not a row to skip."""
    with pytest.raises(ReportUnkeyable):
        reconcile([], [_valu(event_ref="evt-1")], scope=SCOPE, run_id=RUN_ID)


def test_reconcile_achieved_twice_logged_once_is_a_gate_defect_not_a_log_defect():
    """One key ACHIEVED twice by the gate, but logged exactly once by the
    firm. The firm's own log is clean -- the defect is on the GATE's side:
    its at-most-once invariant broke, not the firm's bookkeeping. Every
    log-side bijection class must read empty; only `duplicate_achieved` may
    fire, and `ok` must be False despite the log looking clean."""
    ident = _valu(as_of="2026-08-16")
    key = report_key(ident, scope=SCOPE, run_id=RUN_ID)
    feed = [_achieved(key, 1), _achieved(key, 2)]
    rec = reconcile(feed, [ident], scope=SCOPE, run_id=RUN_ID)
    assert rec.ok is False
    assert rec.matched == []  # not a clean 1:1 -- achieved[key] == 2, not 1
    assert rec.duplicate_achieved == [key]
    assert rec.duplicate_submissions == []
    assert rec.achieved_without_submission == []
    assert rec.submissions_without_achieved == []


# --- the module's import graph is the dependency claim ---------------------------


def test_adapter_import_graph_is_stdlib_and_tree_local():
    import pathlib

    src = (pathlib.Path(__file__).parent / "reporting_adapter.py").read_text(encoding="utf-8")
    allowed = {
        "__future__", "json", "uuid", "dataclasses", "typing", "datetime",
        "unicodedata", "client", "declare", "gating",
    }
    for line in src.splitlines():
        s = line.strip()
        if s.startswith("import ") or s.startswith("from "):
            root = s.split()[1].split(".")[0]
            assert root in allowed, "unexpected import: %s" % s

"""The reporting-gate probe (CONTRACT.md 2.7, "Regulatory-reporting
adapter"): the stdlib reporting adapter against the LIVE gate. Four legs:

  1. a valuation (VALU) for one UTI and one date submits EXACTLY ONCE on a
     live synchronous Proceed -- the counter reads 1 and the ack comes back;
  2. the SAME identity with DIFFERENT report bytes is refused
     ALREADY_RESERVED with the counter still 1 -- content-blind keying
     proven live: the second valuation never reached the repository;
  3. an erasure (EROR) declared under the human-judgment spec is refused by
     the GATE (class HUMAN_JUDGMENT) with the counter at 0 -- abstention as
     the plane working, not adapter code;
  4. a NEWT carrying an as_of it does not key on is refused BEFORE any
     declaration (ReportUnkeyable) -- nothing in the feed for it; the
     recompute probe's intent count is the independent evidence.

The "repository" is a counter, not a trade repository. ASCII output only.
Usage: reporting_gate_live.py <gate_url> <valu_spec_hash> <eror_spec_hash>.
Exit 0 iff all four legs match.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "declarant" / "pydeclarant"))

from client import Client  # noqa: E402
from gating import IntentRefused  # noqa: E402
from reporting_adapter import ReportIdentity, ReportUnkeyable, gate_submission  # noqa: E402

SCOPE = "per-actor"
RUN_ID = "quickstart-reporting"
LEI = "5493001KJTIIGC8Y1R12"
UTI = "1030E2F8BB0C4D6A8B9F1E2D3C4B5A69"
RULES = "ESMA-EMIR-REFIT-VR-1.4.0"


class Repository:
    def __init__(self):
        self.n = 0

    def submitter(self, payload):
        def submit():
            self.n += 1
            return "ack-%d" % self.n
        return submit


def main(gate_url, valu_hash, eror_hash):
    client = Client(gate_url)
    specs = {"VALU": valu_hash, "EROR": eror_hash}
    repo = Repository()
    ok = True
    valu = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="VALU", rule_set=RULES, as_of="2026-08-21")

    done = gate_submission(valu, repo.submitter(b"<valu value=100/>"), client,
                           intent_spec_hash=specs, scope=SCOPE, run_id=RUN_ID)
    print("reporting call 1: submitted=%d result=%s" % (repo.n, done.result))
    ok &= repo.n == 1 and done.result == "ack-1"

    try:
        gate_submission(valu, repo.submitter(b"<valu value=101/>"), client,
                        intent_spec_hash=specs, scope=SCOPE, run_id=RUN_ID)
        print("reporting call 2: EXECUTED submitted=%d" % repo.n)
        ok = False
    except IntentRefused as exc:
        print("reporting call 2: refused class=%s submitted=%d" % (exc.class_, repo.n))
        ok &= exc.class_ == "ALREADY_RESERVED" and repo.n == 1

    eror = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="EROR", rule_set=RULES, prior_ref=done.key)
    before = repo.n
    try:
        gate_submission(eror, repo.submitter(b"<eror/>"), client,
                        intent_spec_hash=specs, scope=SCOPE, run_id=RUN_ID)
        print("reporting erasure: EXECUTED submitted=%d" % repo.n)
        ok = False
    except IntentRefused as exc:
        print("reporting erasure: refused class=%s reason=%s submitted=%d" % (exc.class_, exc.reason, repo.n - before))
        ok &= exc.class_ == "HUMAN_JUDGMENT" and repo.n == before

    newt = ReportIdentity(reporting_entity=LEI, uti=UTI, action_type="NEWT", rule_set=RULES, as_of="2026-08-21")
    try:
        gate_submission(newt, repo.submitter(b"<newt/>"), client,
                        intent_spec_hash=specs, scope=SCOPE, run_id=RUN_ID)
        print("reporting unkeyable: EXECUTED")
        ok = False
    except ReportUnkeyable:
        print("reporting unkeyable: refused before declaring submitted=%d" % (repo.n - before))
        ok &= repo.n == before
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1], sys.argv[2], sys.argv[3]))

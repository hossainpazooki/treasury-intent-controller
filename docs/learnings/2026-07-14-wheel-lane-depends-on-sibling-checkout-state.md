ts: 2026-07-14T14:49:13Z
commit: 9ac6fb8
session: 495274ae-4189-4c09-b42d-8027685f9f5b (rigor-loop-engineering)
status: verified

fact: The scorer's wheel lane is green only when the sibling `regulatory-rule-engine` checkout is on a commit that CONTAINS `fixtures/artifacts/intentspec_payment/` — and today it was not. That checkout sits on a local `main` that is BEHIND `origin/main`: the fixture exists on `origin/main` (added by `c50b5ab`, Stage A) and on `adr/0023-graph-export`, but not in the local working tree, so a branch switch silently removed it. The 5 wheel-lane tests then FAIL (not skip): the `importorskip` guard only covers an absent wheel, and `_atlas_dir()` only checks that the sibling DIRECTORY exists — neither notices that the checkout is on a commit without the fixture. A lane that is red for an environmental reason presents as a code failure. The live probe was recovered by extracting the fixture and `scripts/contract-inputs/` from `origin/main` read-only (`git archive origin/main <path> | tar -x -C <scratch>`) — never by mutating the sibling checkout, which holds the operator's own branch and untracked work.

basis: same tree, one hour apart, nothing in tic changed —
```
$ wsl … python3 -m pytest -q      # 13:5x, sibling on adr/0023-graph-export
46 passed
$ wsl … python3 -m pytest -q      # 14:2x, sibling on local main
5 failed, 41 passed
FAILED tests/test_resolver.py::test_wheel_lane_rule_environment_rejects_intentspec
FAILED tests/test_resolver.py::test_wheel_lane_intent_spec_consumer_surface   (+3 more)
$ git -C ../regulatory-rule-engine cat-file -e HEAD:fixtures/artifacts/intentspec_payment/artifact.kew
fatal: path ... does not exist in 'HEAD'
$ git -C ../regulatory-rule-engine cat-file -e origin/main:fixtures/artifacts/intentspec_payment/artifact.kew   # exit 0
```

re-verify: `git -C ../regulatory-rule-engine cat-file -e HEAD:fixtures/artifacts/intentspec_payment/artifact.kew` — if this exits non-zero, the wheel lane will fail for environmental reasons, not code. Planned fix (already listed as the CI wheel-lane slice): pin `TIC_ATLAS_DIR` to a checkout at a known-good commit, and make `_wheel_lane()` skip-with-reason rather than fail when the fixture is absent.

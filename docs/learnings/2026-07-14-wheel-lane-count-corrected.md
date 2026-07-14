ts: 2026-07-14T14:48:52Z
commit: 9ac6fb8
session: 495274ae-4189-4c09-b42d-8027685f9f5b (rigor-loop-engineering; pick-up of the 2026-07-13 brief)
status: verified
kills: 2026-07-13-wheel-lane-import-name-bug.md (its basis number only — the fact it records still stands)

fact: The WSL wheel-lane suite is **46 passed, zero skips** at `6adff98`/`9ac6fb8`, not the `39 passed` recorded on 2026-07-13. The killed entry's *fact* (the lane was skipping on the wrong module name `ke_artifact`, so it could never run) remains true and is not disturbed; only its quoted count was wrong. Root cause of the wrong number: it was captured mid-session, against a tree that did not yet contain `scorer/tests/test_main_config.py` (7 tests), and then stamped with the commit that ADDED that file. 46 - 39 = 7 exactly; Windows reconciles independently (41 passed + 5 wheel-lane skips = 46 collected). A basis captured before its anchor describes a tree the anchor does not contain — the entry looked anchored and was not.

basis: re-run at HEAD `9ac6fb8`, clean tree, `scorer/` byte-unchanged since `6adff98` (`git diff --stat 6adff98 HEAD -- scorer/` empty):
```
$ wsl -e bash -lc 'cd .../scorer && python3 -m pytest -q'
46 passed, 1 warning in 1.46s
$ wsl -e bash -lc 'cd .../scorer && python3 -m pytest -q --collect-only | tail -1'
46 tests collected in 0.52s
$ .venv/Scripts/python -m pytest -q          # Windows lane
41 passed, 5 skipped, 1 warning in 0.60s
```

re-verify: `wsl -e bash -lc 'cd /mnt/c/Users/hossa/dev/treasury-intent-controller/scorer && python3 -m pytest -q'` — expect `46 passed`, zero skips, provided the sibling ATLAS checkout carries `fixtures/artifacts/intentspec_payment/` (see [2026-07-14-wheel-lane-depends-on-sibling-checkout-state.md]).

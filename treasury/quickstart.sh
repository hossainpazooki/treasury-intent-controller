#!/bin/sh
# Intent-plane quickstart (POSIX twin of quickstart.ps1). ASCII output only.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo=$(dirname -- "$here")
scorer_port=8000; gate_port=8080
gate_url="http://127.0.0.1:$gate_port"

# Fail fast on an occupied port: health-checking into someone else's process
# would score probes against a service this script does not own.
port_in_use() { # returns 0 (true) when something already answers on $1
    rc=0
    curl -sS -m 1 -o /dev/null "http://127.0.0.1:$1/" 2>/dev/null || rc=$?
    # curl exit 7 is "failed to connect" -> nothing is listening. Any other
    # outcome means a process answered (or hung), so the port is not ours.
    [ "$rc" -ne 7 ]
}
for p in $scorer_port $gate_port; do
    if port_in_use "$p"; then
        echo "[error] port $p is already in use - refusing to boot into another process" >&2
        exit 1
    fi
done

data_dir=$(mktemp -d)
scorer_dir="$repo/core/scorer"
venv_py="$scorer_dir/.venv/bin/python"

# Installed before anything is started, and pid-guarded, so a failure in the
# venv bootstrap or the go build cannot leak an already-running scorer.
cleanup() {
    [ -n "${gate_pid:-}" ] && kill "$gate_pid" 2>/dev/null || true
    [ -n "${scorer_pid:-}" ] && kill "$scorer_pid" 2>/dev/null || true
    rm -rf "$data_dir"
}
trap cleanup EXIT

# A .venv built by the Windows twin has a Scripts/ layout, so this bin/ path is
# absent even though the venv is fine. `python3 -m venv` over that directory
# rewrites its pyvenv.cfg and breaks the Windows lane, so an existing .venv is
# never re-venved - only a missing one is created.
if [ ! -x "$venv_py" ] && [ ! -e "$scorer_dir/.venv" ]; then
    echo "[setup] creating scorer venv (one-time)"
    if python3 -m venv "$scorer_dir/.venv" 2>/dev/null; then
        "$venv_py" -m pip install -q -e "$scorer_dir[dev]"
    else
        # No ensurepip on this host: leave no half-built venv behind.
        rm -rf "$scorer_dir/.venv"
    fi
fi
if [ ! -x "$venv_py" ]; then
    if python3 -c 'import fastapi, uvicorn, pydantic' 2>/dev/null; then
        echo "[setup] no POSIX venv here - using python3 with src/ on PYTHONPATH"
        venv_py=python3
        PYTHONPATH="$scorer_dir/src${PYTHONPATH:+:$PYTHONPATH}"
        export PYTHONPATH
    else
        echo "[error] no usable python3 - need a POSIX venv, or fastapi+uvicorn+pydantic installed" >&2
        exit 1
    fi
fi

# The adapter probe (probe 8) needs langchain-core; bootstrap it one-time.
# The system-python3 fallback may be PEP 668 externally-managed -- a
# user-site install is the contained escape hatch there.
if ! "$venv_py" -c 'import langchain_core' 2>/dev/null; then
    echo "[setup] installing langchain-core for the adapter probe (one-time)"
    "$venv_py" -m pip install -q langchain-core 2>/dev/null || \
        "$venv_py" -m pip install --user --break-system-packages -q langchain-core
fi

# The MCP probes (9 and 10) need fastmcp; same guard-then-install shape, and
# the same PEP 668 escape hatch for the system-python3 fallback.
if ! "$venv_py" -c 'import fastmcp' 2>/dev/null; then
    echo "[setup] installing fastmcp for the MCP probes (one-time)"
    "$venv_py" -m pip install -q 'fastmcp>=3,<4' 2>/dev/null || \
        "$venv_py" -m pip install --user --break-system-packages -q 'fastmcp>=3,<4'
fi

echo "[boot] scorer with treasury facts (balance=250, fx_rate=1.30)"
SCORER_FACTS_JSON=$(cat "$here/facts.json") SCORER_PORT=$scorer_port "$venv_py" -m scorer &
scorer_pid=$!

echo "[boot] building the gate, the plane role CLIs, the verifier, and the declarant"
go build -o "$repo/bin/intent-gate" "$repo/core/cmd/server"
go build -o "$repo/bin/intent-control" "$repo/treasury/control"
go build -o "$repo/bin/intent-verify" "$repo/verifier/cmd/intent-verify"
go build -o "$repo/bin/intent-declare" "$repo/declarant/cmd/intent-declare"

# The plane ladder: keygen -> trust root -> attest -> publish. Nothing the
# probes declare against exists until the control role's key has signed it;
# the gate receives criteria ONLY through this store (P1: the wire has no
# criteria field at all).
echo "[plane] keygen + trust root (test key authority - ADR-0009 not landed)"
"$repo/bin/intent-control" keygen -key "$data_dir/attester.key.json"
"$repo/bin/intent-control" root -key "$data_dir/attester.key.json" -out "$data_dir/trust-root.json"

attest_publish() { # spec-file -> prints hash
    "$repo/bin/intent-control" attest -key "$data_dir/attester.key.json" \
        -draft "$here/specs/$1" -out "$data_dir/$1.env.json" >/dev/null
    "$repo/bin/intent-control" publish -root "$data_dir/trust-root.json" \
        -store "$data_dir/specs" -env "$data_dir/$1.env.json" | sed 's/^published + pinned //'
}
echo "[plane] attesting + publishing the treasury specs"
hash01=$(attest_publish 01-within-limits.spec.json)
hash02=$(attest_publish 02-near-duplicate.spec.json)
hash03=$(attest_publish 03-over-threshold.spec.json)
hash05=$(attest_publish 05-thin.spec.json)
hash08=$(attest_publish 08-erasure-human-judgment.spec.json)

echo "[boot] starting the gate (trust root + spec store wired)"
INTENT_SCORER_URL="http://127.0.0.1:$scorer_port/ml/evaluate" INTENT_DATA_DIR="$data_dir" \
    INTENT_TRUST_ROOT="$data_dir/trust-root.json" INTENT_SPEC_DIR="$data_dir/specs" \
    INTENT_ADDR=":$gate_port" "$repo/bin/intent-gate" &
gate_pid=$!

wait_healthy() {
    i=0
    while [ $i -lt 50 ]; do
        if curl -fsS -m 1 "$1/healthz" >/dev/null 2>&1; then return 0; fi
        i=$((i + 1)); sleep 0.2
    done
    echo "service at $1 never became healthy" >&2; exit 1
}
wait_healthy "http://127.0.0.1:$scorer_port"
wait_healthy "$gate_url"

pass=0; fail=0
# $6 is the discriminating guard: a reason may be REQUIRED to contain one
# substring and REFUSED for containing another, so that a fail-closed
# "unevaluable:<name>" can never satisfy a "criterion bound" probe.
probe() { # name file spec_hash want_terminal want_reason_part why [reject_reason_part]
    reject=${7:-}
    body=$(sed "s/@SPEC_HASH@/$3/" "$here/probes/$2")
    resp=$(printf '%s' "$body" | curl -fsS -X POST -H "Content-Type: application/json" --data-binary @- "$gate_url/v2/intents")
    ok=$(printf '%s' "$resp" | "$venv_py" -c "import json,sys; r=json.load(sys.stdin); print('1' if r['terminal']=='$4' and ('$5'=='' or '$5' in r['reason']) and ('$reject'=='' or '$reject' not in r['reason']) else '0')")
    terminal=$(printf '%s' "$resp" | "$venv_py" -c "import json,sys; r=json.load(sys.stdin); print(r['terminal'], r['reason'])")
    if [ "$ok" = "1" ]; then pass=$((pass + 1)); tag=PASS; else fail=$((fail + 1)); tag=FAIL; fi
    echo "[$tag] $1: $terminal"
    echo "       why it matters: $6"
}

probe "declare within limits" 01-declare-pass.json "$hash01" ACHIEVED "" "the full lifecycle runs against a SIGNED spec and real scored facts; exactly one durable ACHIEVED record exists"
probe "near-duplicate, same key" 02-near-duplicate.json "$hash02" FAILED_AT_DISPATCH idempotency-collision "at-most-once holds by construction: the declared key collides, value cannot move twice"
probe "over threshold" 03-over-threshold.json "$hash03" FAILED balance "criteria actually bind - the refusal names the failing criterion, and NOT because it was unevaluable" unevaluable
probe "unattested spec hash" 06-unattested.json "1111111111111111111111111111111111111111111111111111111111111111" FAILED unevaluable:unattested-spec "no signature, no scoring: a hash nobody attested never reaches the scorer (P1)"

echo "[plane] revoking the within-limits spec (signed tombstone)"
"$repo/bin/intent-control" revoke -key "$data_dir/attester.key.json" -root "$data_dir/trust-root.json" \
    -store "$data_dir/specs" -hash "$hash01" -ref quickstart-pull
probe "declare against revoked spec" 07-revoked.json "$hash01" FAILED revoked:quickstart-pull "authority is revocable: the tombstone's ref is witnessed in the refusal"

# Probe 6 (declarant consumption, CONTRACT.md 2.7): declare through the
# published SDK - deterministic derived key, exact wire marshal, classified
# terminal. The first declare authorizes; a second declare with the SAME
# derived key (fresh episode) collides and classifies ALREADY_RESERVED with
# same_key_retry_safe=false. A refusing CLI exits nonzero by design - not a
# script abort.
d1_rc=0; d1=$("$repo/bin/intent-declare" -gate "$gate_url" -seed quickstart-declarant \
    -spec-hash "$hash02" -scope per-actor -run quickstart -tool sample.transfer \
    -args '{"amount":"25","unit":"alpha"}') || d1_rc=$?
d2_rc=0; d2=$("$repo/bin/intent-declare" -gate "$gate_url" -seed quickstart-declarant-2 \
    -spec-hash "$hash02" -scope per-actor -run quickstart -tool sample.transfer \
    -args '{"amount":"25","unit":"alpha"}') || d2_rc=$?
case "$d1" in *"class=PROCEED terminal=ACHIEVED"*) d1_ok=1;; *) d1_ok=0;; esac
case "$d2" in *"class=ALREADY_RESERVED"*"same_key_retry_safe=false"*) d2_ok=1;; *) d2_ok=0;; esac
if [ "$d1_rc" -eq 0 ] && [ "$d1_ok" = 1 ] && [ "$d2_rc" -eq 1 ] && [ "$d2_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] declarant SDK: $d1 -> then $d2"
else
    fail=$((fail + 1)); echo "[FAIL] declarant SDK: rc=$d1_rc/$d2_rc out1=$d1 out2=$d2"
fi
echo "       why it matters: the published SDK is the embedding half of the sale - derived keys make dedup real, and the collision is classified, not mysterious"

# Probe 7 (Python declarant twin LIVE, CONTRACT.md 2.7): the same two-step
# through pydeclarant's stdlib Client under its OWN derived key - the twin's
# first live leg. A refusing step exits nonzero by design - a probe
# assertion, not a script abort.
p7_rc=0; p7=$("$venv_py" "$here/probes/pydeclarant_live.py" "$gate_url" "$hash02") || p7_rc=$?
case "$p7" in
    *"class=PROCEED terminal=ACHIEVED"*"class=ALREADY_RESERVED same_key_retry_safe=false"*) p7_ok=1;;
    *) p7_ok=0;;
esac
if [ "$p7_rc" -eq 0 ] && [ "$p7_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] Python declarant twin (live): $(printf '%s' "$p7" | tr '\n' ';')"
else
    fail=$((fail + 1)); echo "[FAIL] Python declarant twin (live): rc=$p7_rc out=$(printf '%s' "$p7" | tr '\n' ';')"
fi
echo "       why it matters: the twin speaks the same wire against the live gate - the shared golden bytes are not a lab-only claim"

# Probe 8 (LangChain adapter LIVE, CONTRACT.md 2.7 framework adapter): a
# gated tool executes once on a live Proceed; the same-args call refuses
# ALREADY_RESERVED with the tool body not re-fired.
p8_rc=0; p8=$("$venv_py" "$here/probes/adapter_live.py" "$gate_url" "$hash02") || p8_rc=$?
case "$p8" in
    *"executed=1 result=moved 25 alpha"*"refused class=ALREADY_RESERVED same_key_retry_safe=false executed=1"*) p8_ok=1;;
    *) p8_ok=0;;
esac
if [ "$p8_rc" -eq 0 ] && [ "$p8_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] LangChain adapter (live): $(printf '%s' "$p8" | tr '\n' ';')"
else
    fail=$((fail + 1)); echo "[FAIL] LangChain adapter (live): rc=$p8_rc out=$(printf '%s' "$p8" | tr '\n' ';')"
fi
echo "       why it matters: a wrapped agent tool fires its consequence exactly once, and a refusal is a classified outcome - not an exception to debug"

# Probe 9 (MCP gate middleware LIVE, CONTRACT.md 2.7 "MCP gate"): a gated
# FastMCP tool executes once on a live Proceed; the same-args call refuses
# ALREADY_RESERVED with the body not re-fired; and a SECOND middleware
# instance sharing no state with the first is refused too, with its own
# counter at 0. A refusing probe exits nonzero by design - a probe
# assertion, not a script abort.
p9_rc=0; p9=$("$venv_py" "$here/probes/mcp_gate_live.py" "$gate_url" "$hash02") || p9_rc=$?
case "$p9" in
    *"mcp call 1: executed=1 result=moved 25 alpha"*"mcp call 2: refused class=ALREADY_RESERVED executed=1"*"mcp replica: refused class=ALREADY_RESERVED replica_executed=0"*) p9_ok=1;;
    *) p9_ok=0;;
esac
if [ "$p9_rc" -eq 0 ] && [ "$p9_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] MCP gate middleware (live): $(printf '%s' "$p9" | tr '\n' ';')"
else
    fail=$((fail + 1)); echo "[FAIL] MCP gate middleware (live): rc=$p9_rc out=$(printf '%s' "$p9" | tr '\n' ';')"
fi
echo "       why it matters: the key is DERIVED, not remembered - two replicas sharing no state refuse the same retry, so the gate survives horizontal scaling"

# Probe 10 (gated MCP proxy LIVE, CONTRACT.md 2.7 gated_proxy): an UNGATED
# backend standing in for a server the operator does not own is fronted by
# the gate under its OWN run id. The INNER counter is the observable: the
# refused call never reaches the backend at all.
p10_rc=0; p10=$("$venv_py" "$here/probes/mcp_proxy_live.py" "$gate_url" "$hash02") || p10_rc=$?
case "$p10" in
    *"mcp proxy call 1: inner_executed=1 result=moved 25 alpha"*"mcp proxy call 2: refused class=ALREADY_RESERVED inner_executed=1"*"mcp proxy strict: refused schema declares no default inner_tagged=0"*) p10_ok=1;;
    *) p10_ok=0;;
esac
if [ "$p10_rc" -eq 0 ] && [ "$p10_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] gated MCP proxy (live): $(printf '%s' "$p10" | tr '\n' ';')"
else
    fail=$((fail + 1)); echo "[FAIL] gated MCP proxy (live): rc=$p10_rc out=$(printf '%s' "$p10" | tr '\n' ';')"
fi
echo "       why it matters: a server you do not own is gated without changing it - the backend never sees a call the gate refused, and an unkeyable call is refused BEFORE anything is declared"

# Probe 11 (the reporting-gate probe, CONTRACT.md 2.7 "Regulatory-reporting
# adapter"): a valuation submits ONCE on a live Proceed; the same identity
# with different bytes is refused ALREADY_RESERVED (content-blind keying);
# an erasure under the human-judgment spec is refused by the GATE; an
# unkeyable NEWT declares nothing. The counter is the observable. Runs
# BEFORE the scorer is killed below: its spec's "balance" criterion is
# scored at declaration, so this leg needs a live scorer to reach Proceed.
p11_rc=0; p11=$("$venv_py" "$here/probes/reporting_gate_live.py" "$gate_url" "$hash02" "$hash08") || p11_rc=$?
case "$p11" in
    *"reporting call 1: submitted=1 result=ack-1"*"reporting call 2: refused class=ALREADY_RESERVED submitted=1"*"reporting erasure: refused class=HUMAN_JUDGMENT reason=unevaluable:human-judgment:erasure-approval submitted=0"*"reporting unkeyable: refused before declaring submitted=0"*) p11_ok=1;;
    *) p11_ok=0;;
esac
if [ "$p11_rc" -eq 0 ] && [ "$p11_ok" = 1 ]; then
    pass=$((pass + 1)); echo "[PASS] reporting gate (live): $(printf '%s' "$p11" | tr '\n' ';')"
else
    fail=$((fail + 1)); echo "[FAIL] reporting gate (live): rc=$p11_rc out=$(printf '%s' "$p11" | tr '\n' ';')"
fi
echo "       why it matters: the second valuation for one UTI and date is the duplicate a trade repository will NOT reject - refused here by identity, not content; the erasure is refused by the gate's own abstention, not by adapter code"

echo "[chaos] killing the scorer to prove fail-closed on outage"
kill "$scorer_pid"; sleep 1
# Probe 12
probe "declare during scorer outage" 04-outage.json "$hash02" FAILED unevaluable "an unreachable scorer denies - unevaluable NEVER collapses into a pass"
# Probe 13
probe "attested-but-thin spec" 05-empty-criteria.json "$hash05" FAILED unevaluable:empty-criteria "thin-spec defense - attestation does not launder vacuity; zero criteria still refuse"

# Probe 14: the feed-count probe.
achieved=$(curl -fsS "$gate_url/v2/events?since=0" | "$venv_py" -c "import json,sys; e=json.load(sys.stdin)['events']; print(sum(1 for r in e if r['type']=='ACHIEVED'))")
if [ "$achieved" = "7" ]; then pass=$((pass + 1)); echo "[PASS] durable feed: exactly 7 ACHIEVED records - one per authorized key, never a duplicate"; else fail=$((fail + 1)); echo "[FAIL] durable feed: expected exactly 7 ACHIEVED, got $achieved"; fi
echo "       why it matters: consumers settle only from this observable feed - emit-and-observe"

# Probe 15: the recompute probe (CONTRACT.md 9.1). Both verifier twins re-derive
# every commitment - trajectory hashes on grants AND refusals, sequence
# contiguity, exactly-one-ACHIEVED - from the record bytes alone, and their
# reports must be byte-identical. `set -e` is suspended around the calls: a
# refuting verifier exits nonzero, and that is a probe FAIL, not a script abort.
go_rc=0; go_report=$("$repo/bin/intent-verify" "$data_dir/events.jsonl") || go_rc=$?
py_rc=0; py_report=$("$venv_py" "$repo/verifier/pyverifier/verify.py" "$data_dir/events.jsonl") || py_rc=$?
verdict=$(printf '%s\n' "$go_report" | tail -1)
# The intent COUNT is load-bearing, not decoration: it lands on 21 rather than
# 22 precisely because the reporting probe's unkeyable NEWT is refused BEFORE
# it declares. Asserting it here makes that mechanical, instead of a fact
# somebody once read in a transcript. A new declaring probe must bump it.
if [ "$go_rc" -eq 0 ] && [ "$py_rc" -eq 0 ] && [ "$go_report" = "$py_report" ] &&    [ "${verdict#*intents=21 }" != "$verdict" ]; then
    pass=$((pass + 1)); echo "[PASS] verifier recompute: $verdict (Go and Python reports identical; intents=21 - the unkeyable report declared nothing)"
else
    fail=$((fail + 1)); echo "[FAIL] verifier recompute: goExit=$go_rc pyExit=$py_rc verdict=$verdict (expected intents=21)"
    printf '%s\n' "$go_report" | tail -3
fi
echo "       why it matters: an examiner re-derives every commitment from the record bytes alone - no trust in the gate"

echo "RESULT: $pass/15 probes passed"
[ "$fail" -eq 0 ]

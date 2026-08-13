#!/usr/bin/env python3
"""Randomized end-to-end driver for the treasury payment loop.

The quickstart ladder is DETERMINISTIC: ten hand-authored probes, each with a
hand-written expectation. It proves the paths it names and nothing else. This
driver introduces randomness at every input the plane actually reads --
scored facts, attested spec content, posture, criterion names/thresholds/
volatility, idempotency keys (with deliberate reuse), which specs get revoked
and when, and injected scorer faults -- and then holds the live system to an
INDEPENDENT ORACLE that re-derives the expected terminal from CONTRACT.md
4.2's refusal order, not from the gate's code.

Two processes stand between the driver and the gate:

  scorer (real, random facts)  <--  fault proxy  <--  gate  <--  this driver

The proxy is the randomness the real scorer cannot express: per-intent
service faults (503 -> the client maps non-2xx to Unevaluable) and per-intent
fact DRIFT (PASS at declaration, FAIL at the dispatch-edge recheck), which is
the only way to exercise `volatile-recheck` without a mutable fact source.

Non-vacuity: the run FAILS if any of the thirteen terminal possibilities in
CLASSES never occurs, so a harness that quietly stopped exercising the loop
cannot report success. The random plan is resampled (same seed stream) until
the oracle predicts full coverage; the attempts used are reported.

Everything is seeded and the seed is printed: a mismatch is reproducible with
--seed. Output is ASCII (the Windows console is cp1252).

    python treasury/randomized_loop.py [--seed N] [--intents N] [--out FILE]

Exit 0 only when every intent matched the oracle, every class occurred, the
feed invariants held, and both verifier twins re-derived the record.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import random
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
EXE = ".exe" if os.name == "nt" else ""

# The thirteen terminal possibilities the loop can produce (CONTRACT.md 3.2/3.3).
# Every one must occur in a run, or the run fails: coverage is enforced, not hoped.
CLASSES = (
    "ACHIEVED",
    "SHADOW_RECORDED",
    "idempotency-collision",
    "policy-denied",
    "unevaluable:unattested-spec",
    "revoked",
    "unevaluable:invalid-posture",
    "unevaluable:empty-criteria",
    "unevaluable:invalid-volatility",
    "unevaluable:human-judgment",
    "unevaluable:criterion",
    "volatile-recheck",
    "unevaluable:absent-key",
)


def classify(terminal: str, reason: str) -> str:
    """Bucket a (terminal, reason) pair into one of CLASSES.

    Prefix order matters exactly as it does in the declarant's own total
    classification (CONTRACT.md 2.7): specific prefixes before generic, so a
    fail-closed `unevaluable:` never lands in the policy-denied bucket.
    """
    if terminal == "ACHIEVED":
        return "ACHIEVED"
    if terminal == "SHADOW_RECORDED":
        return "SHADOW_RECORDED"
    if reason == "idempotency-collision":
        return "idempotency-collision"
    if reason.startswith("volatile-recheck:"):
        return "volatile-recheck"
    if reason.startswith("revoked:"):
        return "revoked"
    for tag in (
        "unevaluable:absent-key",
        "unevaluable:unattested-spec",
        "unevaluable:invalid-posture",
        "unevaluable:empty-criteria",
        "unevaluable:invalid-volatility",
        "unevaluable:human-judgment",
    ):
        if reason.startswith(tag):
            return tag
    if reason.startswith("unevaluable:"):
        return "unevaluable:criterion"
    if reason:
        return "policy-denied"
    return "UNKNOWN:" + terminal


def intent_id(seed: str) -> str:
    return hashlib.sha256(seed.encode()).hexdigest()[:16]


def free_port() -> int:
    s = socket.socket()
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
    s.close()
    return port


def wait_healthy(url: str, tries: int = 60) -> None:
    for _ in range(tries):
        try:
            with urllib.request.urlopen(url + "/healthz", timeout=1) as r:
                if r.status == 200:
                    return
        except Exception:
            time.sleep(0.2)
    raise SystemExit("service at %s never became healthy" % url)


def post_json(url: str, payload: dict) -> tuple[int, dict]:
    body = json.dumps(payload).encode()
    req = urllib.request.Request(
        url, data=body, headers={"Content-Type": "application/json"}, method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as r:
            return r.status, json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, {"terminal": "HTTP_%d" % e.code, "reason": e.read().decode()[:200]}


def get_json(url: str) -> dict:
    with urllib.request.urlopen(url, timeout=20) as r:
        return json.loads(r.read().decode())


# ---------------------------------------------------------------- fault proxy

class FaultProxy:
    """Sits between the gate and the scorer and injects the two faults the
    static scorer cannot: an outage (503) and fact drift at the dispatch edge.

    Both are keyed by intent_id, which the gate puts on the wire (CONTRACT.md
    2.4), so injection is per-intent and reproducible from the seed.
    """

    def __init__(self, upstream: str, outage: set[str], drift: set[str]):
        self.upstream = upstream
        self.outage = outage
        self.drift = drift
        self.calls: list[tuple[str, str, str]] = []  # (intent_id, criterion, phase)
        self.lock = threading.Lock()
        proxy = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *a):  # silence
                pass

            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"ok")

            def do_POST(self):
                raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
                try:
                    req = json.loads(raw.decode())
                except Exception:
                    req = {}
                iid = req.get("intent_id", "")
                phase = req.get("phase", "")
                with proxy.lock:
                    proxy.calls.append((iid, req.get("criterion", ""), phase))
                if iid in proxy.outage:
                    self.send_response(503)
                    self.end_headers()
                    self.wfile.write(b"injected outage")
                    return
                if iid in proxy.drift and phase == "dispatch":
                    out = json.dumps({"result": "FAIL", "basis": "injected drift"}).encode()
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(out)))
                    self.end_headers()
                    self.wfile.write(out)
                    return
                fwd = urllib.request.Request(
                    proxy.upstream, data=raw,
                    headers={"Content-Type": "application/json"}, method="POST",
                )
                try:
                    with urllib.request.urlopen(fwd, timeout=10) as r:
                        out, status = r.read(), r.status
                except Exception:
                    out, status = b'{"result":"UNEVALUABLE"}', 200
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(out)))
                self.end_headers()
                self.wfile.write(out)

        self.port = free_port()
        self.server = ThreadingHTTPServer(("127.0.0.1", self.port), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def start(self):
        self.thread.start()

    def stop(self):
        self.server.shutdown()


# ------------------------------------------------------------------ the plan

def sample_plan(rng: random.Random, n_intents: int) -> dict:
    """One random world: facts, specs, revocations, and the intent stream."""
    known = ["balance", "fx_rate", "exposure", "liquidity"]
    rng.shuffle(known)
    facts = {name: round(rng.uniform(10, 500), 2) for name in known[: rng.randint(2, 4)]}

    specs = []
    for k in range(rng.randint(6, 10)):
        roll = rng.random()
        posture = "enforce" if roll < 0.6 else ("shadow" if roll < 0.85 else
                                                rng.choice(["audit", "", "ENFORCE"]))
        criteria = []
        if rng.random() > 0.12:  # 12% of specs are thin (zero criteria)
            for _ in range(rng.randint(1, 3)):
                if rng.random() < 0.75:
                    name = rng.choice(list(facts))
                    fact = facts[name]
                    # Straddle the fact so PASS and FAIL are both reachable.
                    threshold = round(fact * rng.uniform(0.4, 1.6), 2)
                else:
                    name = rng.choice(["unknown_metric", "coverage_ratio", "vega"])
                    threshold = round(rng.uniform(1, 100), 2)
                vol = rng.choice(["stable", "volatile"])
                if rng.random() < 0.08:
                    vol = rng.choice(["Volatile", "sometimes", ""])
                criteria.append({"name": name, "threshold": threshold, "volatility": vol})
        spec = {
            "spec_version": 1,
            "action_class": rng.choice(["payment", "transfer", "settlement"]),
            "enforcement_posture": posture,
            "criteria": criteria,
        }
        if rng.random() < 0.12:
            spec["human_judgment"] = [{
                "name": rng.choice(["officer_signoff", "sanctions_review"]),
                "passage_sha256": hashlib.sha256(b"passage-%d" % k).hexdigest(),
            }]
        specs.append(spec)

    # Two VIABLE specs are planted (content still random: names drawn from the
    # random fact set, thresholds random but strictly under their fact, valid
    # volatility). Without a spec that can pass every criterion, ACHIEVED and
    # everything downstream of it -- reservation, collision, shadow-recording --
    # are unreachable BY CONSTRUCTION, and the coverage floor could never be
    # met however long the sampler ran. The randomness is in the world; the
    # plant only guarantees the world is not degenerate.
    viable = {}
    for posture in ("enforce", "shadow"):
        crit = []
        for name in rng.sample(list(facts), rng.randint(1, min(2, len(facts)))):
            crit.append({"name": name,
                         "threshold": round(facts[name] * rng.uniform(0.2, 0.9), 2),
                         "volatility": rng.choice(["stable", "volatile"])})
        specs.append({"spec_version": 1, "action_class": "payment",
                      "enforcement_posture": posture, "criteria": crit})
        viable[posture] = len(specs) - 1

    # Revocations: some before the stream starts, some part-way through it.
    revocations = []
    for idx in range(len(specs)):
        if idx in viable.values():
            continue  # revoking the viable specs would re-degenerate the world
        if rng.random() < 0.2:
            revocations.append({
                "spec": idx,
                "at": 0 if rng.random() < 0.5 else rng.randint(1, max(1, n_intents - 1)),
                "ref": rng.choice(["policy-pull", "counsel-hold", "rotation"]) + "-%d" % idx,
            })

    key_pool = ["pay-%03d" % i for i in range(max(3, n_intents // 3))]
    intents = []
    for i in range(n_intents):
        roll = rng.random()
        if roll < 0.10:
            spec_ref = -1  # a hash nobody attested
        else:
            spec_ref = rng.randrange(len(specs))
        key = "" if rng.random() < 0.06 else rng.choice(key_pool)
        intents.append({
            "i": i,
            "seed": "rnd-%d-%s" % (i, rng.getrandbits(32)),
            "spec": spec_ref,
            "key": key,
            "outage": rng.random() < 0.10,
            "drift": rng.random() < 0.15,
        })

    # Route a few intents at the viable specs with faults off, two of them
    # sharing a key so the second meets a key the first reserved. Which slots
    # get this treatment is random; what happens in them is still decided by
    # the oracle and the live gate, not asserted here.
    slots = rng.sample(range(len(intents)), min(6, len(intents)))
    shared = "pay-shared-%d" % rng.getrandbits(16)
    for n, slot in enumerate(slots):
        it = intents[slot]
        it["outage"] = it["drift"] = False
        it["spec"] = viable["shadow"] if n >= 4 else viable["enforce"]
        it["key"] = shared if n < 2 else rng.choice(key_pool)
    return {"facts": facts, "specs": specs, "revocations": revocations, "intents": intents}


def oracle(plan: dict) -> list[dict]:
    """Predict every terminal INDEPENDENTLY of the gate, from CONTRACT.md 4.2's
    refusal order. This is a second implementation of the decision procedure --
    if it agreed with the gate by construction it would prove nothing."""
    facts = plan["facts"]
    revoked_at: dict[int, list] = {}
    for r in plan["revocations"]:
        revoked_at.setdefault(r["spec"], []).append(r)
    reserved: set[str] = set()
    out = []

    for it in plan["intents"]:
        idx, key = it["i"], it["key"]
        spec_ref = it["spec"]
        rev = None
        for r in revoked_at.get(spec_ref, []):
            if r["at"] <= idx:
                rev = r
                break

        def score(c, phase):
            if it["outage"]:
                return "UNEVALUABLE"
            if it["drift"] and phase == "dispatch" and c["volatility"] == "volatile":
                return "FAIL"
            fact = facts.get(c["name"])
            if fact is None:
                return "UNEVALUABLE"
            return "PASS" if fact >= c["threshold"] else "FAIL"

        if key == "":
            term, reason = "FAILED", "unevaluable:absent-key"
        elif rev is not None:
            term, reason = "FAILED", "revoked:" + rev["ref"]
        elif spec_ref < 0:
            term, reason = "FAILED", "unevaluable:unattested-spec"
        else:
            spec = plan["specs"][spec_ref]
            posture = spec["enforcement_posture"]
            hj = spec.get("human_judgment") or []
            bad_vol = next((c for c in spec["criteria"]
                            if c["volatility"] not in ("stable", "volatile")), None)
            if posture not in ("enforce", "shadow"):
                term, reason = "FAILED", "unevaluable:invalid-posture"
            elif hj:
                term, reason = "FAILED", "unevaluable:human-judgment:" + hj[0]["name"]
            elif not spec["criteria"]:
                term, reason = "FAILED", "unevaluable:empty-criteria"
            elif bad_vol is not None:
                term, reason = "FAILED", "unevaluable:invalid-volatility:" + bad_vol["name"]
            else:
                term = reason = None
                failed = []
                for c in spec["criteria"]:
                    s = score(c, "declaration")
                    if s == "UNEVALUABLE":
                        term, reason = "FAILED", "unevaluable:" + c["name"]
                        break
                    if s == "FAIL":
                        failed.append(c["name"])
                if term is None:
                    if failed:
                        term, reason = "FAILED", ",".join(failed)
                    else:
                        for c in spec["criteria"]:
                            if c["volatility"] != "volatile":
                                continue
                            if score(c, "dispatch") != "PASS":
                                term = "FAILED_AT_DISPATCH"
                                reason = "volatile-recheck:" + c["name"]
                                break
                if term is None:
                    if posture == "shadow":
                        term, reason = "SHADOW_RECORDED", ""
                    elif key in reserved:
                        term, reason = "FAILED_AT_DISPATCH", "idempotency-collision"
                    else:
                        reserved.add(key)
                        term, reason = "ACHIEVED", ""
        out.append({"i": idx, "terminal": term, "reason": reason,
                    "class": classify(term, reason)})
    return out


# ----------------------------------------------------------------- execution

def build(repo: Path) -> dict:
    bins = {
        "gate": ("core/cmd/server", "intent-gate"),
        "control": ("treasury/control", "intent-control"),
        "verify": ("verifier/cmd/intent-verify", "intent-verify"),
    }
    out = {}
    for name, (pkg, exe) in bins.items():
        target = repo / "bin" / (exe + EXE)
        subprocess.run(["go", "build", "-o", str(target), str(repo / pkg)],
                       cwd=repo, check=True)
        out[name] = str(target)
    return out


def scorer_python(repo: Path) -> tuple[str, dict]:
    env = dict(os.environ)
    win = repo / "core/scorer/.venv/Scripts/python.exe"
    posix = repo / "core/scorer/.venv/bin/python"
    if win.exists():
        return str(win), env
    if posix.exists():
        return str(posix), env
    py = shutil.which("python3") or sys.executable
    env["PYTHONPATH"] = str(repo / "core/scorer/src") + os.pathsep + env.get("PYTHONPATH", "")
    return py, env


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--seed", type=int, default=None)
    ap.add_argument("--intents", type=int, default=60)
    ap.add_argument("--out", default=None)
    ap.add_argument("--max-resamples", type=int, default=400)
    args = ap.parse_args()

    seed = args.seed if args.seed is not None else int.from_bytes(os.urandom(4), "big")
    rng = random.Random(seed)
    print("[seed] %d  (reproduce with --seed %d)" % (seed, seed))

    # Resample until the oracle predicts every class: randomness with a floor,
    # so "main possibilities shown" is enforced rather than left to luck.
    attempts = 0
    while True:
        attempts += 1
        plan = sample_plan(rng, args.intents)
        expected = oracle(plan)
        covered = {e["class"] for e in expected}
        if set(CLASSES) <= covered:
            break
        if attempts >= args.max_resamples:
            print("[error] no plan covering all classes in %d resamples" % attempts)
            print("        missing: %s" % ", ".join(sorted(set(CLASSES) - covered)))
            return 2
    print("[plan] %d intents, %d specs, %d revocations (plan %d of %d sampled)"
          % (len(plan["intents"]), len(plan["specs"]), len(plan["revocations"]),
             attempts, attempts))
    print("[facts] " + json.dumps(plan["facts"], sort_keys=True))

    # The data dir is keyed by seed AND by which tree is being driven. Keyed by
    # seed alone, a run against a mutated COPY of the module (the plant-red
    # discipline: inject a defect, watch this driver go red) silently overwrites
    # the clean run's artifact at the same path -- which happened, and was caught
    # only by re-reading the artifacts instead of trusting the terminal output.
    tree = hashlib.sha256(str(REPO).encode()).hexdigest()[:8]
    data_dir = Path(os.environ.get("TEMP", "/tmp")) / ("intent-rnd-%d-%s" % (seed, tree))
    if data_dir.exists():
        shutil.rmtree(data_dir, ignore_errors=True)
    (data_dir / "specs").mkdir(parents=True)

    print("[build] gate, control seat, verifier")
    bins = build(REPO)
    py, env = scorer_python(REPO)

    scorer_port = free_port()
    scorer_env = dict(env)
    scorer_env["SCORER_FACTS_JSON"] = json.dumps(plan["facts"])
    scorer_env["SCORER_PORT"] = str(scorer_port)
    scorer = subprocess.Popen([py, "-m", "scorer"], env=scorer_env, cwd=str(REPO / "core/scorer"))
    proxy = None
    gate = None
    try:
        wait_healthy("http://127.0.0.1:%d" % scorer_port)

        outage = {intent_id(it["seed"]) for it in plan["intents"] if it["outage"]}
        drift = {intent_id(it["seed"]) for it in plan["intents"] if it["drift"]}
        proxy = FaultProxy("http://127.0.0.1:%d/ml/evaluate" % scorer_port, outage, drift)
        proxy.start()
        print("[proxy] fault injection live: %d outage intents, %d drift intents"
              % (len(outage), len(drift)))

        key_path = data_dir / "attester.key.json"
        root_path = data_dir / "trust-root.json"
        subprocess.run([bins["control"], "keygen", "-key", str(key_path)],
                       check=True, stdout=subprocess.DEVNULL)
        subprocess.run([bins["control"], "root", "-key", str(key_path), "-out", str(root_path)],
                       check=True, stdout=subprocess.DEVNULL)

        hashes = []
        for k, spec in enumerate(plan["specs"]):
            draft = data_dir / ("spec-%02d.json" % k)
            draft.write_text(json.dumps(spec, indent=2), encoding="utf-8")
            envf = data_dir / ("spec-%02d.env.json" % k)
            subprocess.run([bins["control"], "attest", "-key", str(key_path),
                            "-draft", str(draft), "-out", str(envf)],
                           check=True, stdout=subprocess.DEVNULL)
            pub = subprocess.run([bins["control"], "publish", "-root", str(root_path),
                                  "-store", str(data_dir / "specs"), "-env", str(envf)],
                                 check=True, capture_output=True, text=True)
            hashes.append(pub.stdout.strip().replace("published + pinned ", ""))
        print("[plane] attested + published %d random specs" % len(hashes))

        gate_port = free_port()
        gate_env = dict(os.environ)
        gate_env.update({
            "INTENT_SCORER_URL": "http://127.0.0.1:%d/ml/evaluate" % proxy.port,
            "INTENT_DATA_DIR": str(data_dir),
            "INTENT_TRUST_ROOT": str(root_path),
            "INTENT_SPEC_DIR": str(data_dir / "specs"),
            "INTENT_ADDR": ":%d" % gate_port,
        })
        gate = subprocess.Popen([bins["gate"]], env=gate_env)
        gate_url = "http://127.0.0.1:%d" % gate_port
        wait_healthy(gate_url)

        pending = {}
        for r in plan["revocations"]:
            pending.setdefault(r["at"], []).append(r)
        for r in pending.get(0, []):
            subprocess.run([bins["control"], "revoke", "-key", str(key_path),
                            "-root", str(root_path), "-store", str(data_dir / "specs"),
                            "-hash", hashes[r["spec"]], "-ref", r["ref"]],
                           check=True, stdout=subprocess.DEVNULL)

        unattested = hashlib.sha256(b"never-attested-%d" % seed).hexdigest()
        actual = []
        for it in plan["intents"]:
            for r in pending.get(it["i"], []):
                if it["i"] != 0:
                    subprocess.run([bins["control"], "revoke", "-key", str(key_path),
                                    "-root", str(root_path), "-store", str(data_dir / "specs"),
                                    "-hash", hashes[r["spec"]], "-ref", r["ref"]],
                                   check=True, stdout=subprocess.DEVNULL)
            spec_hash = unattested if it["spec"] < 0 else hashes[it["spec"]]
            status, resp = post_json(gate_url + "/v2/intents", {
                "episode_seed": it["seed"],
                "idempotency_key": it["key"],
                "rule_artifact_hash": "",
                "intent_spec_hash": spec_hash,
                "spec": {"idempotency_scope": "payer"},
            })
            term = resp.get("terminal", "HTTP_%d" % status)
            reason = resp.get("reason", "")
            actual.append({"i": it["i"], "terminal": term, "reason": reason,
                           "class": classify(term, reason), "http": status})

        # ---- compare against the oracle -------------------------------------
        mismatches = []
        for exp, act in zip(expected, actual):
            if exp["terminal"] != act["terminal"] or exp["reason"] != act["reason"]:
                mismatches.append({"i": exp["i"],
                                   "expected": [exp["terminal"], exp["reason"]],
                                   "actual": [act["terminal"], act["reason"]]})

        counts = {}
        injected = {}  # class -> how many of its occurrences rode an injected fault
        faulty = {it["i"]: (it["outage"] or it["drift"]) for it in plan["intents"]}
        for a in actual:
            counts[a["class"]] = counts.get(a["class"], 0) + 1
            if faulty.get(a["i"]):
                injected[a["class"]] = injected.get(a["class"], 0) + 1
        missing = [c for c in CLASSES if c not in counts]
        # A class every one of whose occurrences rode an injected fault was NOT
        # produced by the real scorer in this run. Saying so here is the fix for
        # run 1's overclaim ("the real Python scorer in every seed"), which a
        # refuter falsified for volatile-recheck (proxy-forged in every seed,
        # mechanically necessary while facts are static) and for
        # unevaluable:criterion in some seeds.
        forged = [c for c in CLASSES if counts.get(c, 0) > 0 and injected.get(c, 0) == counts[c]]

        # ---- feed invariants ------------------------------------------------
        events = get_json(gate_url + "/v2/events?since=0")["events"]
        achieved = [e for e in events if e["type"] == "ACHIEVED"]
        keys = [e.get("idempotency_key") for e in achieved]
        exp_achieved = sum(1 for e in expected if e["class"] == "ACHIEVED")
        feed_ok = (len(achieved) == exp_achieved and len(set(keys)) == len(keys))

        # ---- independent recompute (both twins) ------------------------------
        feed_path = data_dir / "events.jsonl"
        go_v = subprocess.run([bins["verify"], str(feed_path)], capture_output=True, text=True)
        py_v = subprocess.run([py, str(REPO / "verifier/pyverifier/verify.py"), str(feed_path)],
                              capture_output=True, text=True, env=env)
        twins_ok = (go_v.returncode == 0 and py_v.returncode == 0
                    and go_v.stdout == py_v.stdout)

        print("")
        print("[classes] every terminal possibility, as OBSERVED live")
        print("          (injected = an injected fault was in play for that intent;")
        print("           ALL-INJECTED = every occurrence had one, so for a class")
        print("           decided BY SCORING the real scorer never produced it.")
        print("           Classes refused before scoring are unaffected either way.)")
        for c in CLASSES:
            n, inj = counts.get(c, 0), injected.get(c, 0)
            tag = "  <- ALL-INJECTED" if c in forged else ""
            print("   %-32s %-3s injected=%s%s" % (c, n, inj, tag))
        print("")
        print("[oracle]   %d/%d intents matched the independent prediction"
              % (len(actual) - len(mismatches), len(actual)))
        print("[coverage] %d/%d classes occurred" % (len(CLASSES) - len(missing), len(CLASSES)))
        print("[feed]     %d ACHIEVED records, %d distinct keys (expected %d)"
              % (len(achieved), len(set(keys)), exp_achieved))
        print("[verify]   %s" % (go_v.stdout.strip().splitlines() or ["<no output>"])[-1])
        for m in mismatches[:10]:
            print("   MISMATCH intent %d: expected %s / got %s"
                  % (m["i"], m["expected"], m["actual"]))
        if missing:
            print("   MISSING CLASSES: %s" % ", ".join(missing))
        if not twins_ok:
            print("   TWIN DISAGREEMENT: goExit=%d pyExit=%d identical=%s"
                  % (go_v.returncode, py_v.returncode, go_v.stdout == py_v.stdout))

        ok = not mismatches and not missing and feed_ok and twins_ok
        record = {
            "seed": seed, "intents": len(actual), "plan_attempts": attempts,
            "facts": plan["facts"], "class_counts": counts, "missing_classes": missing,
            "injected_counts": injected, "classes_only_via_injection": forged,
            "mismatches": mismatches, "feed_ok": feed_ok,
            "achieved_records": len(achieved), "expected_achieved": exp_achieved,
            "twins_identical": twins_ok, "verifier_verdict": go_v.stdout.strip().splitlines()[-1:],
            "scorer_calls": len(proxy.calls), "ok": ok,
        }
        out_path = Path(args.out) if args.out else (data_dir / "run.json")
        out_path.write_text(json.dumps(record, indent=2, sort_keys=True), encoding="utf-8")
        print("")
        print("RESULT: %s (seed %d) - artifact %s"
              % ("PASS" if ok else "FAIL", seed, out_path))
        return 0 if ok else 1
    finally:
        for p in (gate, scorer):
            if p is not None:
                p.terminate()
        if proxy is not None:
            proxy.stop()


if __name__ == "__main__":
    sys.exit(main())

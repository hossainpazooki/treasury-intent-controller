# The plane as a separating surface — presence by contract, not containment

*Positioning note for the article ("Signed, Sealed, Deliverable"), 2026-08-12.
This is argument material, not contract language: "hyperplane," "crossing,"
and "separating surface" are article vocabulary. Normative terms remain
`CONTRACT.md` §1: declarant · author · attester · gate · verifier.*

---

## 1 · The regression, diagnosed

The original pitch was easy to communicate: the intent plane sat between
governance, control, and the data layer at every stage — a hyperplane
through the whole agent-deployment lifecycle. Because *intent* is the one
object every stage of an agentic system already handles, the framing was
both conceptually sound and practically appealing.

Then the framing got harder to state, and the tempting diagnosis is that the
architecture rulings (minimal core, roles in the application, ADR-0008)
shrank the concept. That diagnosis is wrong. What made the old framing
appealing was never that authoring and control lived *inside* the plane —
it was that the plane was *present at every crossing*. That property
survived the rulings fully intact. What regressed is only the statement of
it: the repo now leads with what the core does **not** contain (no seats,
no keys, no domain nouns), which is an architecture answer offered where a
concept answer is needed.

## 2 · Two ways to be everywhere — one failed twice

"Present at every stage" can be built two ways:

```
  PRESENCE BY CONTAINMENT                  PRESENCE BY CONTRACT
  (the pitch; tried twice, failed twice)   (what is actually built)

  +---------------------------+            governance  control  runtime  audit
  |         the plane         |               stage      stage    stage   stage
  |   +-----------+           |                 |          |        |       |
  |   | authoring |           |                 v          v        v       v
  |   +-----------+           |            =====================================
  |   | control   |           |                the intent plane — the surface
  |   +-----------+           |               every arrow pierces it as a
  |   | gate      |           |                     SIGNED ARTIFACT
  |   +-----------+           |            =====================================
  |   | feed      |           |
  +---------------------------+            the plane owns the crossings,
                                           never the stages
  "we ship a component
   at every stage"
```

Presence by containment failed in both of its attempts:

1. **As fiction.** The concept brief drew authoring and control as plane
   components while the plane half was 0% code — refuted as a
   correct-shaped lie (2026-08-04).
2. **As a layering violation.** When a session then actually built the role
   trees into the core, the operator reversed it: roles are
   application-layer; the core owns the authorization primitive and
   verification only (ADR-0008, Accepted 2026-08-05).

Presence by contract is the formulation that is both true and buildable:
the plane touches every stage **as the artifact each stage must produce or
consume**, not as a runtime component occupying that stage.

## 3 · The geometry, taken seriously

A hyperplane is not a volume. It does not occupy the regions it separates —
it is the surface every trajectory between them must cross. Taken
seriously, that is the *stronger* claim:

> The intent plane does not run your governance, your control system, or
> your runtime. It is the surface between them — and nothing crosses it
> unsigned.

"Thin by design" then stops reading as a retreat and becomes the point.
It also answers the integration-story challenge ("middleware or backbone?")
more crisply than either word:

```
  middleware   sits IN the traffic path        (the mesh: routing, scrubbing,
                                                guardrails, observability)
  backbone     you rebuild ON it               (a platform to adopt)
  surface      everything must CROSS it        (composes with both; owns
                                                the crossings, nothing else)
```

The mesh and the plane are adjacent and composable, not competing: traffic
control decides *where a request goes*; the plane decides *whether an
irreversible action is authorized, and what record that leaves*.

## 4 · One action, four crossings

Follow a single agent action from policy source to audit. It pierces the
surface four times, and each crossing is (or is specified to be) a signed
artifact:

```
  policy source
       |
       |  author drafts: values pinned to passages, unknowns named,
       |  judgment routed to human-judgment entries   [crossing 1: authoring]
  =====|=====================================================================
       v
  attested spec — ed25519 envelope, content-addressed,
  sealed, revocable (tombstones; revoked wins)        [crossing 2: control]
  =====|=====================================================================
       v
  declared intent (hash + idempotency key + scope; criteria
  CANNOT ride the wire) --> tri-state verdict,
  fail-closed twice, dispatch-edge re-check           [crossing 3: decision]
  =====|=====================================================================
       v
  terminal record — trajectory hash on EVERY completed
  authorization, grant or refusal --> append-only feed,
  polled by cursor --> verifier recompute,
  no trust in the gate                                [crossing 4: record]
       v
  audit / settlement / counterparty
```

### The crossings have unequal standing — say so

| crossing | artifact | standing (2026-08-12) |
|---|---|---|
| 1 · authoring | draft → attested payload | **unsigned today** — the draft crossing has no provenance chain; authoring integrity-key class is ADR-0009 scope (open ROADMAP row) |
| 2 · control | signed envelope · tombstone | **built, test-grade keys** — DSSE-shaped, PAE v1; `key_authority: "test"` until ADR-0009 |
| 3 · decision | declaration → verdict | **built & pinned** — thin wire, §2.6 resolution, unevaluable never passes, dispatch-edge re-check |
| 4 · record | terminal record → recompute | **built & shipped** — refusal-hash commitment (§2.3); verifier twins in the SDK (ADR-0011), tampered fixture must refute |

The hyperplane is real at three of four crossings. Naming the fourth as
roadmap costs nothing with this audience and is what keeps the framing from
re-refuting itself the way the pitch did.

## 5 · Two zoom levels, one picture

The hyperplane framing and the two-sided sale are not competing positions —
they are the same picture at different zoom:

```
  ZOOM OUT — the concept (why intent is the right cut)

      every stage of agent deployment crosses one signed surface;
      intent is the object every stage already handles

  ZOOM MID — the sale (who buys, who installs)

      demand side ------ requires examinable records ------> supply side
      accountability function                                platform team
      runs the verifier against                              embeds the
      the records (crossing 4)                               declarant (crossing 3)

  ZOOM IN — the code (what ships where)

      intent-plane (SDK):  core/ · plane/ · verifier/ — the surface
                           and the consumer packages (ADR-0011)
      application repos:   seats (authority/control/authoring) + demo —
                           the stages, composed on top
```

The concept layer answers "is this sound?"; the sale layer answers "is this
useful?"; the code layer answers "is this real?" The article needs all
three, in that order.

## 6 · Where this lands in the article

- **Problem section**: draw intent as the surface every arrow must pierce
  (fig: one action, four crossings — simplified). This is where the
  hyperplane earns its slot, replacing the question mark in
  "agent → intent → execution."
- **Core/domain-split section**: "thin by design — the plane owns the
  crossings, not the stages" explains the minimal core as a virtue, and the
  treasury layer as a worked example of a *stage* composed on top.
- **Differentiation section**: middleware / backbone / surface, then the
  mesh adjacency in one sentence each.
- **Honesty ballast**: the crossing-standing table above, compressed to one
  sentence per crossing. Crossing 1's "unsigned today" is the single most
  credibility-buying admission available.

Two discipline notes carried from the drift review: the article must not
resurrect "Intent Interface" (retired proper noun, test-banned) or
"COMPLETED" (banned; the public term is `ACHIEVED`), and "signed" must be
attributed to the *authority* (the spec the attester signs), never to the
intent on the wire — declarant signing is exactly what does not exist yet.

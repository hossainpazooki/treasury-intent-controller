package verifier

// §9.1 conformance: this lane is held to the frozen feed fixtures and their
// expected reports byte-for-byte. If the fixture directory is absent these
// tests SKIP VISIBLY — a skipped fixture test is never a green one (the hard
// witness against fixture deletion is core/internal/gate's generator test,
// which FAILS on absence). The tampered fixture is the standing mutant: one
// flipped detail byte must refute, forever, in both languages.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "core", "contract", "feed", name)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("feed fixture %s absent (%v) - a skipped fixture test is never a green one", name, err)
	}
	return p
}

func runFixture(t *testing.T, events, report string, want Verdict) {
	t.Helper()
	raw, err := os.ReadFile(fixturePath(t, events))
	if err != nil {
		t.Fatalf("reading %s: %v", events, err)
	}
	expected, err := os.ReadFile(fixturePath(t, report))
	if err != nil {
		t.Fatalf("reading %s: %v", report, err)
	}
	rep := Verify(raw)
	if rep.Overall != want {
		t.Fatalf("%s: overall = %s, want %s", events, rep.Overall, want)
	}
	if got := Render(rep); got != string(expected) {
		t.Fatalf("%s: report does not match frozen %s\ngot:\n%s\nwant:\n%s", events, report, got, expected)
	}
}

func TestFixtureGoodVerifies(t *testing.T) {
	runFixture(t, "events-good.jsonl", "report-good.txt", Verified)
}

func TestFixtureTamperedRefutes(t *testing.T) {
	runFixture(t, "events-tampered.jsonl", "report-tampered.txt", Refuted)
}

// --- unit checks over synthetic feeds ---------------------------------------

func TestEmptyFeedUnverifiable(t *testing.T) {
	rep := Verify(nil)
	if rep.Overall != Unverifiable {
		t.Fatalf("empty feed: overall = %s, want UNVERIFIABLE (verify([]) is never a pass)", rep.Overall)
	}
	if len(rep.Feed) != 1 || rep.Feed[0].Reason != "empty" {
		t.Fatalf("empty feed findings = %+v, want the single 'empty' finding", rep.Feed)
	}
}

func TestTornTrailingLineTolerated(t *testing.T) {
	raw, err := os.ReadFile(fixturePath(t, "events-good.jsonl"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	// Append a torn write: a partial record with NO trailing newline. The
	// store tolerates it on recovery (§2.3); the verifier must reproduce
	// exactly that tolerance.
	torn := append(append([]byte{}, raw...), []byte(`{"seq":99,"intent_`)...)
	rep := Verify(torn)
	if rep.Overall != Verified {
		t.Fatalf("torn trailing line: overall = %s, want VERIFIED\n%s", rep.Overall, Render(rep))
	}
}

func TestUnparseableMidFileUnverifiable(t *testing.T) {
	raw, err := os.ReadFile(fixturePath(t, "events-good.jsonl"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	// The same partial record TERMINATED by a newline is no longer a torn
	// tail: bytes exist that cannot be re-derived, so the feed abstains.
	mid := append(append([]byte{}, raw...), []byte("{\"seq\":99,\"intent_\n")...)
	rep := Verify(mid)
	if rep.Overall != Unverifiable {
		t.Fatalf("unparseable mid-file line: overall = %s, want UNVERIFIABLE", rep.Overall)
	}
	found := false
	for _, f := range rep.Feed {
		if strings.HasPrefix(f.Reason, "unparseable-line:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no unparseable-line finding: %+v", rep.Feed)
	}
}

func TestSeqGapRefutes(t *testing.T) {
	raw := []byte(`{"seq":1,"intent_seq":0,"intent_id":"aa","type":"DECLARED","detail":"aa"}
{"seq":3,"intent_seq":1,"intent_id":"aa","type":"UNEVALUABLE","detail":"absent-key","trajectory_hash":"x"}
`)
	rep := Verify(raw)
	if rep.Overall != Refuted {
		t.Fatalf("seq gap: overall = %s, want REFUTED", rep.Overall)
	}
	if len(rep.Feed) == 0 || rep.Feed[0].Reason != "seq-gap:3:2" {
		t.Fatalf("feed findings = %+v, want seq-gap:3:2 first", rep.Feed)
	}
}

func TestHashOnNonTerminalRefutes(t *testing.T) {
	raw := []byte(`{"seq":1,"intent_seq":0,"intent_id":"aa","type":"DECLARED","detail":"aa","trajectory_hash":"deadbeef"}
{"seq":2,"intent_seq":1,"intent_id":"aa","type":"UNEVALUABLE","detail":"absent-key","trajectory_hash":"deadbeef"}
`)
	rep := Verify(raw)
	if len(rep.Intents) != 1 || rep.Intents[0].Reason != "hash-on-nonterminal:0" {
		t.Fatalf("intents = %+v, want hash-on-nonterminal:0", rep.Intents)
	}
}

func TestNoTerminalHashAbstains(t *testing.T) {
	// A pre-amendment (or mid-flight) intent: no committed hash anywhere.
	// UNVERIFIABLE, never VERIFIED — abstention is the verifier's twin of
	// invariant 2's non-vacuity.
	raw := []byte(`{"seq":1,"intent_seq":0,"intent_id":"aa","type":"DECLARED","detail":"aa"}
{"seq":2,"intent_seq":1,"intent_id":"aa","type":"UNEVALUABLE","detail":"absent-key"}
`)
	rep := Verify(raw)
	if len(rep.Intents) != 1 || rep.Intents[0].Verdict != Unverifiable || rep.Intents[0].Reason != "no-terminal-hash" {
		t.Fatalf("intents = %+v, want UNVERIFIABLE no-terminal-hash", rep.Intents)
	}
}

func TestDuplicateAchievedKeyRefutes(t *testing.T) {
	raw, err := os.ReadFile(fixturePath(t, "events-good.jsonl"))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	// Replay the whole good fixture twice with the second copy's seqs
	// continuing the first — every intent's records duplicate (intent-seq
	// dups) and the ACHIEVED key repeats feed-wide.
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	var doubled []string
	doubled = append(doubled, lines...)
	for i, l := range lines {
		doubled = append(doubled, strings.Replace(l,
			`{"seq":`+strconv.Itoa(i+1)+`,`,
			`{"seq":`+strconv.Itoa(len(lines)+i+1)+`,`, 1))
	}
	rep := Verify([]byte(strings.Join(doubled, "\n") + "\n"))
	if rep.Overall != Refuted {
		t.Fatalf("doubled feed: overall = %s, want REFUTED", rep.Overall)
	}
	foundKey := false
	for _, f := range rep.Feed {
		if strings.HasPrefix(f.Reason, "duplicate-achieved-key:") {
			foundKey = true
		}
	}
	if !foundKey {
		t.Fatalf("no duplicate-achieved-key finding in %+v", rep.Feed)
	}
}

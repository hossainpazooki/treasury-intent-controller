// Package audit holds the per-intent append-only event log.
//
// The log is a deterministic, append-only sequence of events with a logical
// clock (Seq = 0,1,2,…; never wallclock). TrajectoryHash provides a stable,
// order-sensitive fingerprint of the whole log so that identical event
// sequences hash byte-for-byte identically across runs and processes.
package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// Event is one entry in a per-intent append-only log. Seq is a logical clock
// (0,1,2,…), NEVER wallclock.
type Event struct {
	Seq    int
	Type   string
	Detail string
}

// EventLog is a per-intent append-only event log.
type EventLog struct {
	events []Event
}

// NewEventLog returns a fresh, empty event log.
func NewEventLog() *EventLog {
	return &EventLog{}
}

// Append adds an event with the next sequence number and returns it. The first
// appended event has Seq 0, the next 1, and so on (monotonic, gap-free).
func (l *EventLog) Append(typ, detail string) Event {
	e := Event{
		Seq:    len(l.events),
		Type:   typ,
		Detail: detail,
	}
	l.events = append(l.events, e)
	return e
}

// Events returns the events in order. The returned slice is a copy: mutating it
// (or its elements) does not affect the log's internal state.
func (l *EventLog) Events() []Event {
	out := make([]Event, len(l.events))
	copy(out, l.events)
	return out
}

// TrajectoryHash returns a deterministic, order-sensitive SHA-256 hash over the
// canonical serialization of the events, as lowercase hex.
//
// Canonical encoding (FIXED — do not change without rehashing all golden
// values): the events are encoded in order. For each event, in field order
// Seq, Type, Detail, we write the field's byte length in decimal, a ':'
// separator, then the field's raw bytes, then a '\n' terminator. Seq is first
// rendered to its decimal-string form, then length-prefixed like the others.
//
//	<len(seqStr)>:<seqStr>\n<len(Type)>:<Type>\n<len(Detail)>:<Detail>\n
//
// Length-prefixing every variable-width field makes the encoding injection-safe:
// no choice of Type/Detail contents (including ':' or '\n') can forge an event
// boundary, so distinct event sequences always produce distinct byte streams.
// The hash is therefore stable (same events ⟹ same hash) and order-sensitive
// (swapping two events changes both their Seq fields and their positions).
func (l *EventLog) TrajectoryHash() string {
	h := sha256.New()
	writeField := func(s string) {
		h.Write([]byte(strconv.Itoa(len(s))))
		h.Write([]byte{':'})
		h.Write([]byte(s))
		h.Write([]byte{'\n'})
	}
	for _, e := range l.events {
		writeField(strconv.Itoa(e.Seq))
		writeField(e.Type)
		writeField(e.Detail)
	}
	return hex.EncodeToString(h.Sum(nil))
}

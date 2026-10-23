package events_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"sim.dev/engine/events"
)

var at = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func TestMarshalPutsTheTypeFirst(t *testing.T) {
	line, err := events.Marshal(events.TaskStarted{Time: at, Environment: "e", Task: "greet", Machine: "m", Index: 1, Total: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(line), `{"type":"task.started",`) {
		t.Errorf("type is not the first field: %s", line)
	}

	var decoded map[string]any
	if err := json.Unmarshal(line, &decoded); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, line)
	}
	if decoded["task"] != "greet" || decoded["index"] != float64(1) {
		t.Errorf("fields lost in rendering: %s", line)
	}
}

func TestEveryEventHasADistinctType(t *testing.T) {
	all := []events.Event{
		events.ApplyStarted{}, events.NetworkCreated{}, events.MachineCreated{}, events.MachineReady{},
		events.TaskStarted{}, events.TaskOutput{}, events.TaskFinished{}, events.ApplyFinished{},
		events.DestroyStarted{}, events.ResourceRemoved{}, events.DestroyFinished{},
		events.Warning{}, events.Error{},
	}
	seen := map[string]bool{}
	for _, e := range all {
		if seen[e.Type()] {
			t.Errorf("type %q is used twice", e.Type())
		}
		seen[e.Type()] = true

		if _, err := events.Marshal(e); err != nil {
			t.Errorf("%s does not render: %v", e.Type(), err)
		}
	}
}

func TestJSONLinesWritesOneObjectPerLine(t *testing.T) {
	var buf bytes.Buffer
	sink := events.JSONLines(&buf, nil)

	sink(events.Warning{Time: at, Environment: "e", Message: "first"})
	sink(events.Warning{Time: at, Environment: "e", Message: "second\nwith a newline"})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2 — an embedded newline must stay escaped:\n%s", len(lines), buf.String())
	}
	for _, l := range lines {
		if !json.Valid([]byte(l)) {
			t.Errorf("line is not JSON: %s", l)
		}
	}
}

func TestJSONLinesReportsAWriteFailureOnceAndKeepsGoing(t *testing.T) {
	var reports int
	sink := events.JSONLines(failingWriter{}, func(error) { reports++ })

	for i := 0; i < 3; i++ {
		sink(events.Warning{Time: at, Message: "x"})
	}
	if reports != 1 {
		t.Errorf("reported %d times, want once", reports)
	}
}

func TestTeeDeliversToEverySinkInOrder(t *testing.T) {
	var order []string
	sink := events.Tee(
		func(events.Event) { order = append(order, "a") },
		nil,
		func(events.Event) { order = append(order, "b") },
	)
	sink(events.Warning{})

	if got := strings.Join(order, ","); got != "a,b" {
		t.Errorf("delivery order = %s, want a,b", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

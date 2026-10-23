// Package events is the engine's typed progress stream.
//
// Library callers receive these values as they happen; simctl renders the very
// same values as JSON lines, and the run log stored with each Environment is
// that same JSON-lines stream. There is deliberately one event path, so that
// what a caller sees, what the terminal shows and what the log records cannot
// drift apart (FR-30).
package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"
)

// Event is one thing that happened. The set of events is closed: only this
// package defines them.
type Event interface {
	// Type is the event's name in the JSON-lines stream, such as
	// "task.started".
	Type() string
	isEvent()
}

// Sink receives events. A Sink must not block for long: events are emitted
// on the path of the work they describe.
type Sink func(Event)

// Discard is a Sink that drops every event.
func Discard(Event) {}

// Tee returns a Sink that delivers each event to every non-nil sink, in order.
func Tee(sinks ...Sink) Sink {
	return func(e Event) {
		for _, s := range sinks {
			if s != nil {
				s(e)
			}
		}
	}
}

// Statuses carried by finished events.
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusError     = "error"
)

// Streams carried by TaskOutput.
const (
	StreamStdout = "stdout"
	StreamStderr = "stderr"
)

// ApplyStarted opens an apply.
type ApplyStarted struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	PlanHash    string    `json:"planHash"`
	Machines    int       `json:"machines"`
	Tasks       int       `json:"tasks"`
}

// NetworkCreated reports a network the apply created.
type NetworkCreated struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Network     string    `json:"network"`
	Subnet      string    `json:"subnet"`
}

// MachineCreated reports a Machine created, with its files staged, but not yet
// started.
type MachineCreated struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Machine     string    `json:"machine"`
	Image       string    `json:"image"`
}

// MachineReady reports a Machine started and reachable.
type MachineReady struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Machine     string    `json:"machine"`
	IP          string    `json:"ip"`
}

// TaskStarted reports that a Task began. Index counts from 1.
type TaskStarted struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Task        string    `json:"task"`
	Machine     string    `json:"machine"`
	Index       int       `json:"index"`
	Total       int       `json:"total"`
}

// TaskOutput carries output from a running Task's command as it arrives.
// Chunk boundaries are wherever the backend delivered them.
type TaskOutput struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Task        string    `json:"task"`
	Machine     string    `json:"machine"`
	Stream      string    `json:"stream"`
	Data        string    `json:"data"`
}

// TaskFinished reports a Task's verdict.
type TaskFinished struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Task        string    `json:"task"`
	Machine     string    `json:"machine"`
	Status      string    `json:"status"`
	ExitCode    int       `json:"exitCode"`
	Duration    string    `json:"duration"`
	Reason      string    `json:"reason,omitempty"`
}

// ApplyFinished closes an apply.
type ApplyFinished struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	Duration    string    `json:"duration"`
}

// DestroyStarted opens a destroy.
type DestroyStarted struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
}

// ResourceRemoved reports one resource a destroy removed.
type ResourceRemoved struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Kind        string    `json:"kind"`
	Name        string    `json:"name"`
}

// DestroyFinished closes a destroy.
type DestroyFinished struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	Removed     int       `json:"removed"`
}

// Warning is something worth knowing that did not stop the work.
type Warning struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Message     string    `json:"message"`
}

// Error is something that stopped the work.
type Error struct {
	Time        time.Time `json:"time"`
	Environment string    `json:"environment"`
	Message     string    `json:"message"`
}

func (ApplyStarted) Type() string    { return "apply.started" }
func (NetworkCreated) Type() string  { return "network.created" }
func (MachineCreated) Type() string  { return "machine.created" }
func (MachineReady) Type() string    { return "machine.ready" }
func (TaskStarted) Type() string     { return "task.started" }
func (TaskOutput) Type() string      { return "task.output" }
func (TaskFinished) Type() string    { return "task.finished" }
func (ApplyFinished) Type() string   { return "apply.finished" }
func (DestroyStarted) Type() string  { return "destroy.started" }
func (ResourceRemoved) Type() string { return "resource.removed" }
func (DestroyFinished) Type() string { return "destroy.finished" }
func (Warning) Type() string         { return "warning" }
func (Error) Type() string           { return "error" }

func (ApplyStarted) isEvent()    {}
func (NetworkCreated) isEvent()  {}
func (MachineCreated) isEvent()  {}
func (MachineReady) isEvent()    {}
func (TaskStarted) isEvent()     {}
func (TaskOutput) isEvent()      {}
func (TaskFinished) isEvent()    {}
func (ApplyFinished) isEvent()   {}
func (DestroyStarted) isEvent()  {}
func (ResourceRemoved) isEvent() {}
func (DestroyFinished) isEvent() {}
func (Warning) isEvent()         {}
func (Error) isEvent()           {}

// Marshal renders one event as a JSON object whose first field is its type.
func Marshal(e Event) ([]byte, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	typ, err := json.Marshal(e.Type())
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString(`{"type":`)
	buf.Write(typ)
	if inner := bytes.TrimSpace(body[1 : len(body)-1]); len(inner) > 0 {
		buf.WriteByte(',')
		buf.Write(inner)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// JSONLines returns a Sink writing one JSON object per line to w. It is safe
// for concurrent use. A write failure is reported once, to report, and the
// sink keeps accepting events so that a broken log never stops a build.
func JSONLines(w io.Writer, report func(error)) Sink {
	var (
		mu       sync.Mutex
		reported bool
	)
	return func(e Event) {
		line, err := Marshal(e)
		if err == nil {
			line = append(line, '\n')
		}

		mu.Lock()
		defer mu.Unlock()
		if err == nil {
			_, err = w.Write(line)
		}
		if err != nil && !reported && report != nil {
			reported = true
			report(fmt.Errorf("writing event %s: %w", e.Type(), err))
		}
	}
}

package planner

import (
	"fmt"
	"reflect"
	"testing"
)

// TestComputeHashCoversEveryFieldThatDefinesABuild is a structural guard, not
// a behavioural test: it reaches for the unexported hash on purpose.
//
// The Plan hash decides whether apply rebuilds, reports up to date, or
// refuses. A field added to Plan and forgotten in computeHash would make two
// genuinely different builds look identical, which apply would then report as
// up to date. That failure is silent and expensive, so it is worth a test that
// cannot be satisfied by accident.
func TestComputeHashCoversEveryFieldThatDefinesABuild(t *testing.T) {
	// SchemaVersion is excluded so that bumping the Plan's own shape does not
	// make every existing Environment look changed. Hash cannot contain itself.
	excluded := map[string]bool{"SchemaVersion": true, "Hash": true}

	planType := reflect.TypeOf(Plan{})
	for i := 0; i < planType.NumField(); i++ {
		field := planType.Field(i)

		t.Run(field.Name, func(t *testing.T) {
			p := samplePlan()
			before := p.computeHash()

			if err := nudge(reflect.ValueOf(&p).Elem().Field(i)); err != nil {
				t.Fatalf("this guard cannot change %s (%s): %v\nextend nudge for this field type",
					field.Name, field.Type, err)
			}

			changed := p.computeHash() != before

			switch {
			case excluded[field.Name] && changed:
				t.Errorf("%s must not affect the plan hash, but changing it did", field.Name)
			case !excluded[field.Name] && !changed:
				t.Errorf("%s defines the build, but changing it left the hash alone — add it to computeHash, or exclude it deliberately in this test", field.Name)
			}
		})
	}
}

// samplePlan is a Plan with every field set, so that changing any one of them
// is a real change.
func samplePlan() Plan {
	p := Plan{
		SchemaVersion: SchemaVersion,
		Environment:   "sample",
		Backend:       "container",
		Networks:      []Network{{Name: "default", Subnet: "172.30.0.0/24"}},
		Machines:      []Machine{{Name: "node01", Image: "ubuntu:24.04", Network: "default", IP: "172.30.0.2"}},
		Tasks:         []Task{{Name: "greet", Type: "exec", Machine: "node01", Run: "echo hello", Timeout: "1m0s", Attempts: 1}},
	}
	p.Hash = p.computeHash()
	return p
}

// nudge changes a value into a different value of the same type.
func nudge(v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "-changed")
	case reflect.Int:
		v.SetInt(v.Int() + 1)
	case reflect.Slice:
		v.Set(reflect.Append(v, reflect.Zero(v.Type().Elem())))
	default:
		return fmt.Errorf("unsupported kind %s", v.Kind())
	}
	return nil
}

package fake_test

import (
	"testing"

	"sim.dev/engine/provider/conformance"
	"sim.dev/engine/provider/fake"
)

func TestConformance(t *testing.T) {
	conformance.Run(t, func(t *testing.T) conformance.Harness {
		return conformance.Harness{
			Provider: fake.New(),
			Image:    "ubuntu:24.04",
			Subnets:  []string{"10.10.1.0/24", "10.10.2.0/24"},
		}
	})
}

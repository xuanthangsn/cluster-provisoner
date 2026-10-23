//go:build docker

package container_test

import (
	"testing"

	"sim.dev/engine/provider/conformance"
)

// TestConformance is Tier A against real Docker: the same contract the fake
// passes. Run with: go test -tags docker ./provider/container
func TestConformance(t *testing.T) {
	p := newProvider(t)
	conformance.Run(t, func(t *testing.T) conformance.Harness {
		return conformance.Harness{Provider: p, Image: image, Subnets: freeSubnets(t, 2)}
	})
}

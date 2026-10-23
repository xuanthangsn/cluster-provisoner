// Package e2e holds end-to-end tests that drive the engine against real
// Docker. Every test file is behind the docker build tag, so the default test
// run needs no Docker:
//
//	go test -tags docker ./e2e
//
// These tests observe through the docker CLI, never through the engine or the
// provider, and they clean up only resources carrying their own Environment
// label — the host may run unrelated workloads.
package e2e

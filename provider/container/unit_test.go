package container

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"testing"

	"sim.dev/engine/provider"
)

func frame(stream byte, payload string) []byte {
	hdr := make([]byte, 8)
	hdr[0] = stream
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(payload)))
	return append(hdr, payload...)
}

func TestDemuxSeparatesStreamsInArrivalOrder(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(2, "err-1 "))
	in.Write(frame(1, "out-1 "))
	in.Write(frame(1, ""))
	in.Write(frame(2, "err-2"))
	in.Write(frame(1, "out-2"))

	var out, errs bytes.Buffer
	if err := demux(&in, &out, &errs); err != nil {
		t.Fatal(err)
	}
	if out.String() != "out-1 out-2" || errs.String() != "err-1 err-2" {
		t.Errorf("stdout %q, stderr %q", out.String(), errs.String())
	}
}

func TestDemuxRejectsATruncatedFrame(t *testing.T) {
	data := frame(1, "complete payload")
	if err := demux(bytes.NewReader(data[:12]), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Error("a frame cut short was accepted")
	}
}

func TestPullsAlwaysNameATag(t *testing.T) {
	for ref, want := range map[string]bool{
		"ubuntu":                         false,
		"ubuntu:24.04":                   true,
		"kindest/node:v1.35.8":           true,
		"localhost:5000/tools":           false, // the colon is the registry's port
		"localhost:5000/tools:1":         true,
		"ubuntu@sha256:0123456789abcdef": true,
	} {
		if got := hasTagOrDigest(ref); got != want {
			t.Errorf("hasTagOrDigest(%q) = %v, want %v", ref, got, want)
		}
	}
}

// TestAnUnreachableDaemonIsUnavailableAndLeaksNothing needs no Docker: it
// points the provider at a socket that does not exist.
func TestAnUnreachableDaemonIsUnavailableAndLeaksNothing(t *testing.T) {
	p, err := New(Options{Socket: "/nonexistent/docker.sock"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.List(context.Background(), "env")

	if !errors.Is(err, provider.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	var netErr net.Error
	var apiErr *apiError
	if errors.As(err, &netErr) || errors.As(err, &apiErr) {
		t.Errorf("a transport or daemon error type escaped: %#v", err)
	}
}

func TestOnlyAUnixDockerHostIsAccepted(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.1:2375")
	if _, err := New(Options{}); err == nil {
		t.Error("a TCP DOCKER_HOST was accepted")
	}
	t.Setenv("DOCKER_HOST", "unix:///run/custom.sock")
	p, err := New(Options{})
	if err != nil || p.api.socket != "/run/custom.sock" {
		t.Errorf("unix DOCKER_HOST: socket %q, err %v", p.api.socket, err)
	}
}

func TestTranslatedErrorsWrapOnlyProviderErrors(t *testing.T) {
	refused := &apiError{status: 409, message: "Conflict. The container name is already in use"}

	err := translate(refused, "creating machine m", map[int]error{409: provider.ErrAlreadyExists})
	if !errors.Is(err, provider.ErrAlreadyExists) {
		t.Errorf("mapped status lost its kind: %v", err)
	}
	var ae *apiError
	if errors.As(err, &ae) {
		t.Error("the daemon's error type escaped a mapped error")
	}

	err = translate(refused, "op", nil)
	if errors.As(err, &ae) {
		t.Error("the daemon's error type escaped an unmapped error")
	}
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("already in use")) {
		t.Errorf("the daemon's message was lost: %v", err)
	}
}

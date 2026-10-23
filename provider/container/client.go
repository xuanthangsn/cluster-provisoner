package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"sim.dev/engine/provider"
)

// apiVersion pins the Docker Engine API this provider speaks. Docker serves
// every version from its minimum up, so pinning keeps behaviour stable across
// daemon upgrades. v1.44 is Docker Engine 25; this host's daemon serves
// 1.40 to 1.54.
const apiVersion = "v1.44"

// defaultCallTimeout bounds any API call whose context carries no deadline of
// its own (NFR-5).
const defaultCallTimeout = 2 * time.Minute

// client speaks the Docker Engine API over the daemon's Unix socket.
//
// It talks HTTP directly rather than shelling out to the docker CLI because
// the CLI cannot report a command's exit code unambiguously: `docker exec`
// exits 1 both when the command exits 1 and when the daemon refuses the call,
// and a command can print the daemon's error text itself. The API reports the
// exit code from exec-inspect and daemon failures as HTTP errors, separately.
//
// It also avoids the Docker SDK, which would bring a large dependency tree for
// the dozen endpoints used here.
type client struct {
	http   *http.Client
	socket string
}

func newClient(socket string) *client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		DisableCompression: true,
	}
	return &client{http: &http.Client{Transport: transport}, socket: socket}
}

// apiError is a response the daemon refused. It never leaves this package:
// the provider translates it to the provider package's errors.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return fmt.Sprintf("%s (HTTP %d)", e.message, e.status) }

// statusOf returns the HTTP status of a daemon refusal, or 0.
func statusOf(err error) int {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.status
	}
	return 0
}

// do sends one request. A non-2xx response is returned as an *apiError with
// the body consumed; otherwise the caller must close the response body.
func (c *client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (*http.Response, error) {
	u := "http://docker/" + apiVersion + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		// %v, not %w: the transport's error types must not reach callers.
		return nil, fmt.Errorf("%w: cannot reach Docker at %s: %v — is Docker running, and may this user use its socket?",
			provider.ErrUnavailable, c.socket, err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		var msg struct{ Message string }
		if json.Unmarshal(data, &msg) != nil || msg.Message == "" {
			msg.Message = string(bytes.TrimSpace(data))
		}
		return nil, &apiError{status: resp.StatusCode, message: msg.Message}
	}
	return resp, nil
}

// json sends in (if non-nil) as JSON and decodes the response into out (if
// non-nil), bounding the call by defaultCallTimeout unless ctx already has a
// deadline.
func (c *client) json(ctx context.Context, method, path string, query url.Values, in, out any) error {
	ctx, cancel := bounded(ctx, defaultCallTimeout)
	defer cancel()

	var body io.Reader
	contentType := ""
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body, contentType = bytes.NewReader(data), "application/json"
	}

	resp, err := c.do(ctx, method, path, query, body, contentType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// bounded applies d as a deadline when ctx has none.
func bounded(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// labelFilter is the API's filters parameter selecting one label value.
func labelFilter(key, value string) url.Values {
	f, _ := json.Marshal(map[string][]string{"label": {key + "=" + value}})
	return url.Values{"filters": {string(f)}}
}

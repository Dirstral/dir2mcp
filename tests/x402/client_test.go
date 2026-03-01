package tests

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"dir2mcp/internal/x402"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type errorReader struct{ err error }

func (e *errorReader) Read(p []byte) (int, error) { return 0, e.err }
func (e *errorReader) Close() error               { return nil }

func TestDo_ReadError(t *testing.T) {
	errRead := errors.New("read failure")
	r := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(&errorReader{err: errRead}),
		Request: &http.Request{
			Method: http.MethodPost,
			URL:    &url.URL{Scheme: "https", Host: "api.example.com", Path: "/"},
		},
	}
	r.Header.Set("Content-Type", "application/json")

	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return r, nil
	})

	client := x402.NewHTTPClient("https://facilitator.test", "token", &http.Client{Transport: rt})
	// valid requirement so we get past preflight validation.  network must
	// satisfy CAIP-2 (<namespace>:<reference>), so use a simple placeholder.
	req := x402.Requirement{
		Scheme:   "scheme",
		Network:  "foo:bar",
		Amount:   "1",
		Asset:    "asset",
		PayTo:    "pay",
		Resource: "res",
	}
	_, err := client.Verify(context.Background(), "sig", req)
	if err == nil {
		t.Fatalf("expected error when reading response")
	}
	var fe *x402.FacilitatorError
	if !errors.As(err, &fe) {
		t.Fatalf("expected FacilitatorError, got %v", err)
	}
	if fe.Cause != errRead {
		t.Fatalf("expected cause to be read error; got %v", fe.Cause)
	}
}

package tests

import (
	"bytes"
	"context"
	"encoding/json"
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

// Verify currently wraps the internal do method, so this test exercises
// behavior triggered by the lower-level call.  The original name referred to
// the unexported "do" helper, but the exported API used throughout the code
// is Verify.
func TestVerify_ReadError(t *testing.T) {
	errRead := errors.New("read failure")
	r := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       &errorReader{err: errRead},

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
		Scheme:   "exact",
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

func TestRequirementValidate_SchemeWhitelist(t *testing.T) {
	cases := []struct {
		scheme       string
		expectsError bool
	}{
		{"", true},
		{"invalid", true},
		{"exact", false},
		{"EXACT", false},
		{" upto ", false},
	}
	for _, tc := range cases {
		r := x402.Requirement{
			Scheme:   tc.scheme,
			Network:  "foo:bar",
			Amount:   "1",
			Asset:    "a",
			PayTo:    "p",
			Resource: "r",
		}
		err := r.Validate()
		if tc.expectsError {
			if err == nil {
				t.Errorf("scheme %q should have failed validation", tc.scheme)
			}
		} else {
			if err != nil {
				t.Errorf("scheme %q should be accepted: %v", tc.scheme, err)
			}
		}
	}
}

func TestVerify_NormalizesSchemeInOutgoingPayload(t *testing.T) {
	var gotScheme string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			PaymentRequirements []struct {
				Scheme string `json:"scheme"`
			} `json:"paymentRequirements"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if len(body.PaymentRequirements) != 1 {
			t.Fatalf("payment requirements len=%d want=1", len(body.PaymentRequirements))
		}
		gotScheme = body.PaymentRequirements[0].Scheme
		r := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewBufferString(`{"ok":true}`)),
			Request: &http.Request{
				Method: http.MethodPost,
				URL:    &url.URL{Scheme: "https", Host: "api.example.com", Path: "/"},
			},
		}
		r.Header.Set("Content-Type", "application/json")
		return r, nil
	})

	client := x402.NewHTTPClient("https://facilitator.test", "token", &http.Client{Transport: rt})
	req := x402.Requirement{
		Scheme:   " UpTo ",
		Network:  "foo:bar",
		Amount:   "1",
		Asset:    "asset",
		PayTo:    "pay",
		Resource: "res",
	}
	if _, err := client.Verify(context.Background(), "sig", req); err != nil {
		t.Fatalf("verify error: %v", err)
	}
	if gotScheme != "upto" {
		t.Fatalf("scheme sent=%q want=%q", gotScheme, "upto")
	}
}

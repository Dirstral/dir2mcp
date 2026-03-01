package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dir2mcp/internal/config"
	"dir2mcp/internal/mcp"
)

func TestX402ToolsCall_UnpaidReturns402WithPaymentRequiredHeader(t *testing.T) {
	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":101,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPaymentRequired {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusPaymentRequired, string(payload))
	}

	requiredHeader := strings.TrimSpace(resp.Header.Get("PAYMENT-REQUIRED"))
	if requiredHeader == "" {
		t.Fatal("expected PAYMENT-REQUIRED header")
	}
	assertCanonicalErrorCode(t, readAllBytes(t, resp.Body), "PAYMENT_REQUIRED")
}

func TestX402ToolsCall_PaidRetrySucceedsAndReturnsPaymentResponse(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusOK
	fac.settleStatus = http.StatusOK
	fac.verifyBody = `{"ok":true,"kind":"verify"}`
	fac.settleBody = `{"ok":true,"kind":"settle","txHash":"abc123"}`
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":102,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}
	if strings.TrimSpace(resp.Header.Get("PAYMENT-RESPONSE")) == "" {
		t.Fatal("expected PAYMENT-RESPONSE header on successful paid call")
	}
	if fac.verifyCalls != 1 {
		t.Fatalf("verify calls=%d want=1", fac.verifyCalls)
	}
	if fac.settleCalls != 1 {
		t.Fatalf("settle calls=%d want=1", fac.settleCalls)
	}
}

func TestX402ToolsCall_VerifyTransientFailureIsRetryable503(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusServiceUnavailable
	fac.verifyBody = `{"message":"temporary outage"}`
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":103,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	assertRPCErrorCodeAndRetryable(t, resp, http.StatusServiceUnavailable, "PAYMENT_FACILITATOR_UNAVAILABLE", true)
}

func TestX402ToolsCall_VerifyInvalidReturns402WithChallenge(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusBadRequest
	fac.verifyBody = `{"message":"invalid payment"}`
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":104,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	assertRPCErrorCodeAndRetryable(t, resp, http.StatusPaymentRequired, "PAYMENT_INVALID", false)
	if strings.TrimSpace(resp.Header.Get("PAYMENT-REQUIRED")) == "" {
		t.Fatal("expected PAYMENT-REQUIRED header on 402 verify failure")
	}
	if fac.settleCalls != 0 {
		t.Fatalf("settle calls=%d want=0", fac.settleCalls)
	}
}

func TestX402ToolsCall_ToolErrorDoesNotSettle(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusOK
	fac.settleStatus = http.StatusOK
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":105,"method":"tools/call","params":{"name":"dir2mcp.unknown","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	assertToolCallErrorCode(t, resp, "METHOD_NOT_FOUND")
	if fac.verifyCalls != 1 {
		t.Fatalf("verify calls=%d want=1", fac.verifyCalls)
	}
	if fac.settleCalls != 0 {
		t.Fatalf("settle calls=%d want=0", fac.settleCalls)
	}
}

func TestX402ToolsCall_SettleTransientFailureIsRetryable503(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusOK
	fac.settleStatus = http.StatusServiceUnavailable
	fac.settleBody = `{"message":"settlement unavailable"}`
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":108,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	assertRPCErrorCodeAndRetryable(t, resp, http.StatusServiceUnavailable, "PAYMENT_SETTLEMENT_UNAVAILABLE", true)
}

func TestX402_InitializeAndToolsListRemainUngated(t *testing.T) {
	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPC(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":106,"method":"tools/list","params":{}}`)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}
}

func TestX402ToolsCall_FacilitatorBearerTokenForwarded(t *testing.T) {
	fac := newFacilitatorStub(t)
	fac.verifyStatus = http.StatusOK
	fac.settleStatus = http.StatusOK
	fac.expectedAuthorization = "Bearer facilitator-token"
	facServer := httptest.NewServer(fac)
	defer facServer.Close()

	cfg := x402EnabledTestConfig("https://resource.example.com")
	cfg.AuthMode = "none"
	cfg.X402.FacilitatorURL = facServer.URL
	cfg.X402.FacilitatorToken = "facilitator-token"

	server := httptest.NewServer(mcp.NewServer(cfg, nil).Handler())
	defer server.Close()

	sessionID := initializeSession(t, server.URL+cfg.MCPPath)
	resp := postRPCWithHeaders(t, server.URL+cfg.MCPPath, sessionID, `{"jsonrpc":"2.0","id":107,"method":"tools/call","params":{"name":"dir2mcp.stats","arguments":{}}}`, map[string]string{
		"PAYMENT-SIGNATURE": "signed-payment-payload",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, http.StatusOK, string(payload))
	}
	if fac.lastAuthorization != "Bearer facilitator-token" {
		t.Fatalf("authorization header=%q want=%q", fac.lastAuthorization, "Bearer facilitator-token")
	}
}

func x402EnabledTestConfig(resourceBaseURL string) config.Config {
	cfg := config.Default()
	cfg.X402.Mode = "on"
	cfg.X402.ToolsCallEnabled = true
	cfg.X402.ResourceBaseURL = resourceBaseURL
	cfg.X402.Scheme = "exact"
	cfg.X402.PriceAtomic = "1000"
	cfg.X402.Network = "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp"
	cfg.X402.Asset = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"
	cfg.X402.PayTo = "8N5A4rQU8vJrQmH3iiA7kE4m1df4WeyueXQqGb4G9tTj"
	return cfg
}

func postRPCWithHeaders(t *testing.T, url, sessionID, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("MCP-Session-Id", sessionID)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func assertRPCErrorCodeAndRetryable(t *testing.T, resp *http.Response, wantStatus int, wantCode string, wantRetryable bool) {
	t.Helper()

	if resp.StatusCode != wantStatus {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, wantStatus, string(payload))
	}

	var envelope struct {
		Error struct {
			Data struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Data.Code != wantCode {
		t.Fatalf("code=%q want=%q", envelope.Error.Data.Code, wantCode)
	}
	if envelope.Error.Data.Retryable != wantRetryable {
		t.Fatalf("retryable=%t want=%t", envelope.Error.Data.Retryable, wantRetryable)
	}
}

func readAllBytes(t *testing.T, r io.Reader) []byte {
	t.Helper()
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return raw
}

type facilitatorStub struct {
	t                     *testing.T
	verifyStatus          int
	settleStatus          int
	verifyBody            string
	settleBody            string
	verifyCalls           int
	settleCalls           int
	expectedAuthorization string
	lastAuthorization     string
}

func newFacilitatorStub(t *testing.T) *facilitatorStub {
	return &facilitatorStub{
		t:            t,
		verifyStatus: http.StatusOK,
		settleStatus: http.StatusOK,
		verifyBody:   `{"ok":true}`,
		settleBody:   `{"ok":true}`,
	}
}

func (f *facilitatorStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.lastAuthorization = r.Header.Get("Authorization")
	if f.expectedAuthorization != "" && f.lastAuthorization != f.expectedAuthorization {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
		return
	}

	switch r.URL.Path {
	case "/v2/x402/verify":
		f.verifyCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.verifyStatus)
		_, _ = w.Write([]byte(f.verifyBody))
	case "/v2/x402/settle":
		f.settleCalls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(f.settleStatus)
		_, _ = w.Write([]byte(f.settleBody))
	default:
		http.NotFound(w, r)
	}
}

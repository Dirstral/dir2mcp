package tests

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dir2mcp/internal/config"
	"dir2mcp/internal/mcp"
)

func TestRateLimit_NotActiveWhenServerIsNotPublic(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.Public = false
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	handler := mcp.NewServer(cfg, nil).Handler()
	for i := 0; i < 5; i++ {
		rr := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.10")
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d status=%d want=%d body=%s", i, rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}

func TestRateLimit_ExceedingLimitReturns429AndRetryAfter(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.Public = true
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	handler := mcp.NewServer(cfg, nil).Handler()

	first := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.20")
	if first.Code != http.StatusOK {
		t.Fatalf("first request status=%d want=%d body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	second := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.20")
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status=%d want=%d body=%s", second.Code, http.StatusTooManyRequests, second.Body.String())
	}
	if second.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After=%q want=%q", second.Header().Get("Retry-After"), "1")
	}
	assertCanonicalErrorCode(t, second.Body.Bytes(), "RATE_LIMIT_EXCEEDED")
}

func TestRateLimit_TrafficBelowLimitPasses(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.Public = true
	cfg.RateLimitRPS = 10
	cfg.RateLimitBurst = 1

	handler := mcp.NewServer(cfg, nil).Handler()

	first := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.30")
	if first.Code != http.StatusOK {
		t.Fatalf("first request status=%d want=%d body=%s", first.Code, http.StatusOK, first.Body.String())
	}

	time.Sleep(150 * time.Millisecond)

	second := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.30")
	if second.Code != http.StatusOK {
		t.Fatalf("second request status=%d want=%d body=%s", second.Code, http.StatusOK, second.Body.String())
	}
}

func TestRateLimit_LoopbackIPsAreAlwaysExempt(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.Public = true
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	handler := mcp.NewServer(cfg, nil).Handler()

	for _, ip := range []string{"127.0.0.1", "::1", "localhost"} {
		for i := 0; i < 5; i++ {
			rr := initializeRequestFromIP(t, handler, cfg.MCPPath, ip)
			if rr.Code != http.StatusOK {
				t.Fatalf("loopback ip=%q req=%d status=%d want=%d body=%s", ip, i, rr.Code, http.StatusOK, rr.Body.String())
			}
		}
	}
}

func TestRateLimit_BucketsAreIndependentPerIP(t *testing.T) {
	cfg := config.Default()
	cfg.AuthMode = "none"
	cfg.Public = true
	cfg.RateLimitRPS = 1
	cfg.RateLimitBurst = 1

	handler := mcp.NewServer(cfg, nil).Handler()

	a1 := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.40")
	if a1.Code != http.StatusOK {
		t.Fatalf("first request from ip A status=%d want=%d body=%s", a1.Code, http.StatusOK, a1.Body.String())
	}

	a2 := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.40")
	if a2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request from ip A status=%d want=%d body=%s", a2.Code, http.StatusTooManyRequests, a2.Body.String())
	}

	b1 := initializeRequestFromIP(t, handler, cfg.MCPPath, "198.51.100.41")
	if b1.Code != http.StatusOK {
		t.Fatalf("first request from ip B status=%d want=%d body=%s", b1.Code, http.StatusOK, b1.Body.String())
	}
}

func initializeRequestFromIP(t *testing.T, handler http.Handler, path, ip string) *httptest.ResponseRecorder {
	t.Helper()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")
	if ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

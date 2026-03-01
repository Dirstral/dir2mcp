package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"net/http"

	"dir2mcp/internal/x402"
)

func (s *Server) initPaymentConfig() {
	mode := x402.NormalizeMode(s.cfg.X402.Mode)
	if !x402.IsModeEnabled(mode) || !s.cfg.X402.ToolsCallEnabled {
		return
	}

	s.x402Requirement = x402.Requirement{
		Scheme:   strings.TrimSpace(s.cfg.X402.Scheme),
		Network:  strings.TrimSpace(s.cfg.X402.Network),
		Amount:   strings.TrimSpace(s.cfg.X402.PriceAtomic),
		Asset:    strings.TrimSpace(s.cfg.X402.Asset),
		PayTo:    strings.TrimSpace(s.cfg.X402.PayTo),
		Resource: strings.TrimSpace(buildPaymentResourceURL(s.cfg.X402.ResourceBaseURL, s.cfg.MCPPath)),
	}
	s.x402Client = x402.NewHTTPClient(s.cfg.X402.FacilitatorURL, s.cfg.X402.FacilitatorToken, nil)
	s.x402Enabled = true
	s.paymentLogPath = filepath.Join(s.cfg.StateDir, "payments", "settlement.log")
}

func buildPaymentResourceURL(baseURL, mcpPath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	if !strings.HasPrefix(mcpPath, "/") {
		mcpPath = "/" + mcpPath
	}
	return baseURL + mcpPath
}

func (s *Server) handleToolsCallRequest(ctx context.Context, w http.ResponseWriter, r *http.Request, rawParams json.RawMessage, id interface{}) {
	if !s.x402Enabled {
		s.handleToolsCall(ctx, w, rawParams, id)
		return
	}

	paymentSignature := strings.TrimSpace(r.Header.Get(x402.HeaderPaymentSignature))
	if paymentSignature == "" {
		s.emitPaymentEvent("info", "payment_required", map[string]interface{}{
			"reason": "missing_payment_signature",
		})
		s.writePaymentChallenge(w, id, x402.CodePaymentRequired, "payment required", false)
		return
	}

	verifyResponse, err := s.x402Client.Verify(ctx, paymentSignature, s.x402Requirement)
	if err != nil {
		s.handlePaymentFailure(w, id, "verify", err)
		return
	}
	s.emitPaymentEvent("info", "payment_verified", map[string]interface{}{
		"response": json.RawMessage(verifyResponse),
	})
	s.appendPaymentLog("payment_verified", map[string]interface{}{
		"response": json.RawMessage(verifyResponse),
	})

	result, statusCode, rpcErr := s.processToolsCall(ctx, rawParams)
	if rpcErr != nil {
		writeResponse(w, statusCode, rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   rpcErr,
		})
		return
	}
	if result.IsError {
		writeResult(w, statusCode, id, result)
		return
	}

	settleResponse, err := s.x402Client.Settle(ctx, paymentSignature, s.x402Requirement)
	if err != nil {
		s.handlePaymentFailure(w, id, "settle", err)
		return
	}

	w.Header().Set(x402.HeaderPaymentResponse, string(settleResponse))
	writeResult(w, statusCode, id, result)

	s.emitPaymentEvent("info", "payment_settled", map[string]interface{}{
		"response": json.RawMessage(settleResponse),
	})
	s.appendPaymentLog("payment_settled", map[string]interface{}{
		"response": json.RawMessage(settleResponse),
	})
}

func (s *Server) handlePaymentFailure(w http.ResponseWriter, id interface{}, operation string, err error) {
	facErr, ok := err.(*x402.FacilitatorError)
	if !ok {
		facErr = &x402.FacilitatorError{
			Operation: operation,
			Code:      x402.CodePaymentFacilitatorUnavailable,
			Message:   "payment processing failed",
			Retryable: true,
			Cause:     err,
		}
	}

	statusCode := http.StatusServiceUnavailable
	includeChallenge := false
	switch facErr.Code {
	case x402.CodePaymentRequired:
		statusCode = http.StatusPaymentRequired
		includeChallenge = true
	case x402.CodePaymentInvalid, x402.CodePaymentSettlementFailed:
		statusCode = http.StatusPaymentRequired
		includeChallenge = true
	case x402.CodePaymentConfigInvalid:
		statusCode = http.StatusServiceUnavailable
	default:
		if facErr.StatusCode >= 400 && facErr.StatusCode < 500 && !facErr.Retryable {
			statusCode = http.StatusPaymentRequired
			includeChallenge = true
		}
	}

	s.emitPaymentEvent("error", "payment_failed", map[string]interface{}{
		"operation": operation,
		"code":      facErr.Code,
		"message":   facErr.Message,
		"retryable": facErr.Retryable,
		"status":    facErr.StatusCode,
	})
	s.appendPaymentLog("payment_failed", map[string]interface{}{
		"operation": operation,
		"code":      facErr.Code,
		"message":   facErr.Message,
		"retryable": facErr.Retryable,
		"status":    facErr.StatusCode,
	})

	if includeChallenge {
		s.writePaymentChallenge(w, id, facErr.Code, facErr.Message, facErr.Retryable)
		return
	}
	writeError(w, statusCode, id, -32000, facErr.Message, facErr.Code, facErr.Retryable)
}

func (s *Server) writePaymentChallenge(w http.ResponseWriter, id interface{}, code, message string, retryable bool) {
	headerValue, err := x402.BuildPaymentRequiredHeaderValue(s.x402Requirement)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, id, -32000, err.Error(), x402.CodePaymentConfigInvalid, false)
		return
	}
	w.Header().Set(x402.HeaderPaymentRequired, headerValue)
	writeError(w, http.StatusPaymentRequired, id, -32000, message, code, retryable)
}

func (s *Server) emitPaymentEvent(level, event string, data interface{}) {
	if s.eventEmitter == nil {
		return
	}
	s.eventEmitter(level, event, data)
}

func (s *Server) appendPaymentLog(event string, data map[string]interface{}) {
	if strings.TrimSpace(s.paymentLogPath) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.paymentLogPath), 0o755); err != nil {
		return
	}
	entry := map[string]interface{}{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"event": event,
		"data":  data,
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(s.paymentLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() {
		_ = f.Close()
	}()
	_, _ = fmt.Fprintf(f, "%s\n", raw)
}

package mcp

import (
	"errors"
	"strings"

	"dir2mcp/internal/protocol"
	"dir2mcp/internal/x402"
)

const (
	CanonicalCodeUnauthorized     = protocol.ErrorCodeUnauthorized
	CanonicalCodeSessionNotFound  = protocol.ErrorCodeSessionNotFound
	CanonicalCodeIndexNotReady    = protocol.ErrorCodeIndexNotReady
	CanonicalCodeFileNotFound     = protocol.ErrorCodeFileNotFound
	CanonicalCodePermissionDenied = protocol.ErrorCodePermissionDenied
	CanonicalCodeRateLimited      = protocol.ErrorCodeRateLimited
	CanonicalCodePaymentRequired  = x402.CodePaymentRequired
)

// CanonicalCodeFromError extracts a backend canonical code when present.
func CanonicalCodeFromError(err error) string {
	if err == nil {
		return ""
	}

	var rpcErr *jsonRPCError
	if errors.As(err, &rpcErr) {
		if rpcErr.isPaymentRequired() {
			return CanonicalCodePaymentRequired
		}
		if code := canonicalCodeFromText(rpcErr.Message); code != "" {
			return code
		}
	}

	return canonicalCodeFromText(err.Error())
}

// ActionableMessageForCode maps a canonical backend code to user guidance.
func ActionableMessageForCode(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case CanonicalCodeUnauthorized:
		return "Authentication failed (UNAUTHORIZED). Set DIR2MCP_AUTH_TOKEN or refresh your credentials, then retry."
	case CanonicalCodeSessionNotFound:
		return "The MCP session was not found. Reconnect to the server and retry your command."
	case CanonicalCodeIndexNotReady:
		return "The index is not ready yet. Wait for indexing to finish, then retry."
	case CanonicalCodeFileNotFound:
		return "The requested file was not found. Verify the path or use list_files/search first."
	case CanonicalCodePermissionDenied:
		return "Permission denied for this operation. Check server auth/scope and retry."
	case CanonicalCodeRateLimited:
		return "Request rate limit reached. Wait briefly and retry."
	case CanonicalCodePaymentRequired:
		return "This tool requires payment. Run with x402 enabled or configure a payment token."
	default:
		return ""
	}
}

// ActionableMessageFromError derives canonical code and returns user guidance.
func ActionableMessageFromError(err error) string {
	return ActionableMessageForCode(CanonicalCodeFromError(err))
}

func canonicalCodeFromText(text string) string {
	upper := strings.ToUpper(text)

	if containsCanonicalPhrase(upper, CanonicalCodeUnauthorized) || containsCanonicalPhrase(upper, "UNAUTHENTICATED") {
		return CanonicalCodeUnauthorized
	}
	if containsCanonicalPhrase(upper, CanonicalCodeSessionNotFound) || containsCanonicalPhrase(upper, "SESSION NOT FOUND") {
		return CanonicalCodeSessionNotFound
	}
	if containsCanonicalPhrase(upper, CanonicalCodeIndexNotReady) || containsCanonicalPhrase(upper, "INDEX NOT READY") {
		return CanonicalCodeIndexNotReady
	}
	if containsCanonicalPhrase(upper, CanonicalCodeFileNotFound) || containsCanonicalPhrase(upper, "FILE NOT FOUND") {
		return CanonicalCodeFileNotFound
	}
	if containsAny(upper,
		CanonicalCodePermissionDenied,
		"PERMISSION DENIED",
		"ACCESS DENIED",
		"FORBIDDEN",
		"NOT AUTHORIZED",
	) {
		return CanonicalCodePermissionDenied
	}
	if containsAny(upper,
		CanonicalCodeRateLimited,
		"RATE LIMIT",
		"RATE-LIMIT",
		"RATE_LIMIT",
		"RATE LIMIT EXCEEDED",
		"TOO MANY REQUESTS",
	) {
		return CanonicalCodeRateLimited
	}
	if containsAnyWithContext(upper,
		[]string{"QUOTA", "LIMIT EXCEEDED", "THROTTLE", "THROTTLED"},
		[]string{"REQUEST", "API", "RATE", "HTTP", "CALL"},
	) {
		return CanonicalCodeRateLimited
	}
	if containsAny(upper,
		CanonicalCodePaymentRequired,
		"PAYMENT REQUIRED",
		"PAYMENT-REQUIRED",
	) {
		return CanonicalCodePaymentRequired
	}

	return ""
}

func containsAny(value string, patterns ...string) bool {
	for _, pattern := range patterns {
		if containsCanonicalPhrase(value, pattern) {
			return true
		}
	}
	return false
}

func containsAnyWithContext(value string, patterns []string, contextWords []string) bool {
	if len(patterns) == 0 || len(contextWords) == 0 {
		return false
	}
	hasContext := false
	for _, contextWord := range contextWords {
		if containsCanonicalPhrase(value, contextWord) {
			hasContext = true
			break
		}
	}
	if !hasContext {
		return false
	}
	return containsAny(value, patterns...)
}

func containsCanonicalPhrase(value, phrase string) bool {
	valueTokens := canonicalTokens(value)
	phraseTokens := canonicalTokens(phrase)
	if len(phraseTokens) == 0 || len(valueTokens) < len(phraseTokens) {
		return false
	}
	for i := 0; i <= len(valueTokens)-len(phraseTokens); i++ {
		matched := true
		for j := range phraseTokens {
			if valueTokens[i+j] != phraseTokens[j] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func canonicalTokens(value string) []string {
	normalized := strings.NewReplacer("_", " ", "-", " ").Replace(strings.ToUpper(strings.TrimSpace(value)))
	if normalized == "" {
		return nil
	}
	return strings.FieldsFunc(normalized, func(r rune) bool {
		return (r < 'A' || r > 'Z') && (r < '0' || r > '9')
	})
}

package x402

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultHTTPTimeout = 10 * time.Second

type HTTPClient struct {
	baseURL     string
	bearerToken string
	httpClient  *http.Client
}

func NewHTTPClient(baseURL, bearerToken string, httpClient *http.Client) *HTTPClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &HTTPClient{
		baseURL:     baseURL,
		bearerToken: strings.TrimSpace(bearerToken),
		httpClient:  httpClient,
	}
}

func (c *HTTPClient) Verify(ctx context.Context, paymentSignature string, req Requirement) (json.RawMessage, error) {
	return c.do(ctx, "verify", paymentSignature, req)
}

func (c *HTTPClient) Settle(ctx context.Context, paymentSignature string, req Requirement) (json.RawMessage, error) {
	return c.do(ctx, "settle", paymentSignature, req)
}

func (c *HTTPClient) do(ctx context.Context, operation, paymentSignature string, req Requirement) (json.RawMessage, error) {
	if strings.TrimSpace(c.baseURL) == "" {
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentConfigInvalid,
			Message:   "x402 facilitator URL is required",
			Retryable: false,
		}
	}
	if err := req.Validate(); err != nil {
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentConfigInvalid,
			Message:   err.Error(),
			Retryable: false,
			Cause:     err,
		}
	}
	paymentSignature = strings.TrimSpace(paymentSignature)
	if paymentSignature == "" {
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentRequired,
			Message:   "missing payment signature",
			Retryable: false,
		}
	}

	endpoint, err := url.JoinPath(c.baseURL, "v2", "x402", operation)
	if err != nil {
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentConfigInvalid,
			Message:   "invalid facilitator URL",
			Retryable: false,
			Cause:     err,
		}
	}

	body := map[string]interface{}{
		"paymentPayload": paymentSignature,
		"paymentRequirements": []map[string]interface{}{
			toRequirementPayload(req),
		},
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentConfigInvalid,
			Message:   "failed to serialize facilitator request",
			Retryable: false,
			Cause:     err,
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		// request construction failures are programming/validation issues; not
		// retryable since a retry will never succeed.
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentFacilitatorUnavailable,
			Message:   "failed to create facilitator request",
			Retryable: false,
			Cause:     err,
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		code := CodePaymentFacilitatorUnavailable
		if operation == "settle" {
			code = CodePaymentSettlementUnavailable
		}
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      code,
			Message:   "facilitator request failed",
			Retryable: true,
			Cause:     err,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		// reading the response failed; wrap in a FacilitatorError so callers
		// can handle it like other transport-level failures.  This situation
		// is unlikely but we treat it as retryable since it usually indicates
		// a transient network or server problem.
		return nil, &FacilitatorError{
			Operation: operation,
			Code:      CodePaymentFacilitatorUnavailable,
			Message:   "failed to read facilitator response",
			Retryable: true,
			Cause:     err,
		}
	}
	normalized := normalizeResponsePayload(respBody)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return normalized, nil
	}

	retryable := isRetryableStatus(resp.StatusCode)
	code := CodePaymentInvalid
	if operation == "settle" {
		code = CodePaymentSettlementFailed
	}
	if retryable {
		if operation == "settle" {
			code = CodePaymentSettlementUnavailable
		} else {
			code = CodePaymentFacilitatorUnavailable
		}
	}

	message := strings.TrimSpace(extractFacilitatorMessage(respBody))
	if message == "" {
		message = fmt.Sprintf("facilitator %s request failed with status %d", operation, resp.StatusCode)
	}

	return nil, &FacilitatorError{
		Operation:  operation,
		StatusCode: resp.StatusCode,
		Retryable:  retryable,
		Code:       code,
		Message:    message,
		Body:       string(normalized),
	}
}

func toRequirementPayload(req Requirement) map[string]interface{} {
	return map[string]interface{}{
		"scheme":            strings.TrimSpace(req.Scheme),
		"network":           strings.TrimSpace(req.Network),
		"amount":            strings.TrimSpace(req.Amount),
		"maxAmountRequired": strings.TrimSpace(req.Amount),
		"asset":             strings.TrimSpace(req.Asset),
		"payTo":             strings.TrimSpace(req.PayTo),
		"resource":          strings.TrimSpace(req.Resource),
	}
}

func isRetryableStatus(status int) bool {
	if status >= 500 {
		return true
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusTooEarly:
		return true
	default:
		return false
	}
}

func normalizeResponsePayload(payload []byte) json.RawMessage {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return json.RawMessage(`{}`)
	}

	var check json.RawMessage
	if err := json.Unmarshal(trimmed, &check); err == nil {
		return json.RawMessage(trimmed)
	}

	fallback, _ := json.Marshal(map[string]string{
		"raw": string(trimmed),
	})
	return json.RawMessage(fallback)
}

func extractFacilitatorMessage(payload []byte) string {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 {
		return ""
	}

	var asObj map[string]interface{}
	if err := json.Unmarshal(trimmed, &asObj); err != nil {
		return string(trimmed)
	}
	for _, key := range []string{"message", "error", "reason"} {
		if raw, ok := asObj[key]; ok {
			switch value := raw.(type) {
			case string:
				return value
			case map[string]interface{}:
				if msg, ok := value["message"].(string); ok {
					return msg
				}
			}
		}
	}
	return ""
}

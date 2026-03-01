package x402

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	HeaderPaymentRequired  = "PAYMENT-REQUIRED"
	HeaderPaymentSignature = "PAYMENT-SIGNATURE"
	HeaderPaymentResponse  = "PAYMENT-RESPONSE"

	ModeOff      = "off"
	ModeOn       = "on"
	ModeRequired = "required"

	CodePaymentRequired               = "PAYMENT_REQUIRED"
	CodePaymentInvalid                = "PAYMENT_INVALID"
	CodePaymentFacilitatorUnavailable = "PAYMENT_FACILITATOR_UNAVAILABLE"
	CodePaymentSettlementFailed       = "PAYMENT_SETTLEMENT_FAILED"
	CodePaymentSettlementUnavailable  = "PAYMENT_SETTLEMENT_UNAVAILABLE"
	CodePaymentConfigInvalid          = "PAYMENT_CONFIG_INVALID"
)

type Requirement struct {
	Scheme   string
	Network  string
	Amount   string
	Asset    string
	PayTo    string
	Resource string
}

func (r Requirement) Validate() error {
	if strings.TrimSpace(r.Scheme) == "" {
		return fmt.Errorf("x402 scheme is required")
	}
	if strings.TrimSpace(r.Network) == "" {
		return fmt.Errorf("x402 network is required")
	}
	if !IsCAIP2Network(r.Network) {
		return fmt.Errorf("x402 network must be CAIP-2")
	}
	if strings.TrimSpace(r.Amount) == "" {
		return fmt.Errorf("x402 amount is required")
	}
	if strings.TrimSpace(r.Asset) == "" {
		return fmt.Errorf("x402 asset is required")
	}
	if strings.TrimSpace(r.PayTo) == "" {
		return fmt.Errorf("x402 pay_to is required")
	}
	if strings.TrimSpace(r.Resource) == "" {
		return fmt.Errorf("x402 resource is required")
	}
	return nil
}

// BuildPaymentRequiredHeaderValue returns a machine-readable challenge payload
// suitable for the PAYMENT-REQUIRED header.
func BuildPaymentRequiredHeaderValue(req Requirement) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}

	payload := map[string]interface{}{
		"x402Version": 2,
		"accepts": []map[string]interface{}{
			{
				"scheme":            strings.TrimSpace(req.Scheme),
				"network":           strings.TrimSpace(req.Network),
				"amount":            strings.TrimSpace(req.Amount),
				"maxAmountRequired": strings.TrimSpace(req.Amount),
				"asset":             strings.TrimSpace(req.Asset),
				"payTo":             strings.TrimSpace(req.PayTo),
				"resource":          strings.TrimSpace(req.Resource),
			},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func NormalizeMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", ModeOff:
		return ModeOff
	case ModeOn:
		return ModeOn
	case ModeRequired:
		return ModeRequired
	default:
		return mode
	}
}

func IsModeValid(mode string) bool {
	switch NormalizeMode(mode) {
	case ModeOff, ModeOn, ModeRequired:
		return true
	default:
		return false
	}
}

func IsModeEnabled(mode string) bool {
	switch NormalizeMode(mode) {
	case ModeOn, ModeRequired:
		return true
	default:
		return false
	}
}

// IsCAIP2Network validates a conservative CAIP-2 identifier shape:
// <namespace>:<reference>
func IsCAIP2Network(network string) bool {
	network = strings.TrimSpace(network)
	parts := strings.Split(network, ":")
	if len(parts) != 2 {
		return false
	}

	ns := parts[0]
	ref := parts[1]
	if len(ns) == 0 || len(ns) > 32 || len(ref) == 0 || len(ref) > 128 {
		return false
	}

	for _, r := range ns {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	for _, r := range ref {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

type FacilitatorError struct {
	Operation  string
	StatusCode int
	Retryable  bool
	Code       string
	Message    string
	Body       string
	Cause      error
}

func (e *FacilitatorError) Error() string {
	if e == nil {
		return "<nil FacilitatorError>"
	}
	if e.Code == "" && e.Message == "" {
		return "facilitator request failed"
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func (e *FacilitatorError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

package x402

import (
	"encoding/json"
	"fmt"
	"math/big"
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

	// X402Version is the current protocol version encoded in the
	// PAYMENT-REQUIRED header payload.  It’s a small integer that
	// allows clients/servers to evolve the format in a backwards-
	// compatible way, and having it as a constant makes updates
	// straightforward.
	X402Version = 2
)

type Requirement struct {
	Scheme   string
	Network  string
	Amount   string
	Asset    string
	PayTo    string
	Resource string
}

// X402Payload represents the JSON object returned in the PAYMENT-REQUIRED header.
// It mirrors the previous map structure but provides compile-time safety.
//
// Consumers depend on the field names to match the existing keys, so the tags
// are chosen accordingly.
type X402Payload struct {
	X402Version int           `json:"x402Version"`
	Accept      []AcceptEntry `json:"accepts"`
}

// AcceptEntry describes a single acceptable payment requirement.
// The json tags match the keys previously used in the map literal.
type AcceptEntry struct {
	Scheme            string `json:"scheme"`
	Network           string `json:"network"`
	Amount            string `json:"amount"`
	MaxAmountRequired string `json:"maxAmountRequired"`
	Asset             string `json:"asset"`
	PayTo             string `json:"payTo"`
	Resource          string `json:"resource"`
}

const allowedSchemesText = "exact, upto"

func (r Requirement) Validate() error {
	// normalize and check scheme value
	scheme := strings.ToLower(strings.TrimSpace(r.Scheme))
	if scheme == "" {
		return fmt.Errorf("x402 scheme is required")
	}
	switch scheme {
	case "exact", "upto":
	default:
		return fmt.Errorf("x402 scheme must be one of: %s", allowedSchemesText)
	}
	if strings.TrimSpace(r.Network) == "" {
		return fmt.Errorf("x402 network is required")
	}
	if !IsCAIP2Network(r.Network) {
		return fmt.Errorf("x402 network must be CAIP-2")
	}
	// amount must be a non-empty positive integer.
	amt := strings.TrimSpace(r.Amount)
	if amt == "" {
		return fmt.Errorf("x402 amount is required")
	}
	value := new(big.Int)
	if _, ok := value.SetString(amt, 10); !ok || value.Sign() <= 0 {
		return fmt.Errorf("x402 amount must be a positive integer")
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

	// assemble a typed payload rather than a loose map; this aids
	// compile-time checking and prevents inadvertent typos.
	p := X402Payload{
		X402Version: X402Version,
		Accept: []AcceptEntry{
			{
				Scheme:            strings.ToLower(strings.TrimSpace(req.Scheme)),
				Network:           strings.TrimSpace(req.Network),
				Amount:            strings.TrimSpace(req.Amount),
				MaxAmountRequired: strings.TrimSpace(req.Amount),
				Asset:             strings.TrimSpace(req.Asset),
				PayTo:             strings.TrimSpace(req.PayTo),
				Resource:          strings.TrimSpace(req.Resource),
			},
		},
	}

	raw, err := json.Marshal(p)
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

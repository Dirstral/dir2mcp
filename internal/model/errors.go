package model

import "errors"

var (
	// ErrNotImplemented marks skeleton methods that still need subsystem work.
	ErrNotImplemented = errors.New("not implemented")
)

type ProviderError struct {
	Code       string
	Message    string
	Retryable  bool
	StatusCode int
	Cause      error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "<nil ProviderError>"
	}
	if e.Code == "" && e.Message == "" {
		return "<empty ProviderError>"
	}
	if e.Code == "" {
		return e.Message
	}
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

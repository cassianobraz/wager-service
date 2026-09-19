package domainerr

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidArgument   = errors.New("domain: invalid argument")
	ErrConflict          = errors.New("domain: conflicting request")
	ErrNotFound          = errors.New("domain: not found")
	ErrInvalidTransition = errors.New("domain: invalid state transition")
)

type FailureCode string

const (
	FailureInsufficientFunds      FailureCode = "INSUFFICIENT_FUNDS"
	FailureReversalExceedsBalance FailureCode = "REVERSAL_EXCEEDS_BALANCE"
	FailureReferenceNotFound      FailureCode = "REFERENCE_NOT_FOUND"
	FailureReferenceMismatch      FailureCode = "REFERENCE_MISMATCH"
	FailureReferenceNotProcessed  FailureCode = "REFERENCE_NOT_PROCESSED"
	FailureDuplicateReversal      FailureCode = "DUPLICATE_REVERSAL"
	FailureInvalidTransition      FailureCode = "INVALID_TRANSITION"
)

type BusinessRejection struct {
	Code    FailureCode
	Message string
}

func (e *BusinessRejection) Error() string {
	return fmt.Sprintf("domain: business rejection [%s]: %s", e.Code, e.Message)
}

func NewBusinessRejection(code FailureCode, message string) *BusinessRejection {
	return &BusinessRejection{Code: code, Message: message}
}

type InvalidTransitionError struct {
	From string
	To   string
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("domain: invalid transition from %s to %s", e.From, e.To)
}

func (e *InvalidTransitionError) Is(target error) bool {
	return target == ErrInvalidTransition
}

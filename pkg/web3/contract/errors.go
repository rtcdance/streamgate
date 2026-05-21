package contract

import (
	"errors"
	"fmt"
)

type DualError struct {
	Primary   error
	Secondary error
}

func (e *DualError) Error() string {
	return fmt.Sprintf("call failed on both proxy (%v) and implementation (%v)", e.Primary, e.Secondary)
}

func (e *DualError) Unwrap() []error { return []error{e.Primary, e.Secondary} }

func (e *DualError) IsRetryable() bool {
	var r retryable
	if errors.As(e.Primary, &r) && !r.IsRetryable() {
		return false
	}
	if errors.As(e.Secondary, &r) && !r.IsRetryable() {
		return false
	}
	if errors.As(e.Primary, &r) || errors.As(e.Secondary, &r) {
		return true
	}
	return false
}

type retryable interface {
	IsRetryable() bool
}

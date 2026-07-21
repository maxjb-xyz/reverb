package download

import (
	"errors"
	"testing"
)

func TestClassifiedErrorUnwrapsAndReportsClass(t *testing.T) {
	inner := errors.New("HTTP 429: Too Many Requests")
	ce := ClassifiedError{Class: ClassRateLimited, Err: inner}

	if ce.Error() != inner.Error() {
		t.Errorf("Error() = %q, want %q", ce.Error(), inner.Error())
	}
	if !errors.Is(ce, inner) {
		t.Error("errors.Is(ce, inner) = false, want true (Unwrap must expose inner)")
	}

	var got ClassifiedError
	wrapped := errors.New("wrapper")
	_ = wrapped
	if !errors.As(error(ce), &got) {
		t.Fatal("errors.As should extract ClassifiedError from itself")
	}
	if got.Class != ClassRateLimited {
		t.Errorf("got.Class = %q, want %q", got.Class, ClassRateLimited)
	}
}

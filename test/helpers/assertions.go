package helpers

import (
	"testing"
)

// AssertNoError fails the test if err is not nil.
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// AssertNotEmpty fails the test if s is empty.
func AssertNotEmpty(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		t.Fatal("expected non-empty value, got empty string")
	}
}

// AssertEqual fails the test if expected != actual.
func AssertEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	if expected != actual {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}

func AssertTrue(t *testing.T, condition bool) {
	t.Helper()
	if !condition {
		t.Fatal("expected condition to be true, got false")
	}
}

func AssertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
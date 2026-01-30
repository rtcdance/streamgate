package helpers

import (
	"reflect"
	"testing"
)

// AssertNoError asserts that no error occurred
func AssertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

// AssertError asserts that an error occurred
func AssertError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Expected an error, got nil")
	}
}

// AssertEqual asserts two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Expected %v, got %v", expected, actual)
	}
}

// AssertNotEqual asserts two values are not equal
func AssertNotEqual(t *testing.T, expected, actual interface{}) {
	t.Helper()
	if reflect.DeepEqual(expected, actual) {
		t.Fatalf("Expected values to be different, both are %v", expected)
	}
}

// AssertTrue asserts a condition is true
func AssertTrue(t *testing.T, condition bool) {
	t.Helper()
	if !condition {
		t.Fatal("Expected condition to be true, got false")
	}
}

// AssertFalse asserts a condition is false
func AssertFalse(t *testing.T, condition bool) {
	t.Helper()
	if condition {
		t.Fatal("Expected condition to be false, got true")
	}
}

// AssertNil asserts a value is nil
func AssertNil(t *testing.T, value interface{}) {
	t.Helper()
	if value != nil && !reflect.ValueOf(value).IsNil() {
		t.Fatalf("Expected nil, got %v", value)
	}
}

// AssertNotNil asserts a value is not nil
func AssertNotNil(t *testing.T, value interface{}) {
	t.Helper()
	if value == nil {
		t.Fatal("Expected non-nil value, got nil")
	}

	// Check if the value is a pointer, interface, map, slice, or function
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		if rv.IsNil() {
			t.Fatal("Expected non-nil value, got nil")
		}
	}
}

// AssertContains asserts a string contains a substring
func AssertContains(t *testing.T, str, substr string) {
	t.Helper()
	if !contains(str, substr) {
		t.Fatalf("Expected %q to contain %q", str, substr)
	}
}

// AssertNotContains asserts a string does not contain a substring
func AssertNotContains(t *testing.T, str, substr string) {
	t.Helper()
	if contains(str, substr) {
		t.Fatalf("Expected %q to not contain %q", str, substr)
	}
}

// AssertLen asserts a slice/array/map has a specific length
func AssertLen(t *testing.T, obj interface{}, length int) {
	t.Helper()
	v := reflect.ValueOf(obj)
	if v.Len() != length {
		t.Fatalf("Expected length %d, got %d", length, v.Len())
	}
}

// AssertEmpty asserts a slice/array/map is empty
func AssertEmpty(t *testing.T, obj interface{}) {
	t.Helper()
	v := reflect.ValueOf(obj)
	if v.Len() != 0 {
		t.Fatalf("Expected empty collection, got length %d", v.Len())
	}
}

// AssertNotEmpty asserts a slice/array/map is not empty
func AssertNotEmpty(t *testing.T, obj interface{}) {
	t.Helper()
	v := reflect.ValueOf(obj)
	if v.Len() == 0 {
		t.Fatal("Expected non-empty collection, got empty")
	}
}

// AssertPanic asserts a function panics
func AssertPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Expected function to panic, but it didn't")
		}
	}()
	fn()
}

// AssertNoPanic asserts a function does not panic
func AssertNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Expected function not to panic, but it panicked with: %v", r)
		}
	}()
	fn()
}

// contains checks if a string contains a substring
func contains(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || len(substr) == 0 ||
		(len(str) > 0 && len(substr) > 0 && findSubstring(str, substr)))
}

// findSubstring finds a substring in a string
func findSubstring(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

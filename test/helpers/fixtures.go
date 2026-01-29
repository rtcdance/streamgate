package helpers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// LoadFixture loads a JSON fixture file
func LoadFixture(t *testing.T, filename string, v interface{}) {
	t.Helper()

	// Try relative path from test directory
	path := filepath.Join("../fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		// Try from project root
		path = filepath.Join("test/fixtures", filename)
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to load fixture %s: %v", filename, err)
		}
	}

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("Failed to parse fixture %s: %v", filename, err)
	}
}

// LoadTestData loads test data from testdata directory
func LoadTestData(t *testing.T, filename string) []byte {
	t.Helper()

	// Try relative path from test directory
	path := filepath.Join("../testdata", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		// Try from project root
		path = filepath.Join("test/testdata", filename)
		data, err = os.ReadFile(path)
		if err != nil {
			t.Fatalf("Failed to load test data %s: %v", filename, err)
		}
	}

	return data
}

// SaveFixture saves data to a fixture file
func SaveFixture(t *testing.T, filename string, v interface{}) {
	t.Helper()

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal fixture: %v", err)
	}

	path := filepath.Join("test/fixtures", filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("Failed to save fixture %s: %v", filename, err)
	}
}

// CreateTempFile creates a temporary file for testing
func CreateTempFile(t *testing.T, content []byte) string {
	t.Helper()

	tmpfile, err := os.CreateTemp("", "test-*.tmp")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tmpfile.Write(content); err != nil {
		tmpfile.Close()
		os.Remove(tmpfile.Name())
		t.Fatalf("Failed to write temp file: %v", err)
	}

	if err := tmpfile.Close(); err != nil {
		os.Remove(tmpfile.Name())
		t.Fatalf("Failed to close temp file: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		os.Remove(tmpfile.Name())
	})

	return tmpfile.Name()
}

// CreateTempDir creates a temporary directory for testing
func CreateTempDir(t *testing.T) string {
	t.Helper()

	tmpdir, err := os.MkdirTemp("", "test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		os.RemoveAll(tmpdir)
	})

	return tmpdir
}

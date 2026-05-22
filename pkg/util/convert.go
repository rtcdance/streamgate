package util

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
)

// ToJSON converts value to JSON string
func ToJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FromJSON parses JSON string to value
func FromJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

// GzipCompress compresses data using gzip
func GzipCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)

	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// GzipDecompress decompresses gzip data
const maxDecompressSize = 64 * 1024 * 1024

func GzipDecompress(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	return io.ReadAll(io.LimitReader(reader, maxDecompressSize))
}

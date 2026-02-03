package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:  "simple struct",
			input: struct{ Name string }{Name: "test"},
			want:  `{"Name":"test"}`,
		},
		{
			name:  "map",
			input: map[string]interface{}{"key": "value"},
			want:  `{"key":"value"}`,
		},
		{
			name:  "slice",
			input: []int{1, 2, 3},
			want:  `[1,2,3]`,
		},
		{
			name:  "nil",
			input: nil,
			want:  `null`,
		},
		{
			name:  "string",
			input: "test",
			want:  `"test"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToJSON(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, got)
		})
	}
}

func TestFromJSON(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name    string
		input   string
		target  interface{}
		want    interface{}
		wantErr bool
	}{
		{
			name:   "valid json to struct",
			input:  `{"name":"test","value":42}`,
			target: &TestStruct{},
			want:   &TestStruct{Name: "test", Value: 42},
		},
		{
			name:   "valid json to map",
			input:  `{"key":"value"}`,
			target: &map[string]interface{}{},
			want:   &map[string]interface{}{"key": "value"},
		},
		{
			name:    "null",
			input:   `null`,
			target:  &map[string]interface{}{},
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			target:  &TestStruct{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := FromJSON(tt.input, tt.target)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.name == "null" {
				targetMap, ok := tt.target.(*map[string]interface{})
				require.True(t, ok)
				assert.Nil(t, *targetMap)
			} else {
				assert.Equal(t, tt.want, tt.target)
			}
		})
	}
}

func TestGzipCompress(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr bool
	}{
		{
			name:  "empty data",
			input: []byte{},
		},
		{
			name:  "simple string",
			input: []byte("hello world"),
		},
		{
			name:  "large data",
			input: []byte(string(make([]byte, 1000))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GzipCompress(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotEmpty(t, got)
		})
	}
}

func TestGzipDecompress(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:  "compressed data",
			input: func() []byte {
				data, _ := GzipCompress([]byte("hello world"))
				return data
			}(),
			want: []byte("hello world"),
		},
		{
			name:    "invalid gzip data",
			input:   []byte("not gzip"),
			wantErr: true,
		},
		{
			name:  "empty compressed data",
			input: func() []byte {
				data, _ := GzipCompress([]byte{})
				return data
			}(),
			want: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GzipDecompress(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGzipCompressDecompressRoundTrip(t *testing.T) {
	original := []byte("test data for compression and decompression")
	
	compressed, err := GzipCompress(original)
	require.NoError(t, err)
	
	decompressed, err := GzipDecompress(compressed)
	require.NoError(t, err)
	
	assert.Equal(t, original, decompressed)
}

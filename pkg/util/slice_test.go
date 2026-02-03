package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSliceContains(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		element  string
		expected bool
	}{
		{
			name:     "element exists",
			slice:    []string{"a", "b", "c"},
			element:  "b",
			expected: true,
		},
		{
			name:     "element does not exist",
			slice:    []string{"a", "b", "c"},
			element:  "d",
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			element:  "a",
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			element:  "a",
			expected: false,
		},
		{
			name:     "duplicate elements",
			slice:    []string{"a", "a", "b"},
			element:  "a",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceContains(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceIndex(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		element  string
		expected int
	}{
		{
			name:     "element exists at start",
			slice:    []string{"a", "b", "c"},
			element:  "a",
			expected: 0,
		},
		{
			name:     "element exists in middle",
			slice:    []string{"a", "b", "c"},
			element:  "b",
			expected: 1,
		},
		{
			name:     "element exists at end",
			slice:    []string{"a", "b", "c"},
			element:  "c",
			expected: 2,
		},
		{
			name:     "element does not exist",
			slice:    []string{"a", "b", "c"},
			element:  "d",
			expected: -1,
		},
		{
			name:     "empty slice",
			slice:    []string{},
			element:  "a",
			expected: -1,
		},
		{
			name:     "nil slice",
			slice:    nil,
			element:  "a",
			expected: -1,
		},
		{
			name:     "duplicate elements returns first index",
			slice:    []string{"a", "a", "b"},
			element:  "a",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceIndex(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceRemove(t *testing.T) {
	tests := []struct {
		name     string
		slice    []string
		element  string
		expected []string
	}{
		{
			name:     "remove from middle",
			slice:    []string{"a", "b", "c"},
			element:  "b",
			expected: []string{"a", "c"},
		},
		{
			name:     "remove from start",
			slice:    []string{"a", "b", "c"},
			element:  "a",
			expected: []string{"b", "c"},
		},
		{
			name:     "remove from end",
			slice:    []string{"a", "b", "c"},
			element:  "c",
			expected: []string{"a", "b"},
		},
		{
			name:     "remove non-existing element",
			slice:    []string{"a", "b", "c"},
			element:  "d",
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "remove from empty slice",
			slice:    []string{},
			element:  "a",
			expected: []string{},
		},
		{
			name:     "remove from nil slice",
			slice:    nil,
			element:  "a",
			expected: []string{},
		},
		{
			name:     "remove duplicate elements",
			slice:    []string{"a", "a", "b", "a"},
			element:  "a",
			expected: []string{"b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceRemove(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceContainsInt(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		element  int
		expected bool
	}{
		{
			name:     "element exists",
			slice:    []int{1, 2, 3},
			element:  2,
			expected: true,
		},
		{
			name:     "element does not exist",
			slice:    []int{1, 2, 3},
			element:  4,
			expected: false,
		},
		{
			name:     "empty slice",
			slice:    []int{},
			element:  1,
			expected: false,
		},
		{
			name:     "nil slice",
			slice:    nil,
			element:  1,
			expected: false,
		},
		{
			name:     "duplicate elements",
			slice:    []int{1, 1, 2},
			element:  1,
			expected: true,
		},
		{
			name:     "zero value",
			slice:    []int{0, 1, 2},
			element:  0,
			expected: true,
		},
		{
			name:     "negative values",
			slice:    []int{-1, 0, 1},
			element:  -1,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceContainsInt(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceIndexInt(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		element  int
		expected int
	}{
		{
			name:     "element exists at start",
			slice:    []int{1, 2, 3},
			element:  1,
			expected: 0,
		},
		{
			name:     "element exists in middle",
			slice:    []int{1, 2, 3},
			element:  2,
			expected: 1,
		},
		{
			name:     "element exists at end",
			slice:    []int{1, 2, 3},
			element:  3,
			expected: 2,
		},
		{
			name:     "element does not exist",
			slice:    []int{1, 2, 3},
			element:  4,
			expected: -1,
		},
		{
			name:     "empty slice",
			slice:    []int{},
			element:  1,
			expected: -1,
		},
		{
			name:     "nil slice",
			slice:    nil,
			element:  1,
			expected: -1,
		},
		{
			name:     "duplicate elements returns first index",
			slice:    []int{1, 1, 2},
			element:  1,
			expected: 0,
		},
		{
			name:     "zero value",
			slice:    []int{0, 1, 2},
			element:  0,
			expected: 0,
		},
		{
			name:     "negative values",
			slice:    []int{-1, 0, 1},
			element:  -1,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceIndexInt(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceRemoveInt(t *testing.T) {
	tests := []struct {
		name     string
		slice    []int
		element  int
		expected []int
	}{
		{
			name:     "remove from middle",
			slice:    []int{1, 2, 3},
			element:  2,
			expected: []int{1, 3},
		},
		{
			name:     "remove from start",
			slice:    []int{1, 2, 3},
			element:  1,
			expected: []int{2, 3},
		},
		{
			name:     "remove from end",
			slice:    []int{1, 2, 3},
			element:  3,
			expected: []int{1, 2},
		},
		{
			name:     "remove non-existing element",
			slice:    []int{1, 2, 3},
			element:  4,
			expected: []int{1, 2, 3},
		},
		{
			name:     "remove from empty slice",
			slice:    []int{},
			element:  1,
			expected: []int{},
		},
		{
			name:     "remove from nil slice",
			slice:    nil,
			element:  1,
			expected: []int{},
		},
		{
			name:     "remove duplicate elements",
			slice:    []int{1, 1, 2, 1},
			element:  1,
			expected: []int{2},
		},
		{
			name:     "remove zero value",
			slice:    []int{0, 1, 2},
			element:  0,
			expected: []int{1, 2},
		},
		{
			name:     "remove negative values",
			slice:    []int{-1, 0, 1},
			element:  -1,
			expected: []int{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SliceRemoveInt(tt.slice, tt.element)
			assert.Equal(t, tt.expected, result)
		})
	}
}

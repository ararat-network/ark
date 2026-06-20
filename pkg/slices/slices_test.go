package slices_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"noah/pkg/slices"
)

func TestChunk(t *testing.T) {
	tests := []struct {
		name      string
		input     []int
		chunkSize int
		want      [][]int
	}{
		{
			name:      "empty input",
			input:     []int{},
			chunkSize: 2,
			want:      [][]int{{}},
		},
		{
			name:      "input smaller than chunk size",
			input:     []int{1, 2},
			chunkSize: 3,
			want:      [][]int{{1, 2}},
		},
		{
			name:      "input equals chunk size",
			input:     []int{1, 2},
			chunkSize: 2,
			want:      [][]int{{1, 2}},
		},
		{
			name:      "final chunk contains remainder",
			input:     []int{1, 2, 3, 4, 5},
			chunkSize: 2,
			want:      [][]int{{1, 2}, {3, 4}, {5}},
		},
		{
			name:      "chunk size one",
			input:     []int{1, 2, 3},
			chunkSize: 1,
			want:      [][]int{{1}, {2}, {3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, slices.Chunk(tt.input, tt.chunkSize))
		})
	}
}

func TestChunkPanicsForInvalidSize(t *testing.T) {
	tests := []struct {
		name      string
		chunkSize int
	}{
		{
			name:      "zero",
			chunkSize: 0,
		},
		{
			name:      "negative",
			chunkSize: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.PanicsWithValue(t, "chunk size must be positive", func() {
				slices.Chunk([]int{1, 2}, tt.chunkSize)
			})
		})
	}
}

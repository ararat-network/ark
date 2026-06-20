package slices

// Chunk chunks a slice into batches of chunkSize.
// example: {1,2,3,4,5}, chunkSize = 2 -> {1,2}, {3,4}, {5}
func Chunk[T any](input []T, chunkSize int) [][]T {
	if chunkSize <= 0 {
		panic("chunk size must be positive")
	}
	if len(input) <= chunkSize {
		return [][]T{input}
	}
	var chunks [][]T
	for i := 0; i < len(input); i += chunkSize {
		end := min(i+chunkSize, len(input))
		chunks = append(chunks, input[i:end])
	}
	return chunks
}

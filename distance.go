package magpie

import (
	"math"
)

// GetDistanceFunc returns the distance function for the specified metric.
func GetDistanceFunc(metric string) DistanceFunc {
	switch metric {
	case "cosine":
		return CosineSimilarity
	case "euclidean":
		return EuclideanDistance
	case "dot":
		return DotProduct
	default:
		return CosineSimilarity
	}
}

// CosineSimilarity calculates the cosine similarity between two vectors.
// Returns a distance value where 0 = identical, 2 = opposite.
// Cosine similarity is converted to distance as: distance = 1 - similarity
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 2.0 // Maximum distance
	}

	if len(a) == 0 {
		return 0
	}

	var dot, normA, normB float32

	// Calculate dot product and norms in a single pass
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	// Handle zero vectors
	if normA == 0 || normB == 0 {
		return 2.0
	}

	// Cosine similarity = dot / (||a|| * ||b||)
	similarity := dot / (float32(math.Sqrt(float64(normA))) * float32(math.Sqrt(float64(normB))))

	// Clamp to [-1, 1] to handle floating point errors
	if similarity > 1.0 {
		similarity = 1.0
	} else if similarity < -1.0 {
		similarity = -1.0
	}

	// Convert similarity to distance: 0 = identical, 2 = opposite
	return 1.0 - similarity
}

// EuclideanDistance calculates the Euclidean (L2) distance between two vectors.
// Returns the straight-line distance in vector space.
func EuclideanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.Inf(1))
	}

	if len(a) == 0 {
		return 0
	}

	var sum float32
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		sum += diff * diff
	}

	return float32(math.Sqrt(float64(sum)))
}

// DotProduct calculates the negative dot product (for use as a distance).
// More positive dot products = more similar, so we negate for distance.
func DotProduct(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.Inf(1))
	}

	if len(a) == 0 {
		return 0
	}

	var dot float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
	}

	// Negate so that higher similarity = lower distance
	return -dot
}

// ManhattanDistance calculates the Manhattan (L1) distance.
// This is the sum of absolute differences.
func ManhattanDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.Inf(1))
	}

	if len(a) == 0 {
		return 0
	}

	var sum float32
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		if diff < 0 {
			sum -= diff
		} else {
			sum += diff
		}
	}

	return sum
}

// ChebyshevDistance calculates the Chebyshev (L-infinity) distance.
// This is the maximum absolute difference across all dimensions.
func ChebyshevDistance(a, b []float32) float32 {
	if len(a) != len(b) {
		return float32(math.Inf(1))
	}

	if len(a) == 0 {
		return 0
	}

	var maxDiff float32
	for i := 0; i < len(a); i++ {
		diff := a[i] - b[i]
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
	}

	return maxDiff
}

// Normalize normalizes a vector to unit length (L2 norm = 1).
// This is useful for cosine similarity calculations.
func Normalize(vector []float32) []float32 {
	var norm float32
	for _, v := range vector {
		norm += v * v
	}

	if norm == 0 {
		return vector
	}

	norm = float32(math.Sqrt(float64(norm)))
	normalized := make([]float32, len(vector))
	for i, v := range vector {
		normalized[i] = v / norm
	}

	return normalized
}

// VectorMagnitude calculates the L2 norm (magnitude) of a vector.
func VectorMagnitude(vector []float32) float32 {
	var sum float32
	for _, v := range vector {
		sum += v * v
	}
	return float32(math.Sqrt(float64(sum)))
}

// DotProductRaw calculates the raw dot product (not negated).
func DotProductRaw(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}

	var dot float32
	for i := 0; i < len(a); i++ {
		dot += a[i] * b[i]
	}

	return dot
}

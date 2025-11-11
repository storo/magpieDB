package magpie

import (
	"fmt"
	"math"
	"testing"
)

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 0.0,
			epsilon:  0.001,
		},
		{
			name:     "orthogonal vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 1.0,
			epsilon:  0.001,
		},
		{
			name:     "opposite vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{-1, 0, 0},
			expected: 2.0,
			epsilon:  0.001,
		},
		{
			name:     "similar vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: 0.0,
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CosineSimilarity(tt.a, tt.b)
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("CosineSimilarity(%v, %v) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestEuclideanDistance(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "identical vectors",
			a:        []float32{0, 0, 0},
			b:        []float32{0, 0, 0},
			expected: 0.0,
			epsilon:  0.001,
		},
		{
			name:     "unit distance",
			a:        []float32{0, 0, 0},
			b:        []float32{1, 0, 0},
			expected: 1.0,
			epsilon:  0.001,
		},
		{
			name:     "3-4-5 triangle",
			a:        []float32{0, 0, 0},
			b:        []float32{3, 4, 0},
			expected: 5.0,
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EuclideanDistance(tt.a, tt.b)
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("EuclideanDistance(%v, %v) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestDotProduct(t *testing.T) {
	tests := []struct {
		name     string
		a        []float32
		b        []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "perpendicular vectors",
			a:        []float32{1, 0, 0},
			b:        []float32{0, 1, 0},
			expected: 0.0,
			epsilon:  0.001,
		},
		{
			name:     "aligned vectors",
			a:        []float32{1, 2, 3},
			b:        []float32{1, 2, 3},
			expected: -14.0, // -(1 + 4 + 9) = -14
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DotProduct(tt.a, tt.b)
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("DotProduct(%v, %v) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
	}{
		{
			name: "3D vector",
			vec:  []float32{3, 4, 0},
		},
		{
			name: "already normalized",
			vec:  []float32{1, 0, 0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized := Normalize(tt.vec)
			magnitude := VectorMagnitude(normalized)

			if math.Abs(float64(magnitude-1.0)) > 0.001 {
				t.Errorf("Normalized vector magnitude = %f, want 1.0", magnitude)
			}
		})
	}
}

func TestVectorMagnitude(t *testing.T) {
	tests := []struct {
		name     string
		vec      []float32
		expected float32
		epsilon  float32
	}{
		{
			name:     "unit vector",
			vec:      []float32{1, 0, 0},
			expected: 1.0,
			epsilon:  0.001,
		},
		{
			name:     "3-4-5 triangle",
			vec:      []float32{3, 4, 0},
			expected: 5.0,
			epsilon:  0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VectorMagnitude(tt.vec)
			if math.Abs(float64(result-tt.expected)) > float64(tt.epsilon) {
				t.Errorf("VectorMagnitude(%v) = %f, want %f", tt.vec, result, tt.expected)
			}
		})
	}
}

// Benchmark tests
func BenchmarkCosineSimilarity(b *testing.B) {
	dims := []int{128, 384, 768, 1536}

	for _, dim := range dims {
		vecA := make([]float32, dim)
		vecB := make([]float32, dim)
		for i := range vecA {
			vecA[i] = float32(i) * 0.01
			vecB[i] = float32(i) * 0.01
		}

		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				CosineSimilarity(vecA, vecB)
			}
		})
	}
}

func BenchmarkEuclideanDistance(b *testing.B) {
	dims := []int{128, 384, 768, 1536}

	for _, dim := range dims {
		vecA := make([]float32, dim)
		vecB := make([]float32, dim)
		for i := range vecA {
			vecA[i] = float32(i) * 0.01
			vecB[i] = float32(i) * 0.01
		}

		b.Run(fmt.Sprintf("dim=%d", dim), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				EuclideanDistance(vecA, vecB)
			}
		})
	}
}

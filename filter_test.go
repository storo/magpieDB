package magpie

import (
	"testing"
)

func TestEqualsFilter(t *testing.T) {
	filter := Eq("category", "tutorial")

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "matches",
			metadata: map[string]interface{}{"category": "tutorial"},
			expected: true,
		},
		{
			name:     "does not match",
			metadata: map[string]interface{}{"category": "guide"},
			expected: false,
		},
		{
			name:     "key missing",
			metadata: map[string]interface{}{"title": "test"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestRangeFilter(t *testing.T) {
	filter := Between("rating", 3.0, 5.0)

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "in range (float64)",
			metadata: map[string]interface{}{"rating": 4.0},
			expected: true,
		},
		{
			name:     "in range (int)",
			metadata: map[string]interface{}{"rating": 4},
			expected: true,
		},
		{
			name:     "below range",
			metadata: map[string]interface{}{"rating": 2.0},
			expected: false,
		},
		{
			name:     "above range",
			metadata: map[string]interface{}{"rating": 6.0},
			expected: false,
		},
		{
			name:     "at min",
			metadata: map[string]interface{}{"rating": 3.0},
			expected: true,
		},
		{
			name:     "at max",
			metadata: map[string]interface{}{"rating": 5.0},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestInFilter(t *testing.T) {
	filter := In("status", "active", "pending", "review")

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "in list",
			metadata: map[string]interface{}{"status": "active"},
			expected: true,
		},
		{
			name:     "not in list",
			metadata: map[string]interface{}{"status": "archived"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestExistsFilter(t *testing.T) {
	filter := Exists("title")

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "exists",
			metadata: map[string]interface{}{"title": "test"},
			expected: true,
		},
		{
			name:     "does not exist",
			metadata: map[string]interface{}{"category": "test"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestContainsFilter(t *testing.T) {
	filter := Contains("title", "intro")

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "contains",
			metadata: map[string]interface{}{"title": "introduction to magpie"},
			expected: true,
		},
		{
			name:     "does not contain",
			metadata: map[string]interface{}{"title": "advanced tutorial"},
			expected: false,
		},
		{
			name:     "not a string",
			metadata: map[string]interface{}{"title": 123},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAndFilter(t *testing.T) {
	filter := And(
		Eq("category", "tutorial"),
		Between("rating", 4.0, 5.0),
	)

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name: "all match",
			metadata: map[string]interface{}{
				"category": "tutorial",
				"rating":   4.5,
			},
			expected: true,
		},
		{
			name: "one does not match",
			metadata: map[string]interface{}{
				"category": "tutorial",
				"rating":   3.0,
			},
			expected: false,
		},
		{
			name: "none match",
			metadata: map[string]interface{}{
				"category": "guide",
				"rating":   3.0,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestOrFilter(t *testing.T) {
	filter := Or(
		Eq("category", "tutorial"),
		Eq("category", "guide"),
	)

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "first matches",
			metadata: map[string]interface{}{"category": "tutorial"},
			expected: true,
		},
		{
			name:     "second matches",
			metadata: map[string]interface{}{"category": "guide"},
			expected: true,
		},
		{
			name:     "none match",
			metadata: map[string]interface{}{"category": "reference"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNotFilter(t *testing.T) {
	filter := Not(Eq("category", "tutorial"))

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name:     "negated true",
			metadata: map[string]interface{}{"category": "tutorial"},
			expected: false,
		},
		{
			name:     "negated false",
			metadata: map[string]interface{}{"category": "guide"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestComplexFilter(t *testing.T) {
	// (category = "tutorial" AND rating >= 4) OR (featured = true)
	filter := Or(
		And(
			Eq("category", "tutorial"),
			Between("rating", 4.0, 5.0),
		),
		Eq("featured", true),
	)

	tests := []struct {
		name     string
		metadata map[string]interface{}
		expected bool
	}{
		{
			name: "matches first condition",
			metadata: map[string]interface{}{
				"category": "tutorial",
				"rating":   4.5,
				"featured": false,
			},
			expected: true,
		},
		{
			name: "matches second condition",
			metadata: map[string]interface{}{
				"category": "guide",
				"rating":   3.0,
				"featured": true,
			},
			expected: true,
		},
		{
			name: "matches both conditions",
			metadata: map[string]interface{}{
				"category": "tutorial",
				"rating":   4.5,
				"featured": true,
			},
			expected: true,
		},
		{
			name: "matches neither",
			metadata: map[string]interface{}{
				"category": "reference",
				"rating":   3.0,
				"featured": false,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filter.Match(tt.metadata)
			if result != tt.expected {
				t.Errorf("Match() = %v, want %v", result, tt.expected)
			}
		})
	}
}

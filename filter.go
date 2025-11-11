package magpie

import (
	"fmt"
	"regexp"
	"strings"
)

// Filter implementations for metadata queries.

// EqualsFilter matches if metadata[key] equals value.
type EqualsFilter struct {
	Key   string
	Value interface{}
}

func (f *EqualsFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}
	return v == f.Value
}

// NotEqualsFilter matches if metadata[key] does not equal value.
type NotEqualsFilter struct {
	Key   string
	Value interface{}
}

func (f *NotEqualsFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return true
	}
	return v != f.Value
}

// InFilter matches if metadata[key] is in the list of values.
type InFilter struct {
	Key    string
	Values []interface{}
}

func (f *InFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	for _, val := range f.Values {
		if v == val {
			return true
		}
	}

	return false
}

// RangeFilter matches if metadata[key] is within [Min, Max].
type RangeFilter struct {
	Key string
	Min float64
	Max float64
}

func (f *RangeFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	// Try to convert to float64
	var num float64
	switch val := v.(type) {
	case float64:
		num = val
	case float32:
		num = float64(val)
	case int:
		num = float64(val)
	case int64:
		num = float64(val)
	default:
		return false
	}

	return num >= f.Min && num <= f.Max
}

// ExistsFilter matches if the key exists in metadata.
type ExistsFilter struct {
	Key string
}

func (f *ExistsFilter) Match(metadata map[string]interface{}) bool {
	_, ok := metadata[f.Key]
	return ok
}

// NotExistsFilter matches if the key does not exist in metadata.
type NotExistsFilter struct {
	Key string
}

func (f *NotExistsFilter) Match(metadata map[string]interface{}) bool {
	_, ok := metadata[f.Key]
	return !ok
}

// ContainsFilter matches if metadata[key] (string) contains substring.
type ContainsFilter struct {
	Key       string
	Substring string
}

func (f *ContainsFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	str, ok := v.(string)
	if !ok {
		return false
	}

	return strings.Contains(str, f.Substring)
}

// StartsWithFilter matches if metadata[key] (string) starts with prefix.
type StartsWithFilter struct {
	Key    string
	Prefix string
}

func (f *StartsWithFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	str, ok := v.(string)
	if !ok {
		return false
	}

	return strings.HasPrefix(str, f.Prefix)
}

// EndsWithFilter matches if metadata[key] (string) ends with suffix.
type EndsWithFilter struct {
	Key    string
	Suffix string
}

func (f *EndsWithFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	str, ok := v.(string)
	if !ok {
		return false
	}

	return strings.HasSuffix(str, f.Suffix)
}

// RegexFilter matches if metadata[key] (string) matches regex pattern.
type RegexFilter struct {
	Key     string
	Pattern *regexp.Regexp
}

func NewRegexFilter(key, pattern string) (*RegexFilter, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return &RegexFilter{
		Key:     key,
		Pattern: re,
	}, nil
}

func (f *RegexFilter) Match(metadata map[string]interface{}) bool {
	v, ok := metadata[f.Key]
	if !ok {
		return false
	}

	str, ok := v.(string)
	if !ok {
		return false
	}

	return f.Pattern.MatchString(str)
}

// NotFilter inverts another filter.
type NotFilter struct {
	Filter Filter
}

func (f *NotFilter) Match(metadata map[string]interface{}) bool {
	return !f.Filter.Match(metadata)
}

// AllFilter matches only if all filters match (AND).
type AllFilter struct {
	Filters []Filter
}

func (f *AllFilter) Match(metadata map[string]interface{}) bool {
	for _, filter := range f.Filters {
		if !filter.Match(metadata) {
			return false
		}
	}
	return true
}

// AnyFilter matches if any filter matches (OR).
type AnyFilter struct {
	Filters []Filter
}

func (f *AnyFilter) Match(metadata map[string]interface{}) bool {
	for _, filter := range f.Filters {
		if filter.Match(metadata) {
			return true
		}
	}
	return false
}

// Helper functions to create filters

// Eq creates an equality filter.
func Eq(key string, value interface{}) Filter {
	return &EqualsFilter{Key: key, Value: value}
}

// Ne creates a not-equals filter.
func Ne(key string, value interface{}) Filter {
	return &NotEqualsFilter{Key: key, Value: value}
}

// In creates an in-list filter.
func In(key string, values ...interface{}) Filter {
	return &InFilter{Key: key, Values: values}
}

// Between creates a range filter.
func Between(key string, min, max float64) Filter {
	return &RangeFilter{Key: key, Min: min, Max: max}
}

// Exists creates an exists filter.
func Exists(key string) Filter {
	return &ExistsFilter{Key: key}
}

// NotExists creates a not-exists filter.
func NotExists(key string) Filter {
	return &NotExistsFilter{Key: key}
}

// Contains creates a contains filter.
func Contains(key, substring string) Filter {
	return &ContainsFilter{Key: key, Substring: substring}
}

// StartsWith creates a starts-with filter.
func StartsWith(key, prefix string) Filter {
	return &StartsWithFilter{Key: key, Prefix: prefix}
}

// EndsWith creates an ends-with filter.
func EndsWith(key, suffix string) Filter {
	return &EndsWithFilter{Key: key, Suffix: suffix}
}

// Regex creates a regex filter.
func Regex(key, pattern string) (Filter, error) {
	return NewRegexFilter(key, pattern)
}

// Not creates a NOT filter.
func Not(filter Filter) Filter {
	return &NotFilter{Filter: filter}
}

// And creates an AND filter from multiple filters.
func And(filters ...Filter) Filter {
	return &AllFilter{Filters: filters}
}

// Or creates an OR filter from multiple filters.
func Or(filters ...Filter) Filter {
	return &AnyFilter{Filters: filters}
}

// Example usage:
//
//	// Simple filter
//	filter := magpie.Eq("category", "tutorial")
//
//	// Complex filter
//	filter := magpie.And(
//	    magpie.Eq("category", "tutorial"),
//	    magpie.Between("rating", 4.0, 5.0),
//	    magpie.Or(
//	        magpie.Contains("title", "intro"),
//	        magpie.Contains("title", "guide"),
//	    ),
//	)
//
//	results := nest.FindWithFilter(query, 10, filter)

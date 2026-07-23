package core

import (
	"encoding/json"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestBoundedMillisecondsLimitsBeforeDurationConversion(t *testing.T) {
	for _, test := range []struct {
		name                     string
		value, fallback, maximum int
		want                     time.Duration
	}{
		{name: "negative uses fallback", value: -1, fallback: 15000, maximum: 120000, want: 15 * time.Second},
		{name: "custom", value: 2500, fallback: 15000, maximum: 120000, want: 2500 * time.Millisecond},
		{name: "capped", value: math.MaxInt, fallback: 15000, maximum: 120000, want: 2 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := BoundedMilliseconds(test.value, test.fallback, test.maximum); got != test.want {
				t.Fatalf("BoundedMilliseconds() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestIntArgAcceptsOnlyIntegralInRangeValues(t *testing.T) {
	const fallback = 77
	tests := []struct {
		name  string
		value any
		want  int
	}{
		{name: "int", value: 12, want: 12},
		{name: "float integer", value: float64(12), want: 12},
		{name: "json number", value: json.Number("12"), want: 12},
		{name: "trimmed string", value: " 12 ", want: 12},
		{name: "fraction", value: 12.5, want: fallback},
		{name: "nan", value: math.NaN(), want: fallback},
		{name: "positive infinity", value: math.Inf(1), want: fallback},
		{name: "negative infinity", value: math.Inf(-1), want: fallback},
		{name: "huge float", value: math.MaxFloat64, want: fallback},
		{name: "fractional json number", value: json.Number("12.5"), want: fallback},
		{name: "invalid string", value: "twelve", want: fallback},
	}
	if strconv.IntSize == 64 {
		tests = append(tests, struct {
			name  string
			value any
			want  int
		}{name: "overflowing float64", value: float64(1 << 63), want: fallback})
	} else {
		tests = append(tests, struct {
			name  string
			value any
			want  int
		}{name: "overflowing float64", value: float64(1 << 31), want: fallback})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IntArg(map[string]any{"value": test.value}, "value", fallback); got != test.want {
				t.Fatalf("IntArg(%T(%v)) = %d, want %d", test.value, test.value, got, test.want)
			}
		})
	}
}

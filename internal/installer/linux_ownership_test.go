package installer

import (
	"strconv"
	"testing"
)

func TestParseOwnershipIDBoundsConversionToInt(t *testing.T) {
	maxInt := int64(^uint(0) >> 1)
	for _, test := range []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "zero", value: "0", want: 0},
		{name: "ordinary", value: "1000", want: 1000},
		{name: "max int", value: strconv.FormatInt(maxInt, 10), want: int(maxInt)},
		{name: "negative", value: "-1", wantErr: true},
		{name: "unsigned overflow", value: "18446744073709551615", wantErr: true},
		{name: "invalid", value: "not-an-id", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseOwnershipID(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseOwnershipID(%q) succeeded with %d, want error", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseOwnershipID(%q): %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("parseOwnershipID(%q)=%d, want %d", test.value, got, test.want)
			}
		})
	}
}

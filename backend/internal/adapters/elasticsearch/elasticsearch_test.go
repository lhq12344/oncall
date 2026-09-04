package elasticsearch

import (
	"testing"
	"time"
)

func TestParseLogTimeRangeAcceptsOperationalDayAndWeekUnits(t *testing.T) {
	cases := map[string]time.Duration{
		"5m":  5 * time.Minute,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"2w":  14 * 24 * time.Hour,
	}
	for input, want := range cases {
		got, err := parseLogTimeRange(input)
		if err != nil || got != want {
			t.Fatalf("parseLogTimeRange(%q) = %v, %v; want %v, nil", input, got, err, want)
		}
	}
}

func TestParseLogTimeRangeRejectsInvalidValues(t *testing.T) {
	if _, err := parseLogTimeRange("soon"); err == nil {
		t.Fatal("expected invalid time range to fail")
	}
}

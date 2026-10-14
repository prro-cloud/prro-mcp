package tools

import (
	"testing"
	"time"

	"github.com/prro-cloud/prro-go"
)

func TestUAH(t *testing.T) {
	for in, want := range map[prro.Kopecks]string{
		"150.0000":    "1.50 UAH",
		"15000":       "150.00 UAH",
		"5":           "0.05 UAH",
		"0.0000":      "0.00 UAH",
		"1234.5":      "12.345 UAH",
		"-500.0000":   "-5.00 UAH",
		"100000.0001": "1000.000001 UAH",
		"abc":         "abc kop",
		"":            " kop",
	} {
		if got := uah(in); got != want {
			t.Errorf("uah(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTimePart(t *testing.T) {
	if got := timePart("since", time.Time{}); got != "" {
		t.Errorf("zero = %q, want nothing", got)
	}
	ts := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	if got := timePart("since", ts); got != " since 2026-10-14T09:00:00Z" {
		t.Errorf("got %q", got)
	}
}

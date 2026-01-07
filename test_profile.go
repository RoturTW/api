package main

import "testing"

func TestNormalizeUserTimeLayout_TimeOnly(t *testing.T) {
	cases := map[string]string{
		"h:m":       "15:04",
		"h:m:s":     "15:04:05",
		"h:m:s a":   "15:04:05 PM",
		"H:M":       "15:04",       // case-insensitive
		"h:m:s   a": "15:04:05 PM", // extra spacing
	}

	for in, want := range cases {
		if got := normalizeUserTimeLayout(in); got != want {
			t.Fatalf("normalizeUserTimeLayout(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeUserTimeLayout_DateOnly(t *testing.T) {
	cases := map[string]string{
		"y/m/d": "2006/01/02",
		"y-m-d": "2006-01-02",
		"d/m/y": "02/01/2006",
		"d.m.y": "02.01.2006",
	}

	for in, want := range cases {
		if got := normalizeUserTimeLayout(in); got != want {
			t.Fatalf("normalizeUserTimeLayout(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeUserTimeLayout_DateTime(t *testing.T) {
	cases := map[string]string{
		"y/m/d h:m":     "2006/01/02 15:04",
		"y-m-d h:m:s":   "2006-01-02 15:04:05",
		"d/m/y h:m:s a": "02/01/2006 15:04:05 PM",
	}

	for in, want := range cases {
		if got := normalizeUserTimeLayout(in); got != want {
			t.Fatalf("normalizeUserTimeLayout(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeUserTimeLayout_Passthrough(t *testing.T) {
	// Already a Go layout -> unchanged
	in := "2006-01-02 15:04"
	if got := normalizeUserTimeLayout(in); got != in {
		t.Fatalf("normalizeUserTimeLayout(%q) = %q, want %q", in, got, in)
	}
}

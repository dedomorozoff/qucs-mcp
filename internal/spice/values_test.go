package spice

import "testing"

// values_test.go covers the normalization contract from the plan:
// SPICE 1M = milli, 1MEG = mega; Qucs-S writes 1m / 1M. Only bare
// literals are normalized; {...} and .param values are untouched.
func TestSPICEMilliVsMega(t *testing.T) {
	if got := NormalizeValue("1M"); got != "1m" {
		t.Fatalf("1M = %q, want 1m", got)
	}
	if got := NormalizeValue("1MEG"); got != "1M" {
		t.Fatalf("1MEG = %q, want 1M", got)
	}
	if got := NormalizeValue("100Meg"); got != "100M" {
		t.Fatalf("100Meg = %q, want 100M", got)
	}
}

// Suffix-digit form ("3k3" = 3.3k): qucs-s misreads "3k3" as 3k, so the
// normalization must expand it (regression: update_schematic with
// "R1 in out 3k3" silently produced a 3k resistor).
func TestSuffixDigits(t *testing.T) {
	cases := [][2]string{
		{"3k3", "3.3k"},
		{"1k33", "1.33k"},
		{"4R7", "4.7"}, // PSpice style: R = decimal point (ohms unchanged)
		{"10k02", "10.02k"},
		{"1M5", "1.5m"}, // SPICE M = milli: 1M5 = 1.5 milli
	}
	for _, c := range cases {
		if got := NormalizeValue(c[0]); got != c[1] {
			t.Fatalf("NormalizeValue(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	// unit junk after the suffix digits is dropped, like "10uF" → "10u"
	if got := NormalizeValue("10u2F"); got != "10.2u" {
		t.Fatalf("10u2F rewritten to %q, want 10.2u", got)
	}
}

func TestExpressionsUntouched(t *testing.T) {
	for _, s := range []string{"{R1*2}", "V(N001)*I(L1)", "xx", "1e-6 * 2"} {
		if got := NormalizeValue(s); got != s {
			t.Fatalf("%q was rewritten to %q", s, got)
		}
	}
}

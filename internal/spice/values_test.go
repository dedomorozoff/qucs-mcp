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

func TestExpressionsUntouched(t *testing.T) {
	for _, s := range []string{"{R1*2}", "V(N001)*I(L1)", "xx", "1e-6 * 2"} {
		if got := NormalizeValue(s); got != s {
			t.Fatalf("%q was rewritten to %q", s, got)
		}
	}
}

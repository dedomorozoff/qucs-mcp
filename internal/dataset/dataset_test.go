package dataset

import (
	"strings"
	"testing"
)

const goldenSmall = `<Qucs Dataset 26.1.1>
<indep frequency 3>
1.000000000000e+04
2.000000000000e+04
4.000000000000e+04
</indep>
<dep ac.v(out) frequency>
9.999999803401e-01+j-3.989443027060e-04
9.999999215270e-01+j-7.978117416225e-04
9.999996858918e-01+j-1.595460116472e-03
</dep>
<dep ac.v(in) frequency>
1.000000000000e+00+j0.000000000000e+00
1.000000000000e+00+j0.000000000000e+00
1.000000000000e+00+j0.000000000000e+00
</dep>
`

func TestParseGolden(t *testing.T) {
	d, err := Parse(goldenSmall)
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != "26.1.1" {
		t.Fatalf("version = %q", d.Version)
	}
	if len(d.Vars) != 3 {
		t.Fatalf("vars = %d, want 3", len(d.Vars))
	}
	freq, ok := d.Var("frequency")
	if !ok || !freq.Indep {
		t.Fatalf("frequency series wrong: %+v", freq)
	}
	if len(freq.Values) != 3 || freq.Values[2] != 4e4 {
		t.Fatalf("freq values = %v", freq.Values)
	}
	vout, ok := d.Var("ac.v(out)")
	if !ok || vout.Indep {
		t.Fatalf("ac.v(out) wrong: %+v", vout)
	}
	want := []complex128{
		0.9999999803401 - 3.989443027060e-4i,
		0.9999999215270 - 7.978117416225e-4i,
		0.9999996858918 - 1.595460116472e-3i,
	}
	for i, w := range want {
		if d := cmpl(vout.Values[i] - w); d > 1e-12 {
			t.Fatalf("vout[%d] = %v, want %v", i, vout.Values[i], w)
		}
	}
	vin, _ := d.Var("ac.v(in)")
	if vin.Values[0] != 1 {
		t.Fatalf("vin[0] = %v", vin.Values[0])
	}
}

func TestParseErrors(t *testing.T) {
	// bad header
	_, err := Parse("<Not a Dataset>\n")
	if err == nil || !strings.Contains(err.Error(), "Qucs Dataset") {
		t.Fatalf("err = %v", err)
	}
	// value outside any block
	_, err = Parse("<Qucs Dataset 0>\n1.0\n")
	if err == nil || !strings.Contains(err.Error(), "outside a declared block") {
		t.Fatalf("err = %v, want declared-block error", err)
	}
	// dep header missing the indep reference
	_, err = Parse("<Qucs Dataset 0>\n<dep name>\n</dep>\n")
	if err == nil || !strings.Contains(err.Error(), "bad dep header") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseVarMissing(t *testing.T) {
	d, err := Parse(goldenSmall)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Var("ac.i(r1)"); ok {
		t.Fatal("missing variable must not be found")
	}
}

func cmpl(z complex128) float64 {
	r := real(z)
	if r < 0 {
		r = -r
	}
	i := imag(z)
	if i < 0 {
		i = -i
	}
	return r + i
}

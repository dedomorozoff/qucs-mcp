package spice

import (
	"strings"
	"testing"
)

func TestNormalizeValues(t *testing.T) {
	cases := [][2]string{
		{"1M", "1m"},   // SPICE M = milli
		{"1MEG", "1M"}, // SPICE MEG = mega
		{"4.7M", "4.7m"},
		{"2.2k", "2.2k"},
		{"10uF", "10u"}, // junk unit dropped
		{"1kOhm", "1k"},
		{"100n", "100n"},
		{"1e-6", "1e-6"}, // exponent literal untouched
		{"1E3", "1E3"},
		{"42", "42"},
		{"0.5", "0.5"},
		{"1t", "1T"},
		{"2.2meg", "2.2M"},
		{"-3.3", "-3.3"},
		{"{R1*2}", "{R1*2}"}, // expression untouched
		{"V(T)*2", "V(T)*2"}, // expression untouched
		{"gmax(x)*V0", "gmax(x)*V0"},
		{"", ""},
		{"xx", "xx"}, // not a literal (letters only) — untouched
	}
	for _, c := range cases {
		if got := NormalizeValue(c[0]); got != c[1] {
			t.Errorf("NormalizeValue(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestParseRCLowPass(t *testing.T) {
	spec, err := Parse(strings.Join([]string{
		"* RC low-pass",
		"V1 in 0 DC 0 AC 1",
		"R1 in out 1k",
		"C1 out 0 100n",
		".ac dec 91 10 1MEG",
		".end",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Components) != 3 {
		t.Fatalf("components = %d, want 3", len(spec.Components))
	}
	v1, _ := spec.Find("V1")
	if v1.Type != "S4Q_V" {
		t.Fatalf("V1.Type = %q", v1.Type)
	}
	if v1.Value != "DC 0 AC 1" {
		t.Fatalf("V1.Value = %q", v1.Value)
	}
	if v1.NetOf("1") != "in" || v1.NetOf("2") != "0" {
		t.Fatalf("V1 pins = %v", v1.Pins)
	}
	r1, _ := spec.Find("R1")
	if r1.Type != "R" || r1.Value != "1k" {
		t.Fatalf("R1 = %+v", r1)
	}
	if r1.NetOf("1") != "in" || r1.NetOf("2") != "out" {
		t.Fatalf("R1 pins = %v", r1.Pins)
	}
	c1, _ := spec.Find("C1")
	if c1.Value != "100n" || c1.NetOf("1") != "out" || c1.NetOf("2") != "0" {
		t.Fatalf("C1 = %+v", c1)
	}
	if !spec.HasAnalysis("ac") {
		t.Fatal("ac analysis missing")
	}
	ac := spec.Analyses[0]
	if ac.Params["sweep"] != "dec" || ac.Params["points"] != "91" || ac.Params["start"] != "10" || ac.Params["stop"] != "1M" {
		t.Fatalf("ac params = %v", ac.Params)
	}
}

func TestParseModelParamAndErrors(t *testing.T) {
	spec, err := Parse(strings.Join([]string{
		"d1 1 2 DMOD", // lowercase letter is fine; name must start with the type letter
		".model DMOD D(IS=1e-9)",
		".param RLOAD=1k",
		".temp 27",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Models) != 1 || !strings.Contains(spec.Models[0], "DMOD") {
		t.Fatalf("models = %v", spec.Models)
	}
	if spec.Params["RLOAD"] != "1k" {
		t.Fatalf("params = %v (must be untouched)", spec.Params)
	}
	if len(spec.Warnings) != 1 || !strings.Contains(spec.Warnings[0], ".temp") {
		t.Fatalf("warnings = %v", spec.Warnings)
	}
	d1, _ := spec.Find("d1")
	if d1.Value != "DMOD" || d1.NetOf("1") != "1" || d1.NetOf("2") != "2" {
		t.Fatalf("D = %+v", d1)
	}
}

func TestParseSubcircuitIsPhase2(t *testing.T) {
	_, err := Parse("X1 a b subtop\n")
	if err == nil || !strings.Contains(err.Error(), "phase 2") {
		t.Fatalf("err = %v, want phase-2 message", err)
	}
}

func TestParseUnknownDevice(t *testing.T) {
	_, err := Parse("J1 a b c jfet\n")
	if err == nil || !strings.Contains(err.Error(), "R C L V I D Q M") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseBJTAndMOSFETFlavors(t *testing.T) {
	spec, err := Parse(strings.Join([]string{
		"Q1 c b e QMOD",
		"M1 d g s 0 MMOD",
		".model QMOD pnp",
		".model MMOD PMOS",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	q, _ := spec.Find("Q1")
	if q.Type != "_BJT" || q.Props["flavor"] != "pnp" || q.NetOf("B") != "b" {
		t.Fatalf("Q1 = %+v", q)
	}
	m, _ := spec.Find("M1")
	if m.Props["flavor"] != "pmos" || m.NetOf("S") != "s" {
		t.Fatalf("M1 = %+v", m)
	}
}

func TestParseContinuation(t *testing.T) {
	spec, err := Parse(strings.Join([]string{
		"V1 in 0 PWL(0 0 1m 5)",
		"+ 2m 0 3m 5)",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := spec.Find("V1")
	if !strings.Contains(v1.Value, "2m 0") {
		t.Fatalf("V1.Value = %q (continuation lost)", v1.Value)
	}
}

func TestParseDuplicateRef(t *testing.T) {
	_, err := Parse(strings.Join([]string{"R1 a b 1k", "R1 b c 2k"}, "\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseTranOpInclude(t *testing.T) {
	spec, err := Parse(strings.Join([]string{
		".op",
		".tran 1u 100u 10u 5u",
		".include models.inc",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !spec.HasAnalysis("op") || !spec.HasAnalysis("tran") {
		t.Fatalf("analyses = %v", spec.Analyses)
	}
	tr := spec.Analyses[1]
	if tr.Params["tstep"] != "1u" || tr.Params["tstop"] != "100u" || tr.Params["tstart"] != "10u" || tr.Params["tmax"] != "5u" {
		t.Fatalf("tran params = %v", tr.Params)
	}
	if len(spec.Includes) != 1 || spec.Includes[0] != "models.inc" {
		t.Fatalf("includes = %v", spec.Includes)
	}
}

package components

import (
	"strings"
	"testing"
)

func pinOf(t *testing.T, pins []Pin, name string) Pin {
	t.Helper()
	for _, p := range pins {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("pin %q not found in %v", name, pins)
	return Pin{}
}

func TestRHorizPins(t *testing.T) {
	pins := PinsOf("R", false)
	if got, want := pinOf(t, pins, "1"), (Pin{"1", -30, 0}); got != want {
		t.Fatalf("R pin1 = %+v, want %+v", got, want)
	}
	if got, want := pinOf(t, pins, "2"), (Pin{"2", 30, 0}); got != want {
		t.Fatalf("R pin2 = %+v, want %+v", got, want)
	}
}

func TestRVerticalPins(t *testing.T) {
	pins := PinsOf("R", true)
	if got, want := pinOf(t, pins, "1"), (Pin{"1", 0, -30}); got != want {
		t.Fatalf("R vertical pin1 = %+v, want %+v", got, want)
	}
	if got, want := pinOf(t, pins, "2"), (Pin{"2", 0, 30}); got != want {
		t.Fatalf("R vertical pin2 = %+v, want %+v", got, want)
	}
}

func TestCLSameAsR(t *testing.T) {
	for _, typ := range []string{"C", "L"} {
		h := PinsOf(typ, false)
		if pinOf(t, h, "1") != (Pin{"1", -30, 0}) || pinOf(t, h, "2") != (Pin{"2", 30, 0}) {
			t.Fatalf("%s horizontal pins wrong: %v", typ, h)
		}
		v := PinsOf(typ, true)
		if pinOf(t, v, "1") != (Pin{"1", 0, -30}) || pinOf(t, v, "2") != (Pin{"2", 0, 30}) {
			t.Fatalf("%s vertical pins wrong: %v", typ, v)
		}
	}
}

func TestSourcePinsHorizAndVert(t *testing.T) {
	h := PinsOf("S4Q_V", false)
	// empirically calibrated: pin1 is the "+" pin on the right.
	if pinOf(t, h, "1") != (Pin{"1", 30, 0}) || pinOf(t, h, "2") != (Pin{"2", -30, 0}) {
		t.Fatalf("S4Q_V horizontal pins wrong: %v", h)
	}
	v := PinsOf("S4Q_V", true)
	if pinOf(t, v, "1") != (Pin{"1", 0, -30}) || pinOf(t, v, "2") != (Pin{"2", 0, 30}) {
		t.Fatalf("S4Q_V vertical pins wrong: %v", v)
	}
	if PinsOf("S4Q_I", true)[0] != (Pin{"1", 0, -30}) {
		t.Fatal("S4Q_I vertical pin1 wrong")
	}
}

func TestBJTMOSFETGNDPins(t *testing.T) {
	bjt := PinsOf("_BJT", false)
	if pinOf(t, bjt, "C") != (Pin{"C", 0, -30}) || pinOf(t, bjt, "B") != (Pin{"B", -30, 0}) || pinOf(t, bjt, "E") != (Pin{"E", 0, 30}) {
		t.Fatalf("_BJT pins wrong: %v", bjt)
	}
	m := PinsOf("_MOSFET", false)
	if pinOf(t, m, "D") != (Pin{"D", 0, -30}) || pinOf(t, m, "G") != (Pin{"G", -30, 0}) || pinOf(t, m, "S") != (Pin{"S", 0, 30}) {
		t.Fatalf("_MOSFET pins wrong: %v", m)
	}
	gnd := PinsOf("GND", false)
	if len(gnd) != 1 || gnd[0] != (Pin{"gnd", 0, 0}) {
		t.Fatalf("GND pins wrong: %v", gnd)
	}
}

func TestBuildersRender(t *testing.T) {
	r := R("R1", 100, 100, "1k", false)
	if r.Render() != `<R R1 1 100 100 -26 15 0 0 "1k" 1>` {
		t.Fatalf("R render = %q", r.Render())
	}
	rv := R("R1", 100, 100, "1k", true)
	if !strings.Contains(rv.Render(), `1 100 100 -26 15 3 0`) {
		t.Fatalf("R vertical render = %q", rv.Render())
	}
	c := C("C1", 0, 0, "100n", false)
	if c.Render() != `<C C1 1 0 0 -26 17 0 0 "100n" 1>` {
		t.Fatalf("C render = %q", c.Render())
	}
	l := L("L1", 0, 0, "1m", false)
	if l.Render() != `<L L1 1 0 0 -26 10 0 0 "1m" 1>` {
		t.Fatalf("L render = %q", l.Render())
	}
	v := S4QV("V1", -20, 0, "DC 1", false)
	if v.Render() != `<S4Q_V V1 1 -20 0 18 -26 0 0 "DC 1" 1 "" 0 "" 0 "" 0 "" 0>` {
		t.Fatalf("S4Q_V render = %q", v.Render())
	}
	if S4QI("I1", 0, 0, "DC 1m", false).Type != "S4Q_I" {
		t.Fatal("S4QI type wrong")
	}
	g := GND(200, 300)
	if g.Render() != `<GND * 1 200 300 0 0 0 0>` {
		t.Fatalf("GND render = %q", g.Render())
	}
}

func TestSemiconductorRender(t *testing.T) {
	d := Diode("D1", 0, 0, "1N4148", false)
	if d.Render() != `<DIODE_SPICE D1 1 0 0 -26 15 0 0 "1N4148" 1 "" 0 "" 0 "" 0 "" 0 "2" 1 "D" 1>` {
		t.Fatalf("DIODE_SPICE render = %q", d.Render())
	}
	q := BJT("Q1", 0, 0, false)
	if !strings.Contains(q.Render(), `<_BJT Q1 1 0 0 8 -26 0 0 "npn" 1 "1e-14" 0 "1" 0 "1" 0 "0.3" 0`) {
		t.Fatalf("_BJT render prefix = %q", q.Render())
	}
	if !strings.Contains(BJT("Q1", 0, 0, true).Render(), `"pnp" 1`) {
		t.Fatal("_BJT pnp body wrong")
	}
	m := MOSFET("M1", 0, 0, false)
	if !strings.Contains(m.Render(), `<_MOSFET M1 1 0 0 8 -26 0 0 "nfet" 0 "1.0 V" 1`) {
		t.Fatalf("_MOSFET render prefix = %q", m.Render())
	}
	if !strings.Contains(MOSFET("M1", 0, 0, true).Render(), `"pfet" 0`) {
		t.Fatal("_MOSFET pmos body wrong")
	}
}

func TestSimulationBlocks(t *testing.T) {
	ac := AC("AC1", 0, 0, "log", "10 Hz", "1 MHz", 91)
	want := `<.AC AC1 1 0 0 0 70 0 0 "log" 1 "10 Hz" 1 "1 MHz" 1 "91" 1 "no" 0>`
	if ac.Render() != want {
		t.Fatalf(".AC render = %q", ac.Render())
	}
	tr := TR("TR1", 0, 0, "0", "1 ms", 200, "1 us")
	if !strings.Contains(tr.Render(), `<.TR TR1 1 0 0 0 70 0 0 "lin" 1 "0" 1 "1 ms" 1 "200" 0 "Trapezoidal" 0 "2" 0 "1 us" 0 "1e-16" 0`) {
		t.Fatalf(".TR render = %q", tr.Render())
	}
	op := DCOrOp("DC1", 40, 40)
	if !strings.Contains(op.Render(), `<.DC DC1 1 40 40 0 70 0 0 "26.85" 0 "0.001" 0`) {
		t.Fatalf(".DC render = %q", op.Render())
	}
	sp := SpicePar("SpicePar1", 0, 0, map[string]string{"g0": "1"})
	if !strings.Contains(sp.Render(), `"g0=1" 1`) {
		t.Fatalf("SpicePar render = %q", sp.Render())
	}
	inc := SpiceInclude("SpiceInclude1", 0, 0, "models.lib")
	if !strings.Contains(inc.Render(), `"models.lib" 1 "" 0`) {
		t.Fatalf("SpiceInclude render = %q", inc.Render())
	}
}

func TestSupported(t *testing.T) {
	for _, ty := range []string{"R", "C", "L", "S4Q_V", "S4Q_I", "GND", "DIODE_SPICE", "_BJT", "_MOSFET", ".AC", ".TR", ".DC", "SpicePar", "SpiceInclude"} {
		if !Supported(ty) {
			t.Fatalf("%s must be supported", ty)
		}
	}
	if Supported("X") {
		t.Fatal("X must not be supported in MVP")
	}
}

func TestPinsOfUnknown(t *testing.T) {
	if PinsOf("SpicePar", false) != nil {
		t.Fatal("analysis blocks have no pins")
	}
}

package layout

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

// rcSpec returns the test circuit: SPICE voltage divider low-pass.
func rcSpec() *model.CircuitSpec {
	return &model.CircuitSpec{
		Name: "rclow",
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Value: "DC 0 AC 1", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "0"},
			}},
			{Ref: "R1", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "out"},
			}},
			{Ref: "C1", Type: "C", Value: "100n", Pins: []model.Pin{
				{Name: "1", Net: "out"}, {Name: "2", Net: "0"},
			}},
		},
		Analyses: []model.Analysis{
			{Type: "ac", Params: map[string]string{
				"sweep": "dec", "points": "91", "start": "10", "stop": "1M",
			}},
		},
	}
}

func TestBuildRCLowPass(t *testing.T) {
	doc, err := Build(rcSpec())
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]int{}
	for _, c := range doc.Components {
		refs[c.Ref]++
	}
	for _, want := range []string{"V1", "R1", "C1"} {
		if refs[want] != 1 {
			t.Errorf("component %s count = %d, want 1 (all: %v)", want, refs[want], refs)
		}
	}
	if refs["*"] != 2 { // two ground symbols for the two "0" pins
		t.Errorf("GND count = %d (%+v), want 2", refs["*"], doc.Components)
	}
	Ha, err := HasAnalysisBlock(doc, "ac")
	if err != nil || !Ha {
		t.Fatalf("no .AC block in layout, err=%v", err)
	}
}

func TestBuildRCLowPassWires(t *testing.T) {
	doc, err := Build(rcSpec())
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	// in attaches to V1 pin1 and R1 pin1; out to R1 pin2 and C1 pin1.
	if labels["in"] != 2 || labels["out"] != 2 {
		t.Fatalf("labels = %v, want in:2 out:2", labels)
	}
	// all wire endpoints and component anchors must be multiples of 10
	// (label offsets LX/LY may be arbitrary — Qucs allows e.g. "208 270").
	for _, w := range doc.Wires {
		for _, v := range []int{w.X1, w.Y1, w.X2, w.Y2} {
			if v%10 != 0 {
				t.Fatalf("wire %v has off-grid coordinate %d", w, v)
			}
		}
	}
	for _, c := range doc.Components {
		if c.X%10 != 0 || c.Y%10 != 0 {
			t.Fatalf("component %s off grid: %d,%d", c.Ref, c.X, c.Y)
		}
	}
}

func TestBuildACDecAutoresizingMask(t *testing.T) {
	doc, err := Build(rcSpec())
	if err != nil {
		t.Fatal(err)
	}
	var ac *sch.ComponentLine
	for i := range doc.Components {
		if doc.Components[i].Type == ".AC" {
			ac = &doc.Components[i]
		}
	}
	if ac == nil {
		t.Fatal("no .AC block")
	}
	// dec 91 over 10..1M (5 decades) -> Qucs Points = 91*5+1 = 456; the
	// stop goes in suffix-free scientific form (spice4qucs loses "M").
	want := `<.AC AC1 1 40 560 0 70 0 0 "log" 1 "10" 1 "1e+06" 1 "456" 1 "no" 0>`
	if ac.Render() != want {
		t.Fatalf("ac line = %q, want %q", ac.Render(), want)
	}
}

func TestBuildPolarity(t *testing.T) {
	doc, err := Build(rcSpec())
	if err != nil {
		t.Fatal(err)
	}
	var v1 *sch.ComponentLine
	for i := range doc.Components {
		if doc.Components[i].Ref == "V1" {
			v1 = &doc.Components[i]
		}
	}
	// S4Q_V horizontal: pin1"+" at +30 must wire to net "in" (stub to the right)
	if strings.Contains(v1.Render(), "Mirror: 1") {
		t.Fatal("V1 must be horizontal in MVP")
	}
	if !strings.Contains(v1.Render(), "DC 0 AC 1") {
		t.Fatalf("V1 value lost: %q", v1.Render())
	}
}

func TestBuildBJTFlavors(t *testing.T) {
	spec := &model.CircuitSpec{
		Name: "bjtpnp",
		Components: []model.Component{
			{Ref: "Q1", Type: "_BJT", Value: "QMOD", Props: map[string]string{"flavor": "pnp"},
				Pins: []model.Pin{{Name: "C", Net: "c"}, {Name: "B", Net: "b"}, {Name: "E", Net: "0"}}},
			{Ref: "M1", Type: "_MOSFET", Value: "MMOD", Props: map[string]string{"flavor": "pmos"},
				Pins: []model.Pin{{Name: "D", Net: "d"}, {Name: "G", Net: "g"}, {Name: "S", Net: "0"}}},
		},
	}
	doc, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, c := range doc.Components {
		if c.Type == "_BJT" && strings.Contains(c.Render(), `"pnp" 1`) {
			found++
		}
		if c.Type == "_MOSFET" && strings.Contains(c.Render(), `"pfet" 0`) {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("flavor rendering wrong; components=%+v", doc.Components)
	}
}

func TestBuildUnsupportedDeviceFails(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{{Ref: "J1", Type: "JFET", Pins: nil}},
	}
	_, err := Build(spec)
	if err == nil || !strings.Contains(err.Error(), "unsupported component type") {
		t.Fatalf("err = %v", err)
	}
}

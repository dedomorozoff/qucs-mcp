package layout

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

// crystalSpec returns the plainest version of the crystal receiver:
// source → series chain → detector diode with two shunts. It is the
// showcase circuit for the pretty router.
func crystalSpec() *model.CircuitSpec {
	return &model.CircuitSpec{
		Name: "crystal",
		Components: []model.Component{
			{Ref: "Vant", Type: "S4Q_V", Value: "DC 0 AC 1", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "0"}}},
			{Ref: "Ra", Type: "R", Value: "20", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "ant"}}},
			{Ref: "L1", Type: "L", Value: "250u", Pins: []model.Pin{
				{Name: "1", Net: "ant"}, {Name: "2", Net: "tun"}}},
			{Ref: "C1", Type: "C", Value: "100p", Pins: []model.Pin{
				{Name: "1", Net: "tun"}, {Name: "2", Net: "0"}}},
			{Ref: "D1", Type: "DIODE_SPICE", Value: "DDET", Pins: []model.Pin{
				{Name: "1", Net: "tun"}, {Name: "2", Net: "det"}}},
			{Ref: "Cf", Type: "C", Value: "10n", Pins: []model.Pin{
				{Name: "1", Net: "det"}, {Name: "2", Net: "0"}}},
			{Ref: "Rh", Type: "R", Value: "47k", Pins: []model.Pin{
				{Name: "1", Net: "det"}, {Name: "2", Net: "0"}}},
		},
	}
}

// TestRoutePrettyCrystal pins down the whole geometry: the grounded
// source leads the chain, Ra/L1/D1 follow on the rail, and the shunt
// devices hang below their junction slots with ground symbols beneath.
func TestRoutePrettyCrystal(t *testing.T) {
	doc, err := Build(crystalSpec())
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]*sch.ComponentLine{}
	for i := range doc.Components {
		byRef[doc.Components[i].Ref] = &doc.Components[i]
	}
	// chain anchors: Vant 140 (head ground at 110,250) then each device
	// advanced by the rail; shunts on hangAnchors under the rails.
	for ref, x := range map[string]int{"Vant": 140, "Ra": 260, "L1": 380, "D1": 540} {
		c := byRef[ref]
		if c == nil {
			t.Fatalf("%s missing", ref)
		}
		if c.X != x || c.Y != prettyRailY {
			t.Fatalf("%s at %d,%d, want %d,%d", ref, c.X, c.Y, x, prettyRailY)
		}
		if c.Rot != 0 || c.Mirror != 0 {
			t.Fatalf("%s must stay horizontal, got rot=%d mirror=%d", ref, c.Rot, c.Mirror)
		}
	}
	for ref, x := range map[string]int{"C1": 450, "Cf": 610, "Rh": 680} {
		c := byRef[ref]
		if c == nil {
			t.Fatalf("%s missing", ref)
		}
		if c.X != x || c.Y != prettyHangY {
			t.Fatalf("%s hangs at %d,%d, want %d,%d", ref, c.X, c.Y, x, prettyHangY)
		}
		// vertical shunt with pin1 (the rail-side pin) on top
		if c.Rot != 3 || c.Mirror != 0 {
			t.Fatalf("%s must be a Rot=3 vertical, got rot=%d mirror=%d", ref, c.Rot, c.Mirror)
		}
	}
	countGND := 0
	for _, c := range doc.Components {
		if c.Type == "GND" {
			countGND++
		}
	}
	// source head + one ground under every shunt bottom pin
	if countGND != 4 {
		t.Fatalf("GND count = %d, want 4 (source head + three shunts)", countGND)
	}

	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	for _, net := range []string{"in", "ant", "tun", "det"} {
		if labels[net] != 1 {
			t.Fatalf("net %q carries %d labels, want exactly 1: %v", net, labels[net], labels)
		}
	}
	// every coordinate stays on the 10-grid
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
	// C1 junction: the rail halves meet exactly at the drop wire's top
	// endpoint, the drop ends on the shunt's top pin, and the bottom
	// pin wires into a GND.
	railA := sch.WireLine{X1: 410, Y1: prettyRailY, X2: 450, Y2: prettyRailY, Label: "tun"}
	railB := sch.WireLine{X1: 450, Y1: prettyRailY, X2: 510, Y2: prettyRailY}
	drop := sch.WireLine{X1: 450, Y1: prettyRailY, X2: 450, Y2: prettyHangY - 30}
	if !wireIn(doc, railA) || !wireIn(doc, railB) || !wireIn(doc, drop) {
		t.Fatalf("C1 junction wires missing:\n%s", doc.Render())
	}
	if !wireIn(doc, sch.WireLine{X1: 450, Y1: prettyHangY + 30, X2: 450, Y2: prettyHangY + 30 + gndOffset}) {
		t.Fatalf("C1 bottom ground wire missing")
	}
}

func wireIn(doc *sch.Document, want sch.WireLine) bool {
	for _, w := range doc.Wires {
		if w.X1 == want.X1 && w.Y1 == want.Y1 && w.X2 == want.X2 && w.Y2 == want.Y2 {
			return true
		}
	}
	return false
}

// TestRoutePrettyFloatingSourceSuiteSeed: a floating-entry source seeds
// its own chain with a labeled head stub; the strict parallel loop
// (source across a resistor) draws as two stub chains sharing labels.
func TestRoutePrettyFloatingSourceSeed(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Value: "DC 5", Pins: []model.Pin{
				{Name: "1", Net: "load"}, {Name: "2", Net: "mid"}}},
			{Ref: "R1", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "mid"}, {Name: "2", Net: "load"}}},
		},
	}
	doc, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	// The walker cannot chain this pair (a closed V/R loop needs pin2-
	// side entry), but labels still merge both sides: one labeled stub
	// per device endpoint.
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	if labels["mid"] != 2 || labels["load"] != 2 {
		t.Fatalf("labels = %v, want stub pair for mid and load", labels)
	}
	byRef := map[string]*sch.ComponentLine{}
	for i := range doc.Components {
		byRef[doc.Components[i].Ref] = &doc.Components[i]
	}
	if byRef["V1"].X >= byRef["R1"].X {
		t.Fatalf("source must lead: V.x=%d R.x=%d", byRef["V1"].X, byRef["R1"].X)
	}
}

// TestRouteHangsOnRailLessNet: shunts whose net never meets the chain
// get labeled stubs above each device (e.g. two resistors sharing one
// floating net).
func TestRouteHangsOnRailLessNet(t *testing.T) {
	spec := &model.CircuitSpec{
		Components: []model.Component{
			{Ref: "R1", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "a"}, {Name: "2", Net: "0"}}},
			{Ref: "R2", Type: "R", Value: "2k", Pins: []model.Pin{
				{Name: "1", Net: "a"}, {Name: "2", Net: "0"}}},
		},
	}
	doc, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	if labels["a"] != 2 {
		t.Fatalf("labels = %v, want two labeled stubs on net a", labels)
	}
}

// bridgeSpec: R3 closes the loop R1→R3 over net n2 while R2 spans n1…
// n3 — the walker cannot chain R2 into the first rail, so it forms a
// second chain whose endpoints reuse the same net labels.
func bridgeSpec() *model.CircuitSpec {
	return &model.CircuitSpec{
		Name: "bridge",
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Value: "DC 5", Pins: []model.Pin{
				{Name: "1", Net: "n1"}, {Name: "2", Net: "0"}}},
			{Ref: "R1", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "n1"}, {Name: "2", Net: "n2"}}},
			{Ref: "R2", Type: "R", Value: "2k", Pins: []model.Pin{
				{Name: "1", Net: "n1"}, {Name: "2", Net: "n3"}}},
			{Ref: "R3", Type: "R", Value: "3k", Pins: []model.Pin{
				{Name: "1", Net: "n2"}, {Name: "2", Net: "n3"}}},
		},
	}
}

// TestMultiChainPretty: loop shapes that the walker cannot express as
// one rail draw as several chains sharing net labels instead of
// regressing to something invalid — the second chain (R2 looped back
// over labels) keeps its own head/tail stubs.
func TestMultiChainPretty(t *testing.T) {
	doc, err := Build(bridgeSpec())
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]*sch.ComponentLine{}
	for i := range doc.Components {
		byRef[doc.Components[i].Ref] = &doc.Components[i]
	}
	for ref, x := range map[string]int{"V1": 140, "R1": 260, "R3": 380, "R2": 570} {
		c := byRef[ref]
		if c == nil || c.X != x || c.Y != prettyRailY {
			t.Fatalf("%s anchor wrong: %+v", ref, c)
		}
	}
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	// n1: V1→R1 rail + R2's head stub; n2: R1→R3 rail; n3: the tail
	// stubs of R3 and R2.
	if labels["n1"] != 2 || labels["n2"] != 1 || labels["n3"] != 2 {
		t.Fatalf("labels = %v", labels)
	}
}

// TestPrettySkipsSemiconductors: a _BJT in the spec forces the legacy
// path for the whole document — the pretty router must not drop or
// half-render the transistor.
func TestPrettySkipsSemiconductors(t *testing.T) {
	doc, err := Build(bridgeSpec())
	if err != nil {
		t.Fatal(err)
	}
	_ = doc
	spec := &model.CircuitSpec{
		Name: "semi",
		Components: []model.Component{
			{Ref: "V1", Type: "S4Q_V", Value: "DC 5", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "0"}}},
			{Ref: "Q1", Type: "_BJT", Value: "QM", Pins: []model.Pin{
				{Name: "C", Net: "c"}, {Name: "B", Net: "in"}, {Name: "E", Net: "0"}}},
		},
	}
	doc, err = Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	sawBJT := false
	for _, c := range doc.Components {
		if c.Type == "_BJT" {
			sawBJT = true
		}
	}
	if !sawBJT {
		t.Fatal("_BJT dropped from the fallback layout")
	}
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	if labels["in"] != 2 { // V1 pin1 stub + Q1 base stub
		t.Fatalf("labels = %v, want two legacy stubs on in", labels)
	}
}

// TestPlacementsKeepLegacyLayout: build-with-positions keeps the stub
// layout contract (pretty is a fresh-build feature only).
func TestPlacementsKeepLegacyLayout(t *testing.T) {
	doc, err := BuildPlaced(crystalSpec(), Placements{"Ra": [2]int{500, 300}})
	if err != nil {
		t.Fatal(err)
	}
	var ra *sch.ComponentLine
	for i := range doc.Components {
		if doc.Components[i].Ref == "Ra" {
			ra = &doc.Components[i]
		}
	}
	if ra.X != 500 || ra.Y != 300 {
		t.Fatalf("placement ignored: Ra at %d,%d", ra.X, ra.Y)
	}
	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	if labels["in"] != 2 || labels["ant"] != 2 {
		t.Fatalf("labels = %v, want legacy stubs", labels)
	}
}

// TestRouteVerticalBody: the pretty path shares the legacy document
// contract (orientation bodies render in the MIRROR ROT order Qucs-S
// expects); a hanger C is vertical Rot=3 with pin1 up.
func TestRouteVerticalBody(t *testing.T) {
	doc, err := Build(crystalSpec())
	if err != nil {
		t.Fatal(err)
	}
	rendered := doc.Render()
	if !strings.Contains(rendered, `<C C1 1 450 300 -26 17 0 3 "100p" 1>`) {
		t.Fatalf("C1 vertical body wrong:\n%s", rendered)
	}
	// the DC Operating point remark, not a layout defect
	if len(doc.Remarks) != 1 || !strings.Contains(doc.Remarks[0], "DC operating point") {
		t.Fatalf("remarks = %v", doc.Remarks)
	}
}

package layout

import (
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

// hangY is the anchor row of the vertical shunts hanging under the
// main chain row.
func hangY() int { return prettyRailY + prettyHangOff }

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
		if c.X != x || c.Y != hangY() {
			t.Fatalf("%s hangs at %d,%d, want %d,%d", ref, c.X, c.Y, x, hangY())
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
	drop := sch.WireLine{X1: 450, Y1: prettyRailY, X2: 450, Y2: hangY() - 30}
	if !wireIn(doc, railA) || !wireIn(doc, railB) || !wireIn(doc, drop) {
		t.Fatalf("C1 junction wires missing:\n%s", doc.Render())
	}
	if !wireIn(doc, sch.WireLine{X1: 450, Y1: hangY() + 30, X2: 450, Y2: hangY() + 30 + gndOffset}) {
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

// TestRoutePrettyFloatingSourceSeed: a floating-entry source seeds its
// own chain with a labeled head stub; the strict parallel loop
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

// bridgeSpec: R3 closes the loop R1→R3→R2 over nets n3/n2, so the walker
// cannot chain R2 into the first rail — it forms a second chain whose
// endpoints reuse the same net labels.
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
// regressing to the legacy stub layout (stubs remain for devices whose
// endpoint rails were already drawn).
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

// trfSpec is the one-transistor TRF receiver: tuned tank into an RF
// stage with base divider, bypassed emitter, and a diode detector.
// It exercises every pretty feature at once: a grounded source chain,
// a stage with emitter shunts, a feeder (Rc), and detector shunts.
func trfSpec() *model.CircuitSpec {
	return &model.CircuitSpec{
		Name: "trf",
		Components: []model.Component{
			{Ref: "Vant", Type: "S4Q_V", Value: "dc 0 ac 1 0", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "0"}}},
			{Ref: "Ra", Type: "R", Value: "100", Pins: []model.Pin{
				{Name: "1", Net: "in"}, {Name: "2", Net: "ant"}}},
			{Ref: "L1", Type: "L", Value: "250u", Pins: []model.Pin{
				{Name: "1", Net: "ant"}, {Name: "2", Net: "tun"}}},
			{Ref: "C1", Type: "C", Value: "75p", Pins: []model.Pin{
				{Name: "1", Net: "tun"}, {Name: "2", Net: "0"}}},
			{Ref: "Cc", Type: "C", Value: "10n", Pins: []model.Pin{
				{Name: "1", Net: "tun"}, {Name: "2", Net: "aud"}}},
			{Ref: "Rb1", Type: "R", Value: "100k", Pins: []model.Pin{
				{Name: "1", Net: "vcc"}, {Name: "2", Net: "aud"}}},
			{Ref: "Rb2", Type: "R", Value: "22k", Pins: []model.Pin{
				{Name: "1", Net: "aud"}, {Name: "2", Net: "0"}}},
			{Ref: "Q1", Type: "_BJT", Value: "Qmod", Props: map[string]string{"flavor": "npn"}, ModelParams: map[string]string{"BF": "150"}, Pins: []model.Pin{
				{Name: "C", Net: "amp"}, {Name: "B", Net: "aud"}, {Name: "E", Net: "em0"}}},
			{Ref: "Re", Type: "R", Value: "1k", Pins: []model.Pin{
				{Name: "1", Net: "em0"}, {Name: "2", Net: "0"}}},
			{Ref: "Ce", Type: "C", Value: "100u", Pins: []model.Pin{
				{Name: "1", Net: "em0"}, {Name: "2", Net: "0"}}},
			{Ref: "Rc", Type: "R", Value: "10k", Pins: []model.Pin{
				{Name: "1", Net: "vcc"}, {Name: "2", Net: "amp"}}},
			{Ref: "D1", Type: "DIODE_SPICE", Value: "DDET", Pins: []model.Pin{
				{Name: "1", Net: "amp"}, {Name: "2", Net: "det"}}},
			{Ref: "Cf", Type: "C", Value: "1n", Pins: []model.Pin{
				{Name: "1", Net: "det"}, {Name: "2", Net: "0"}}},
			{Ref: "Rh", Type: "R", Value: "10k", Pins: []model.Pin{
				{Name: "1", Net: "det"}, {Name: "2", Net: "0"}}},
			{Ref: "V1", Type: "S4Q_V", Value: "dc 6", Pins: []model.Pin{
				{Name: "1", Net: "vcc"}, {Name: "2", Net: "0"}}},
		},
	}
}

// TestRoutePrettyBJTStage pins the whole stage geometry: the chain
// runs Vant→Ra→L1→Cc on the main row, rises through Q1 (base on the
// main row, collector feeding the exit rail one row up), while Rc
// hangs above that rail with a "vcc" stub, Re/Ce hang under the
// emitter leg, and the bias pair V1/Rb1 closes the vcc net on a second
// chain to the right.
func TestRoutePrettyBJTStage(t *testing.T) {
	doc, err := Build(trfSpec())
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]*sch.ComponentLine{}
	for i := range doc.Components {
		byRef[doc.Components[i].Ref] = &doc.Components[i]
	}
	expect := map[string]struct {
		x, y, wantRot int
	}{
		// chain 1: tank into the base
		"Vant": {140, prettyRailY, 0},
		"Ra":   {260, prettyRailY, 0},
		"L1":   {380, prettyRailY, 0},
		"C1":   {450, hangY(), 3},
		"Cc":   {540, prettyRailY, 0},
		// the stage itself; its exit continues on the row above
		"Q1": {700, prettyRailY, 0},
		"D1": {830, prettyRailY - 30, 0},
		// detector shunts under the det rail
		"Cf": {900, hangY() - 30, 3},
		"Rh": {970, hangY() - 30, 3},
		// the emitter leg shunts under the leg's own rail
		"Re": {740, hangY() + 30, 3},
		"Ce": {810, hangY() + 30, 3},
		// the feeder rises above the collector rail
		"Rc": {740, prettyRailY - 60, 3},
		// chain 2: the vcc bias feed
		"V1":  {1090, prettyRailY, 0},
		"Rb1": {1210, prettyRailY, 0},
		"Rb2": {610, hangY(), 3},
	}
	for ref, e := range expect {
		c := byRef[ref]
		if c == nil {
			t.Fatalf("%s missing", ref)
		}
		if c.X != e.x || c.Y != e.y {
			t.Fatalf("%s at %d,%d, want %d,%d", ref, c.X, c.Y, e.x, e.y)
		}
		if c.Rot != e.wantRot {
			t.Fatalf("%s rot = %d, want %d", ref, c.Rot, e.wantRot)
		}
	}
	if byRef["Q1"].Mirror != 0 {
		t.Fatal("Q1 must stay unmirrored")
	}

	countGND := 0
	for _, c := range doc.Components {
		if c.Type == "GND" {
			countGND++
		}
	}
	// Vant head + C1 + Rb2 + Re + Ce + Cf + Rh + V1 head = 8
	if countGND != 8 {
		t.Fatalf("GND count = %d, want 8", countGND)
	}

	labels := map[string]int{}
	for _, w := range doc.Wires {
		if w.Label != "" {
			labels[w.Label]++
		}
	}
	for net, n := range map[string]int{
		"in": 1, "ant": 1, "tun": 1, "aud": 2, "amp": 1, "det": 1,
		"em0": 1, "vcc": 2,
	} {
		if labels[net] != n {
			t.Fatalf("net %q carries %d labels, want %d: %v", net, labels[net], n, labels)
		}
	}

	rend := doc.Render()

	// Rc body: vertical, stub pin (vcc) pointing up into a "vcc" stub
	if !strings.Contains(rend, `<R Rc 1 740 160 -26 15 0 3 "10k" 1>`) {
		t.Fatalf("Rc feeder body wrong:\n%s", rend)
	}
	if !wireIn(doc, sch.WireLine{X1: 740, Y1: prettyRailY - 90, X2: 740, Y2: prettyRailY - 130, Label: "vcc"}) {
		t.Fatalf("Rc vcc stub missing:\n%s", rend)
	}
	// the amp rail passes exactly at the Rc bottom pin and ends at D1
	if !wireIn(doc, sch.WireLine{X1: 740, Y1: prettyRailY - 30, X2: 800, Y2: prettyRailY - 30}) {
		t.Fatalf("amp rail to D1 missing:\n%s", rend)
	}
	// the emitter leg rail starts exactly at the emitter pin
	if !wireIn(doc, sch.WireLine{X1: 700, Y1: prettyRailY + 30, X2: 740, Y2: prettyRailY + 30, Label: "em0"}) {
		t.Fatalf("emitter leg rail missing:\n%s", rend)
	}
}

// TestRoutePrettyMOSFETStage: the same stage shape must fall out of the
// _MOSFET (D/G/S pins) — geometry only, one quick probe.
func TestRoutePrettyMOSFETStage(t *testing.T) {
	spec := trfSpec()
	for i := range spec.Components {
		c := &spec.Components[i]
		if c.Type == "_BJT" {
			c.Type = "_MOSFET"
			for j := range c.Pins {
				switch c.Pins[j].Name {
				case "C":
					c.Pins[j].Name = "D"
				case "B":
					c.Pins[j].Name = "G"
				case "E":
					c.Pins[j].Name = "S"
				}
			}
			c.Props["flavor"] = "nfet"
		}
	}
	doc, err := Build(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := "<" + `_MOSFET Q1 1 700 220` + " "
	if !strings.Contains(doc.Render(), want) &&
		!strings.Contains(doc.Render(), "<_MOSFET Q1 1 700 220 ") {
		t.Fatalf("MOSFET not on the stage anchor:\n%s", doc.Render())
	}
}

// TestRoutePrettyFloatingEmitter: a stage whose emitter leg carries no
// grounded shunt has no renderable shape — the whole build defers to
// the legacy stub layout.
func TestRoutePrettyFloatingEmitter(t *testing.T) {
	spec := trfSpec()
	// move both emitter shunts' ground pins onto starved chain-less nets
	for i := range spec.Components {
		if spec.Components[i].Ref == "Re" {
			spec.Components[i].Pins[1].Net = "X"
		}
		if spec.Components[i].Ref == "Ce" {
			spec.Components[i].Pins[1].Net = "Y"
		}
	}
	plan, ok := planPretty(spec)
	if ok {
		t.Fatalf("plan accepted a floating emitter leg: %+v", plan.chain)
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
	if !strings.Contains(doc.Render(), `<C C1 1 450 300 -26 17 0 3 "100p" 1>`) {
		t.Fatalf("C1 vertical body wrong:\n%s", doc.Render())
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

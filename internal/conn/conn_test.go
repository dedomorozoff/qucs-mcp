package conn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dedomorozoff/qucs-mcp/internal/layout"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

func rcSpec() *model.CircuitSpec {
	return &model.CircuitSpec{
		Name: "rc",
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
	}
}

// TestRebuildRoundTrip: a generated schematic must rebuild into the
// same pin nets.
func TestRebuildRoundTrip(t *testing.T) {
	doc, err := layout.Build(rcSpec())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "rc.sch")
	if err := doc.WriteTo(p); err != nil {
		t.Fatal(err)
	}
	rd, err := sch.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	spec, warnings := Rebuild(rd, "rc")
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	byRef := map[string]model.Component{}
	for _, c := range spec.Components {
		byRef[c.Ref] = c
	}
	if len(byRef) != 3 {
		t.Fatalf("components = %+v", spec.Components)
	}
	c1 := byRef["C1"]
	if c1.NetOf("1") != "out" || c1.NetOf("2") != "0" {
		t.Fatalf("C1 nets wrong: %+v", c1.Pins)
	}
	r1 := byRef["R1"]
	if r1.NetOf("1") != "in" || r1.NetOf("2") != "out" {
		t.Fatalf("R1 nets wrong: %+v", r1.Pins)
	}
	v1 := byRef["V1"]
	if v1.NetOf("1") != "in" || v1.NetOf("2") != "0" {
		t.Fatalf("V1 nets wrong: %+v", v1.Pins)
	}
	if r1.Value != "1k" || c1.Value != "100n" {
		t.Fatalf("values wrong: %q / %q", r1.Value, c1.Value)
	}
}

// TestRebuildRotatedOrientations runs the RC circuit with vertical
// passives (Rot=3) and a mirrored source (Mirror=1) — the two
// orientation forms the writer produces for those devices.
func TestRebuildRotatedOrientations(t *testing.T) {
	src := `<Qucs Schematic 25.1.1>
<Components>
  <S4Q_V V1 1 100 100 18 -26 0 1 "DC 0 AC 1" 1 "" 0 "" 0 "" 0 "" 0>
  <R R1 1 300 220 -26 15 3 0 "1k" 1>
  <C C1 1 300 340 -26 17 3 0 "100n" 1>
  <GND * 1 100 160 0 0 0 0>
  <GND * 1 300 400 0 0 0 0>
</Components>
<Wires>
  <100 70 130 70 "in" 115 60 0 "">
  <130 70 130 190 "" 0 0 0 "">
  <130 190 300 190 "" 0 0 0 "">
  <100 130 100 160 "" 0 0 0 "">
  <300 250 300 310 "out" 320 270 0 "">
  <300 370 300 400 "" 0 0 0 "">
</Wires>
`
	dir := t.TempDir()
	p := filepath.Join(dir, "rot.sch")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rd, err := sch.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parsed: comps=%d wires=%d", len(rd.Components), len(rd.Wires))
	spec, warnings := Rebuild(rd, "rot")
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	byRef := map[string]model.Component{}
	for _, c := range spec.Components {
		byRef[c.Ref] = c
	}
	// S4Q_V mirror=1: pin1 top (100,70), pin2 bottom (100,130 → gnd).
	v1 := byRef["V1"]
	if v1.NetOf("1") != "in" || v1.NetOf("2") != "0" {
		t.Fatalf("V1 nets: %+v", v1.Pins)
	}
	// Rot=3 passives: pin1 top (300,190), pin2 bottom (300,250).
	r1 := byRef["R1"]
	if r1.NetOf("1") != "in" || r1.NetOf("2") != "out" {
		t.Fatalf("R1 nets: %+v", r1.Pins)
	}
	c1 := byRef["C1"]
	if c1.NetOf("1") != "out" || c1.NetOf("2") != "0" {
		t.Fatalf("C1 nets: %+v", c1.Pins)
	}
}

// TestRebuildMergesSameLabels: identical labels on physically separate
// wires merge into one net.
func TestRebuildMergesSameLabels(t *testing.T) {
	src := `<Qucs Schematic 25.1.1>
<Components>
  <R RcalR 1 100 100 -26 15 0 0 "1k" 1>
  <C C1 1 400 100 18 -26 0 0 "100n" 1>
</Components>
<Wires>
  <70 100 100 100 "a" 80 80 0 "">
  <130 100 160 100 "b" 140 90 0 "">
  <370 100 400 100 "a" 380 90 0 "">
  <430 100 460 100 "b" 440 90 0 "">
</Wires>
`
	dir := t.TempDir()
	p := filepath.Join(dir, "lab.sch")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rd, err := sch.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	spec, warnings := Rebuild(rd, "lab")
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(spec.Components) != 2 {
		t.Fatalf("components: %+v", spec.Components)
	}
	byRef := map[string]model.Component{}
	for _, c := range spec.Components {
		byRef[c.Ref] = c
	}
	r := byRef["RcalR"]
	// ref RcalR with type R: pin1 left
	if r.NetOf("1") != "a" || r.NetOf("2") != "b" {
		t.Fatalf("RcalR nets: %+v", r.Pins)
	}
	c := byRef["C1"]
	if c.NetOf("1") != "a" || c.NetOf("2") != "b" {
		t.Fatalf("C1 nets: %+v", c.Pins)
	}
}

// TestRebuildUnknownComponentFlagsWarning but keeps the rest.
func TestRebuildUnknownComponentFlagsWarning(t *testing.T) {
	src := `<Qucs Schematic 25.1.1>
<Components>
  <Lib OP1 1 420 180 41 9 0 0 "SpiceOpamp" 0 "ad822" 0>
  <R R1 1 100 100 -26 15 0 0 "1k" 1>
</Components>
`
	dir := t.TempDir()
	p := filepath.Join(dir, "u.sch")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	rd, err := sch.Read(p)
	if err != nil {
		t.Fatal(err)
	}
	spec, warnings := Rebuild(rd, "u")
	if len(spec.Components) != 1 || spec.Components[0].Ref != "R1" {
		t.Fatalf("components: %+v", spec.Components)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "OP1") {
		t.Fatalf("warnings: %v", warnings)
	}
}

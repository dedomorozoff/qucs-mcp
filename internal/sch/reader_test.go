package sch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleFull = `<Qucs Schematic 26.1.1>
<Properties>
  <View=0,0,1200,900,1,0,0>
  <Grid=10,10,1>
  <DataSet=test.dat>
</Properties>
<Symbol>
</Symbol>
<Components>
  <R R1 1 100 100 -26 15 0 0 "1k" 1 "26.85" 0 "european" 0>
  <S4Q_V V1 1 -20 100 18 -26 0 0 "DC 5" 1 "" 0 "" 0 "" 0 "" 0>
  <GND * 1 60 160 0 0 0 0>
</Components>
<Wires>
  <60 100 70 100 "in" 65 90 0 "">
  <130 100 150 100 "" 0 0 0 "">
</Wires>
<Diagrams>
</Diagrams>
<Paintings>
  <Text 100 700 0 0 "hello" #000000 0>
</Paintings>
`

func TestReadDocument(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "test.sch")
	if werr := os.WriteFile(p, []byte(sampleFull), 0o644); werr != nil {
		t.Fatal(werr)
	}
	r, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "26.1.1" {
		t.Fatalf("version = %q", r.Version)
	}
	if len(r.Components) != 3 {
		t.Fatalf("components = %d", len(r.Components))
	}
	r1 := r.Components[0]
	if r1.Type != "R" || r1.Ref != "R1" || r1.X != 100 || r1.Y != 100 || r1.Rot != 0 || r1.Mirror != 0 {
		t.Fatalf("R1 = %+v", r1)
	}
	if !strings.Contains(r1.Body, `"1k" 1`) {
		t.Fatalf("R1 body = %q", r1.Body)
	}
	v1 := r.Components[1]
	if v1.Type != "S4Q_V" || v1.X != -20 || v1.Body == "" || !strings.Contains(v1.Body, `"DC 5" 1`) {
		t.Fatalf("V1 = %+v", v1)
	}
	gnd := r.Components[2]
	if gnd.Type != "GND" || gnd.Ref != "*" {
		t.Fatalf("GND = %+v", gnd)
	}
	if len(r.Wires) != 2 {
		t.Fatalf("wires = %d", len(r.Wires))
	}
	w0 := r.Wires[0]
	if w0.X1 != 60 || w0.Y1 != 100 || w0.X2 != 70 || w0.Y2 != 100 || w0.Label != "in" || w0.LX != 65 || w0.LY != 90 {
		t.Fatalf("wire0 = %+v", w0)
	}
	if r.ExtraPaintings == nil || len(r.ExtraPaintings) != 1 {
		t.Fatalf("paintings = %v", r.ExtraPaintings)
	}
	if len(r.ExtraPaintings) == 1 && !strings.Contains(r.ExtraPaintings[0], "hello") {
		t.Fatalf("paintings[0] = %q", r.ExtraPaintings[0])
	}
}

func TestParseComponentLine(t *testing.T) {
	c, err := parseComponentLine(`<M M1 1 480 170 18 -17 0 0 "M" 0 "3" 0 "nmos" 0>`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Type != "M" || c.Ref != "M1" || c.X != 480 || c.Y != 170 || c.LX != 18 || c.LY != -17 || c.Rot != 0 || c.Mirror != 0 {
		t.Fatalf("c = %+v", c)
	}
	if c.Body != `"M" 0 "3" 0 "nmos" 0` {
		t.Fatalf("body = %q", c.Body)
	}
	_, err = parseComponentLine(`<R>`)
	if err == nil {
		t.Fatal("short line must fail")
	}
	_, err = parseComponentLine(`garbage`)
	if err == nil {
		t.Fatal("non-angle line must fail")
	}
}

func TestRoundTrip(t *testing.T) {
	// a document written by the writer must read back losslessly for the
	// fields the reader is responsible for.
	d := NewDocument("rt")
	d.AddComponent(ComponentLine{Type: "R", Ref: "R1", X: 100, Y: 220, LX: -26, LY: 15, Rot: 3, Mirror: 1, Body: `"1k" 1`})
	d.AddWire(WireLine{X1: 60, Y1: 100, X2: 70, Y2: 100, Label: "in", LX: 65, LY: 90})
	dir := t.TempDir()
	p := filepath.Join(dir, "rt.sch")
	if err := d.WriteTo(p); err != nil {
		t.Fatal(err)
	}
	r, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Components) != 1 {
		t.Fatalf("components = %d", len(r.Components))
	}
	c := r.Components[0]
	want := d.Components[0]
	if c.Type != want.Type || c.Ref != want.Ref || c.X != want.X || c.Y != want.Y ||
		c.LX != want.LX || c.LY != want.LY || c.Rot != want.Rot || c.Mirror != want.Mirror || c.Body != want.Body {
		t.Fatalf("roundtrip mismatch:\nwant %+v\n got %+v", want, c)
	}
	w := r.Wires[0]
	if w != d.Wires[0] {
		t.Fatalf("wire roundtrip: %+v vs %+v", w, d.Wires[0])
	}
}

func TestUnknownComponentKept(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "u.sch")
	src := `<Qucs Schematic 26.1.1>
<Components>
  <Lib OP1 1 420 180 41 9 0 0 "SpiceOpamp" 0 "ad822" 0>
</Components>
`
	os.WriteFile(p, []byte(src), 0o644)
	r, err := Read(p)
	if err != nil {
		t.Fatalf("unknown components must not break reading: %v", err)
	}
	if len(r.Components) != 1 || r.Components[0].Type != "Lib" {
		t.Fatalf("components = %+v", r.Components)
	}
}

// ParseNumberFields guards against negative/signed offsets.
func TestParseNumberFields(t *testing.T) {
	c, err := parseComponentLine(`<R X1 1 -100 -70 -26 15 0 0 "1k" 1>`)
	if err != nil {
		t.Fatal(err)
	}
	if c.X != -100 || c.Y != -70 || c.LX != -26 || c.LY != 15 {
		t.Fatalf("c = %+v", c)
	}
}

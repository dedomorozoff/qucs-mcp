package sch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testDoc() *Document {
	d := NewDocument("test")
	d.AddComponent(ComponentLine{
		Type: "R", Ref: "R1", X: 100, Y: 100, LX: -26, LY: 15,
		Rot: 0, Mirror: 0, Body: `"1k" 1 "26.85" 0 "european" 0`,
	})
	d.AddWire(WireLine{X1: 60, Y1: 100, X2: 70, Y2: 100, Label: "in", LX: 70, LY: 80, Rot: 0})
	return d
}

func TestRenderHeaderAndSections(t *testing.T) {
	out := testDoc().Render()
	for _, want := range []string{
		"<Qucs Schematic " + SchemaVer + ">",
		"<Properties>",
		"<Grid=10,10,1>",
		"<DataSet=test.dat>",
		"<DataDisplay=test.dpl>",
		"<OpenDisplay=0>",
		"<Symbol>",
		"<Components>", "</Components>",
		"<Wires>", "</Wires>",
		"<Diagrams>", "</Diagrams>",
		"<Paintings>", "</Paintings>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestRenderComponentLine(t *testing.T) {
	out := testDoc().Render()
	want := `  <R R1 1 100 100 -26 15 0 0 "1k" 1 "26.85" 0 "european" 0>`
	if !strings.Contains(out, want) {
		t.Errorf("component line mismatch, want %q in:\n%s", want, out)
	}
}

func TestRenderWireLine(t *testing.T) {
	out := testDoc().Render()
	want := `  <60 100 70 100 "in" 70 80 0 "">`
	if !strings.Contains(out, want) {
		t.Errorf("wire line mismatch, want %q in:\n%s", want, out)
	}
}

func TestWriteToAtomic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "test.sch")
	d := testDoc()
	if err := d.WriteTo(p); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "R1") {
		t.Error("file missing component line")
	}
	// Temp leftovers must not remain next to the file.
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if e.Name() != "test.sch" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestLineFormats(t *testing.T) {
	c := ComponentLine{Type: "S4Q_V", Ref: "V1", X: -20, Y: 0, Body: `"DC 1" 1`}
	if got, want := c.Render(), `<S4Q_V V1 1 -20 0 0 0 0 0 "DC 1" 1>`; got != want {
		t.Errorf("component render = %q, want %q", got, want)
	}
	w := WireLine{X1: 0, Y1: 0, X2: 0, Y2: 20, Label: "out", LX: 10, LY: 10, Rot: 0}
	if got, want := w.Render(), `<0 0 0 20 "out" 10 10 0 "">`; got != want {
		t.Errorf("wire render = %q, want %q", got, want)
	}
}

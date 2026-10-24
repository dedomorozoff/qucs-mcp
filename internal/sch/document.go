// Package sch reads and writes Qucs-S schematic files (format 25.1.1).
//
// Documents hold component and wire lines. Rendering follows the format
// Qucs-S writes itself: one "<TYPE REF 1 X Y LX LY ROT MIRROR body>"
// line per component and one "<X1 Y1 X2 Y2 "label" LX LY ROT "">" per
// wire, inside the fixed section skeleton.
package sch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SchemaVer is the schematic format version this package emits.
const SchemaVer = "25.1.1"

// ComponentLine is a single line inside <Components>.
type ComponentLine struct {
	Type   string // schematic type, e.g. "R", "S4Q_V", "_BJT"
	Ref    string // reference designator, e.g. "R1"
	X, Y   int    // component position on the grid
	LX, LY int    // label offset
	Rot    int    // rotation in 90° steps
	Mirror int    // 1 when mirrored
	// Body is the property sequence verbatim, e.g.
	// `"1k" 1 "26.85" 0 "european" 0`.
	Body string
}

// Render formats the component line for insertion into a .sch file.
func (c ComponentLine) Render() string {
	b := c.Body
	if b != "" {
		b = " " + b
	}
	return fmt.Sprintf("<%s %s 1 %d %d %d %d %d %d%s>",
		c.Type, c.Ref, c.X, c.Y, c.LX, c.LY, c.Rot, c.Mirror, b)
}

// WireLine is a single line inside <Wires>.
type WireLine struct {
	X1, Y1      int
	X2, Y2      int
	Label       string // net label; labels connect nets by name
	LX, LY, Rot int    // label position and orientation
}

// Render formats the wire line.
func (w WireLine) Render() string {
	return fmt.Sprintf("<%d %d %d %d %q %d %d %d %q>",
		w.X1, w.Y1, w.X2, w.Y2, w.Label, w.LX, w.LY, w.Rot, "")
}

// Document accumulates component and wire lines and renders the full
// .sch file skeleton.
type Document struct {
	Name       string
	Components []ComponentLine
	Wires      []WireLine
	// Remarks carry non-fatal notes (e.g. unsupported sweep parameters)
	// surfaced to the MCP caller; they are not written into the file.
	Remarks []string
}

// NewDocument creates an empty named document.
func NewDocument(name string) *Document {
	return &Document{Name: name}
}

// AddComponent appends a component line.
func (d *Document) AddComponent(c ComponentLine) {
	d.Components = append(d.Components, c)
}

// AddWire appends a wire line.
func (d *Document) AddWire(w WireLine) {
	d.Wires = append(d.Wires, w)
}

// SetRemark adds a non-fatal note visible to the MCP caller.
func (d *Document) SetRemark(msg string) {
	d.Remarks = append(d.Remarks, msg)
}

// Render returns the complete .sch text (LF line endings).
func (d *Document) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "<Qucs Schematic %s>\n", SchemaVer)
	fmt.Fprintf(&b, "<Properties>\n")
	fmt.Fprintf(&b, "  <View=0,0,1200,900,1,0,0>\n")
	fmt.Fprintf(&b, "  <Grid=10,10,1>\n")
	fmt.Fprintf(&b, "  <DataSet=%s.dat>\n", d.Name)
	fmt.Fprintf(&b, "  <DataDisplay=%s.dpl>\n", d.Name)
	fmt.Fprintf(&b, "  <OpenDisplay=0>\n")
	fmt.Fprintf(&b, "  <Script=%s.m>\n", d.Name)
	fmt.Fprintf(&b, "  <RunScript=0>\n")
	fmt.Fprintf(&b, "  <showFrame=0>\n")
	fmt.Fprintf(&b, "  <FrameText0=Title>\n")
	fmt.Fprintf(&b, "  <FrameText1=Author:>\n")
	fmt.Fprintf(&b, "  <FrameText2=Date:>\n")
	fmt.Fprintf(&b, "  <FrameText3=Revision:>\n")
	fmt.Fprintf(&b, "</Properties>\n")
	b.WriteString("<Symbol>\n</Symbol>\n")
	b.WriteString("<Components>\n")
	for _, c := range d.Components {
		fmt.Fprintf(&b, "  %s\n", c.Render())
	}
	b.WriteString("</Components>\n")
	b.WriteString("<Wires>\n")
	for _, w := range d.Wires {
		fmt.Fprintf(&b, "  %s\n", w.Render())
	}
	b.WriteString("</Wires>\n")
	b.WriteString("<Diagrams>\n</Diagrams>\n")
	b.WriteString("<Paintings>\n</Paintings>\n")
	return b.String()
}

// WriteTo renders the document and writes it atomically: the file lands
// in a temporary file next to the target, then replaces it.
func (d *Document) WriteTo(path string) error {
	out := []byte(d.Render())
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("tempfile for %s: %w", path, err)
	}
	tname := tmp.Name()
	_, werr := tmp.Write(out)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tname)
		return fmt.Errorf("write %s: %w", path, werr)
	}
	if err := os.Rename(tname, path); err != nil {
		os.Remove(tname)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

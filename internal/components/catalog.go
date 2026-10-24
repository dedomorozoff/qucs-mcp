package components

import (
	"strconv"
	"strings"

	"github.com/lau/qucs-mcp/internal/sch"
)

// Pin is a calibrated pin offset in component-local coordinates
// (dx, dy), with the pin name in netlist order.
type Pin struct {
	Name string // "1", "2", "A", "C", "B", "E", "G", ...
	DX   int
	DY   int
}

// ---------------------------------------------------------------- helpers

// props builds a .sch property body from (value, shown) pairs assembled
// as value, showFlag. Call Props("1k", 1, "26.85", 0).
func Props(pairs ...any) string {
	var b strings.Builder
	for i, v := range pairs {
		if i > 0 {
			b.WriteByte(' ')
		}
		if i%2 == 0 {
			b.WriteString(`"` + v.(string) + `"`)
		} else {
			b.WriteString(strconv.Itoa(v.(int)))
		}
	}
	return b.String()
}

func body(pairs ...any) string { return Props(pairs...) }

// verticalRot helper removed; orientation decided by builder functions.

// ---------------------------------------------------------------- R / C / L

// R builds a resistor line. vertical=true uses Rot=3 so the rotated
// pin order becomes 1=(0,-30) top and 2=(0,30) bottom.
func R(ref string, x, y int, value string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "R", Ref: ref, X: x, Y: y,
		LX: -26, LY: 15, Rot: 0, Mirror: 0,
		Body: body(value, 1),
	}
	if vertical {
		c.Rot = 3
	}
	return c
}

func C(ref string, x, y int, value string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "C", Ref: ref, X: x, Y: y,
		LX: -26, LY: 17, Rot: 0, Mirror: 0,
		Body: body(value, 1),
	}
	if vertical {
		c.Rot = 3
	}
	return c
}

func L(ref string, x, y int, value string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "L", Ref: ref, X: x, Y: y,
		LX: -26, LY: 10, Rot: 0, Mirror: 0,
		Body: body(value, 1),
	}
	if vertical {
		c.Rot = 3
	}
	return c
}

// ------------------------------------------------------------ S4Q_V/S4Q_I

// S4QV builds a SPICE-like independent voltage source. spec is the raw
// regression SPICE value string ("DC 1", "DC 0 AC 1", "PULSE(...)", ...).
//
// Mirror=0 pins: 1=(+30,0) (right), 2=(-30,0) (left).
// Mirror=1 (vertical) pins: 1=(0,-30) (top), 2=(0,30) (bottom).
func S4QV(ref string, x, y int, spec string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "S4Q_V", Ref: ref, X: x, Y: y,
		LX: 18, LY: -26, Rot: 0, Mirror: 0,
		Body: body(spec, 1, "", 0, "", 0, "", 0, "", 0),
	}
	if vertical {
		c.Mirror = 1
	}
	return c
}

// S4QI builds a SPICE-like independent current source with the same pin
// geometry as S4QV.
func S4QI(ref string, x, y int, spec string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "S4Q_I", Ref: ref, X: x, Y: y,
		LX: 18, LY: -26, Rot: 0, Mirror: 0,
		Body: body(spec, 1, "", 0, "", 0, "", 0, "", 0),
	}
	if vertical {
		c.Mirror = 1
	}
	return c
}

// -------------------------------------------------------------------- GND

// GND builds a ground symbol at (x, y). Its single pin is (0,0).
func GND(x, y int) sch.ComponentLine {
	return sch.ComponentLine{Type: "GND", Ref: "*", X: x, Y: y}
}

// ---------------------------------------------------------- semiconductors

// Diode builds a DIODE_SPICE with a named model from the .sch .model
// context; model "" means the schematic's default diode model.
func Diode(ref string, x, y int, model string, vertical bool) sch.ComponentLine {
	c := sch.ComponentLine{
		Type: "DIODE_SPICE", Ref: ref, X: x, Y: y,
		LX: -26, LY: 15, Rot: 0, Mirror: 0,
		Body: body(model, 1, "", 0, "", 0, "", 0, "", 0, "2", 1, "D", 1),
	}
	if vertical {
		c.Rot = 1
	}
	return c
}

// bjtBody returns the full default _BJT property body calibrated from
// qucs-s --list-entries; pos 0 is the npn/pnp selector.
func bjtBody(pnp bool) string {
	// Values and ordering follow the qucs-s 25/26 _BJT default template.
	return body(
		typ(pnp, "npn", "pnp"), 1,
		"1e-16", 1, "1", 1, "1", 0, "0", 0, "0", 0, "0", 1,
		"0", 0, "0", 0, "1.5", 0, "0", 0, "2", 0,
		"100", 1, // Bf
		"1", 0, "0", 0, "0", 0, "0", 0, "0", 0, "0", 0, "0", 0,
		"0.75", 0, "0.33", 0, "0", 0, "0.75", 0, "0.33", 0,
		"1.0", 0, "0", 0, "0.75", 0, "0", 0, "0.5", 0,
		"0.0", 0, "0.0", 0, "0.0", 0, "0.0", 0, "0.0", 0, "0.0", 0,
		"26.85", 0, "0.0", 0, "1.0", 0, "1.0", 0, "0.0", 0,
		"1.0", 0, "1.0", 0, "0.0", 0, "0.0", 0, "3.0", 0, "1.11", 0,
		"26.85", 0, "1.0", 0, "yes", 0, "Generic", 0, "Generic", 0,
	)
}

func BJT(ref string, x, y int, pnp bool) sch.ComponentLine {
	return sch.ComponentLine{
		Type: "_BJT", Ref: ref, X: x, Y: y,
		LX: 8, LY: -26, Rot: 0, Mirror: 0,
		Body: bjtBody(pnp),
	}
}

func MOSFET(ref string, x, y int, pchannel bool) sch.ComponentLine {
	body := body(
		typ(pchannel, "nfet", "pfet"), 0,
		"1.0 V", 1, "2e-5", 1, "0.0", 0, "0.6 V", 0, "0.0", 1,
		"0.0 Ohm", 0, "0.0 Ohm", 0, "0.0 Ohm", 0, "1e-14 A", 0,
		"1.0", 0, "1 um", 0, "1 um", 0, "0.0", 0, "0.1 um", 0,
		"0.0", 0, "0.0", 0, "0.0 F", 0, "0.0 F", 0, "0.8 V", 0,
		"0.5", 0, "0.5", 0, "0.0", 0, "0.33", 0, "0.0 ps", 0,
		"0.0", 0, "0.0", 0, "1", 0, "600.0", 0, "0.0", 0, "1", 0, "1", 0,
		"0.0", 0, "0.0", 0, "0.0", 0, "0.0", 0, "0.0 m", 0, "0.0 m", 0,
		"0.0", 0, "1.0", 0, "1.0", 0, "26.85", 0, "26.85", 0,
		"yes", 0, "Generic", 0, "Generic", 0,
	)
	return sch.ComponentLine{
		Type: "_MOSFET", Ref: ref, X: x, Y: y,
		LX: 8, LY: -26, Rot: 0, Mirror: 0,
		Body: body,
	}
}

func typ(cond bool, t, f string) string {
	if cond {
		return f
	}
	return t
}

// -------------------------------------------------------- spice directives

// AC builds a .AC simulation block. freqType is "lin" or "log".
func AC(ref string, x, y int, freqType, start, stop string, points int) sch.ComponentLine {
	return sch.ComponentLine{
		Type: ".AC", Ref: ref, X: x, Y: y,
		LX: 0, LY: 70, Rot: 0, Mirror: 0,
		Body: body(freqType, 1, start, 1, stop, 1, strconv.Itoa(points), 1, "no", 0),
	}
}

// TR builds a .TRAN simulation block. start/stop/points follow Qucs
// conventions; maxStep is the suggested maximum step.
func TR(ref string, x, y int, start, stop string, points int, maxStep string) sch.ComponentLine {
	return sch.ComponentLine{
		Type: ".TR", Ref: ref, X: x, Y: y,
		LX: 0, LY: 70, Rot: 0, Mirror: 0,
		Body: body(
			"lin", 1, start, 1, stop, 1, strconv.Itoa(points), 0,
			"Trapezoidal", 0, "2", 0, maxStep, 0, "1e-16", 0,
			"150", 0, "0.001", 0, "1 pA", 0, "1 uV", 0, "26.85", 0,
			"1e-3", 0, "1e-6", 0, "1", 0, "CroutLU", 0,
			"no", 0, "yes", 0, "0", 0,
		),
	}
}

// DCOrOp builds a .DC block which in spice mode acts as an .OP.
func DCOrOp(ref string, x, y int) sch.ComponentLine {
	return sch.ComponentLine{
		Type: ".DC", Ref: ref, X: x, Y: y,
		LX: 0, LY: 70, Rot: 0, Mirror: 0,
		Body: body("26.85", 0, "0.001", 0, "1 pA", 0, "1 uV", 0,
			"no", 0, "150", 0, "no", 0, "none", 0, "CroutLU", 0),
	}
}

// SpicePar builds a parameter block (.param equivalent).
func SpicePar(ref string, x, y int, params map[string]string) sch.ComponentLine {
	pairs := make([]any, 0, len(params)*2)
	for k, v := range params { // deterministic order
		pairs = append(pairs, k+"="+v, 1)
	}
	return sch.ComponentLine{
		Type: "SpicePar", Ref: ref, X: x, Y: y,
		LX: -27, LY: 16, Rot: 0, Mirror: 0,
		Body: body(pairs...),
	}
}

// SpiceInclude builds a file include block (.include equivalent).
func SpiceInclude(ref string, x, y int, files ...string) sch.ComponentLine {
	pairs := make([]any, 0, 10)
	for i := range 5 {
		want := 1
		if i >= len(files) {
			pairs = append(pairs, "", 0)
			want = 0
			continue
		}
		pairs = append(pairs, files[i], want)
	}
	return sch.ComponentLine{
		Type: "SpiceInclude", Ref: ref, X: x, Y: y,
		LX: -34, LY: 16, Rot: 0, Mirror: 0,
		Body: body(pairs...),
	}
}

// ------------------------------------------------------------ pin table

// PinsOf returns the calibrated offsets for a catalog type in the given
// orientation. rotate(x, y) = (y, -x) for rot=1 (two-terminal vertical).
func PinsOf(schType string, vertical bool) []Pin {
	var base []Pin
	switch schType {
	case "R", "C", "L", "DIODE_SPICE":
		base = twoPin()
	case "S4Q_V", "S4Q_I":
		// empirically calibrated: pin1=+30 (QuCS "+" terminal),
		// pin2=-30; mirror=1 flips to vertical with pin1 on top.
		base = []Pin{{Name: "1", DX: 30, DY: 0}, {Name: "2", DX: -30, DY: 0}}
	case "_BJT":
		base = []Pin{{Name: "C", DX: 0, DY: -30}, {Name: "B", DX: -30, DY: 0}, {Name: "E", DX: 0, DY: 30}}
	case "_MOSFET":
		base = []Pin{{Name: "D", DX: 0, DY: -30}, {Name: "G", DX: -30, DY: 0}, {Name: "S", DX: 0, DY: 30}}
	case "GND":
		base = []Pin{{Name: "gnd", DX: 0, DY: 0}}
	default:
		return nil
	}
	if !vertical || len(base) < 2 {
		return base
	}
	switch schType {
	case "S4Q_V", "S4Q_I":
		// empirically calibrated on this install: mirror=1 turns the
		// horizontal +30/-30 pins into pin1 top / pin2 bottom.
		return []Pin{{Name: "1", DX: 0, DY: -30}, {Name: "2", DX: 0, DY: 30}}
	default:
		// rot=3 passives/diodes: pin1 lands on top (0,-30).
	}
	out := make([]Pin, len(base))
	for i, p := range base {
		// rotate (x,y) -> (-y, x): (-30,0) -> (0,-30).
		out[i] = Pin{Name: p.Name, DX: -p.DY, DY: p.DX}
	}
	return out
}

func twoPin() []Pin { return []Pin{{Name: "1", DX: -30, DY: 0}, {Name: "2", DX: 30, DY: 0}} }

// Supported reports whether schType is in the catalog.
func Supported(schType string) bool {
	switch schType {
	case "R", "C", "L", "S4Q_V", "S4Q_I", "GND", "DIODE_SPICE", "_BJT", "_MOSFET",
		".AC", ".TR", ".DC", "SpicePar", "SpiceInclude":
		return true
	}
	return false
}

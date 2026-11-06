// Package layout turns a parsed model.CircuitSpec into a schematic
// document. Connectivity is carried entirely by wire net labels, so the
// geometric arrangement never changes the electrical result (design §9).
//
// Calibrated facts baked into this package:
//   - a wire connects to a pin only when a wire ENDPOINT sits exactly on
//     the pin point (passing through is not enough);
//   - identical labels on physically separate wires merge into one net;
//   - unlabeled wires into a GND symbol create SPICE node "0";
//   - .AC mapping: Qucs "log" Points = round(points_per_decade * decades)+1,
//     Qucs "lin" Points = total point count;
//   - .TR mapping: step is recomputed by spice4qucs as
//     (stop-start)/(Points-1), so Points = round((stop-start)/step)+1.
package layout

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/dedomorozoff/qucs-mcp/internal/components"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

const (
	gridStep  = 10
	pinStubs  = 30 // stub length beyond the pin point
	gndOffset = 30
)

// Row Y positions per device group; all multiples of 10.
const (
	ySources = 100
	yPassive = 220
	ySemi    = 360
	yAnalys  = 560
	yDirect  = 660
	yModels  = 740
	yInclude = 820
)

// Build renders spec into a schematic document.
func Build(spec *model.CircuitSpec) (*sch.Document, error) {
	return BuildPlaced(spec, nil)
}

// Placements overrides automatic component positions: ref → (X, Y).
// Orientation (Rot/Mirror) stays layout-controlled, because pin stub
// wires are generated from the catalog geometry.
type Placements map[string][2]int

// BuildPlaced renders spec with explicit component positions for refs
// found in pl (used to preserve user-arranged positions when
// regenerating an existing schematic).
func BuildPlaced(spec *model.CircuitSpec, pl Placements) (*sch.Document, error) {
	doc := sch.NewDocument(spec.Name)

	cursorTwo := 0
	cursorSemi := 0

	for i := range spec.Components {
		c := &spec.Components[i]
		var line sch.ComponentLine
		switch c.Type {
		case "S4Q_V":
			cursorTwo++
			x, y := xAt(cursorTwo), ySources
			if pl != nil {
				if p, ok := pl[c.Ref]; ok {
					x, y = p[0], p[1]
				}
			}
			line = components.S4QV(c.Ref, x, y, c.Value, false)
		case "S4Q_I":
			cursorTwo++
			x, y := xAt(cursorTwo), ySources
			if pl != nil {
				if p, ok := pl[c.Ref]; ok {
					x, y = p[0], p[1]
				}
			}
			line = components.S4QI(c.Ref, x, y, c.Value, false)
		case "R", "C", "L", "DIODE_SPICE":
			cursorTwo++
			x, y := xAt(cursorTwo), yPassive
			if pl != nil {
				if p, ok := pl[c.Ref]; ok {
					x, y = p[0], p[1]
				}
			}
			switch c.Type {
			case "R":
				line = components.R(c.Ref, x, y, c.Value, false)
			case "C":
				line = components.C(c.Ref, x, y, c.Value, false)
			case "L":
				line = components.L(c.Ref, x, y, c.Value, false)
			case "DIODE_SPICE":
				line = components.Diode(c.Ref, x, y, c.Value, false)
			}
		case "_BJT", "_MOSFET":
			cursorSemi++
			x, y := xAt(cursorSemi), ySemi
			if pl != nil {
				if p, ok := pl[c.Ref]; ok {
					x, y = p[0], p[1]
				}
			}
			if c.Type == "_BJT" {
				line = components.BJT(c.Ref, x, y, c.Props["flavor"] == "pnp")
			} else {
				line = components.MOSFET(c.Ref, x, y, c.Props["flavor"] == "pmos")
			}
		case "SpicePar", "SpiceInclude", "SpiceModel", "GND":
			continue // emitted separately below
		default:
			return nil, fmt.Errorf("unsupported component type %q (ref %s)", c.Type, c.Ref)
		}
		applyModelParams(doc, &line, c)
		doc.AddComponent(line)
		addPinStubs(doc, c, line.X, line.Y)
	}

	if err := addAnalyses(doc, spec); err != nil {
		return nil, err
	}
	if err := addDirectives(doc, spec); err != nil {
		return nil, err
	}
	return doc, nil
}

func xAt(n int) int { return 100 + 160*n }

// addPinStubs wires every pin of a device placed at (x, y): non-ground
// nets get a labeled stub, ground pins get an unlabeled wire into a GND
// symbol (which produces SPICE node "0").
func addPinStubs(doc *sch.Document, c *model.Component, x, y int) {
	for _, p := range components.PinsOf(c.Type, false) {
		net := c.NetOf(p.Name)
		px, py := x+p.DX, y+p.DY
		if net == "0" {
			var gx, gy int
			switch {
			case p.DY > 0: // bottom pin: ground below
				gx, gy = px, py+gndOffset
			case p.DY < 0: // top pin: ground above
				gx, gy = px, py-gndOffset
			default: // side pin: ground below
				gx, gy = px, py+gndOffset
			}
			doc.AddWire(sch.WireLine{X1: px, Y1: py, X2: gx, Y2: gy, LX: gx - 10, LY: gy - 10})
			doc.AddComponent(components.GND(gx, gy))
			continue
		}
		sx, sy := 0, 0
		if p.DX != 0 {
			sx = sign(p.DX)
		}
		if p.DY != 0 {
			sy = sign(p.DY)
		}
		tx, ty := px+sx*pinStubs, py+sy*pinStubs
		doc.AddWire(sch.WireLine{
			X1: px, Y1: py, X2: tx, Y2: ty,
			Label: net, LX: tx + 5, LY: ty - 10,
		})
	}
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	}
	return 0
}

// applyModelParams rewrites a transistor property body from the SPICE
// model card and instance parameters. qucs-s does not bind _BJT/_MOSFET
// to a named .model: it generates QMOD_<ref>/MMOD_<ref> from the body, so
// without this the device would silently run on built-in defaults.
func applyModelParams(doc *sch.Document, line *sch.ComponentLine, c *model.Component) {
	modelSlots, instanceSlots := components.ModelSlotTable(c.Type)
	if modelSlots == nil && instanceSlots == nil {
		return
	}
	if len(c.ModelParams) == 0 {
		return
	}
	values := make(map[string]string, len(c.ModelParams))
	for k, v := range c.ModelParams {
		values[k] = neutralNumber(v)
	}
	var ignored []string
	line.Body, ignored = components.ApplyParams(line.Body, modelSlots, instanceSlots, values)
	for _, p := range ignored {
		doc.SetRemark(fmt.Sprintf("%s: parameter %s has no Qucs-S property slot and is dropped", c.Ref, p))
	}
}

// addAnalyses maps parsed analyses to Qucs simulation blocks. All
// analysis blocks share one vertical stack below the circuit.
func addAnalyses(doc *sch.Document, spec *model.CircuitSpec) error {
	stack := 0
	nextY := func() int {
		stack++
		return yAnalys + 80*(stack-1)
	}
	if len(spec.Analyses) == 0 {
		// A schematic without a simulation block cannot be run at all
		// (qucs-s answers "Ngspice timed out or start error!"), so a plain
		// operating point is the safe default for DC-only netlists.
		doc.AddComponent(components.DCOrOp("DC1", 40, nextY()))
		doc.SetRemark("no analysis directive in the netlist; added a DC operating point")
		return nil
	}
	for _, a := range spec.Analyses {
		switch a.Type {
		case "op", "dc":
			doc.AddComponent(components.DCOrOp(fmt.Sprintf("DC%d", stack+1), 40, nextY()))
			if a.Type == "dc" && a.Params["src"] != "" {
				// spice-mode qucs-s has no sweep variables in the .DC
				// block, so the simulation runs a single operating point.
				doc.SetRemark(fmt.Sprintf(".dc sweep of %s is not supported in Qucs-S spice mode: the schematic runs a plain operating point (one point, no sweep)", a.Params["src"]))
			}
		case "ac":
			sweep := strings.ToLower(a.Params["sweep"])
			pts, err := acPoints(sweep, a.Params["points"], a.Params["start"], a.Params["stop"])
			if err != nil {
				return err
			}
			qt := "lin"
			if sweep == "dec" || sweep == "oct" {
				qt = "log"
			}
			// spice4qucs loses the mega suffix ("1M" is emitted as "1m")
			// when translating .AC to ngspice, so start/stop go in the
			// suffix-free scientific form ("1e+06") which passes through
			// verbatim (calibrated: 1e6/1e+06/1000000 all give the exact
			// 10->1e6 sweep, plain "1M" silently truncates to 10..10k).
			start := neutralNumber(a.Params["start"])
			stop := neutralNumber(a.Params["stop"])
			doc.AddComponent(components.AC(fmt.Sprintf("AC%d", stack+1), 40, nextY(),
				qt, start, stop, pts))
		case "tran":
			start := neutralNumber(orzero(a.Params["tstart"]))
			stop := neutralNumber(a.Params["tstop"])
			pts, err := tranPoints(orzero(a.Params["tstart"]), a.Params["tstop"], a.Params["tstep"])
			if err != nil {
				return err
			}
			doc.AddComponent(components.TR(fmt.Sprintf("TR%d", stack+1), 40, nextY(),
				start, stop, pts, "0"))
		}
	}
	return nil
}

// acPoints computes the Qucs .AC Points field. spice4qucs translates
// Qucs log sweeps to ngspice "dec" using points-per-decade =
// (Points-1)/decades, hence Points = N*decades+1 to reproduce SPICE
// ".ac dec N". "lin" passes the count through. SPICE "oct N" is
// approximated as N*log2(10) points per decade (~3.322*N).
func acPoints(sweep, points, start, stop string) (int, error) {
	n, err := strconv.Atoi(points)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf(".ac points %q must be a positive count", points)
	}
	switch sweep {
	case "lin":
		return n, nil
	case "dec":
		s0, ok0 := parseQucsNumber(start)
		s1, ok1 := parseQucsNumber(stop)
		if !ok0 || !ok1 {
			return 0, fmt.Errorf(".ac dec: cannot interpret start/stop %q / %q", start, stop)
		}
		if s0 <= 0 || s1 <= 0 || s1 <= s0 {
			return 0, fmt.Errorf(".ac dec needs 0 < start < stop, got %q..%q", start, stop)
		}
		decades := math.Log10(s1) - math.Log10(s0)
		return int(math.Round(float64(n)*decades)) + 1, nil
	case "oct":
		s0, ok0 := parseQucsNumber(start)
		s1, ok1 := parseQucsNumber(stop)
		if !ok0 || !ok1 {
			return 0, fmt.Errorf(".ac oct: cannot interpret start/stop %q / %q", start, stop)
		}
		if s0 <= 0 || s1 <= 0 || s1 <= s0 {
			return 0, fmt.Errorf(".ac oct needs 0 < start < stop, got %q..%q", start, stop)
		}
		decades := (math.Log10(s1) - math.Log10(s0)) / math.Log10(2)
		return int(math.Round(float64(n)*math.Log2(10)*decades)) + 1, nil
	}
	return 0, fmt.Errorf(".ac sweep %q not supported (lin/dec/oct)", sweep)
}

// tranPoints computes .TR Points so spice4qucs recovers the requested
// step: Points = round((stop-start)/step)+1.
func tranPoints(start, stop, step string) (int, error) {
	s0, ok0 := parseQucsNumber(start)
	s1, ok1 := parseQucsNumber(stop)
	d, okd := parseQucsNumber(step)
	if !ok0 || !ok1 || !okd {
		return 0, fmt.Errorf(".tran: cannot interpret start/stop/step %q %q %q", start, stop, step)
	}
	if d <= 0 || s1 <= s0 {
		return 0, fmt.Errorf(".tran needs stop > start and step > 0, got %v..%v step %q", s0, s1, step)
	}
	pts := int(math.Round((s1-s0)/d)) + 1
	if pts < 2 {
		pts = 2
	}
	return pts, nil
}

// neutralNumber converts a Qucs-suffixed numeric literal to the
// suffix-free scientific form that travels through spice4qucs into
// ngspice unchanged ("1M" → "1e+06"). Unparseable text is returned
// untouched (Build validates numeric fields where they matter).
//
// Values are printed with the shortest round-tripping digits, but the
// binary product of a multiplier introduces noise beyond SPICE precision
// (110u → 0.00010999999999999999), so anything longer than 12 significant
// digits is reprinted at that precision.
func neutralNumber(s string) string {
	if s == "" {
		return s
	}
	v, ok := parseQucsNumber(s)
	if !ok {
		return s
	}
	out := strconv.FormatFloat(v, 'g', -1, 64)
	if sigDigits(out) > 12 {
		out = strconv.FormatFloat(v, 'g', 12, 64)
	}
	return out
}

// sigDigits counts the mantissa digits of a scientific-form literal.
func sigDigits(s string) int {
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		s = s[:i]
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			n++
		}
	}
	return n
}

func orzero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// parseQucsNumber parses Qucs-suffixed numbers: "10", "1M" (mega),
// "100u", "1e-6", "2.5E3". M/m are case sensitive per Qucs semantics;
// trailing letters beyond the multiplier are junk units and ignored.
func parseQucsNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	// exponent if present
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j && j > 0 {
			i = k
		}
	}
	numText := s[:i]
	num, err := strconv.ParseFloat(numText, 64)
	if err != nil {
		return 0, false
	}
	if i >= len(s) {
		return num, true
	}
	switch s[i] {
	case 'T', 't':
		num *= 1e12
	case 'G', 'g':
		num *= 1e9
	case 'M':
		num *= 1e6
	case 'k', 'K':
		num *= 1e3
	case 'm':
		num *= 1e-3
	case 'U', 'u':
		num *= 1e-6
	case 'N', 'n':
		num *= 1e-9
	case 'P', 'p':
		num *= 1e-12
	case 'F', 'f':
		num *= 1e-15
	}
	return num, true
}

// addDirectives renders .param/.include/.model blocks. Parameter names
// are sorted so regenerating a schematic is byte-for-byte stable.
func addDirectives(doc *sch.Document, spec *model.CircuitSpec) error {
	keys := make([]string, 0, len(spec.Params))
	for k := range spec.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		doc.AddComponent(components.SpicePar(fmt.Sprintf("SpicePar%d", i+1), 300, yDirect+60*(i+1),
			map[string]string{k: spec.Params[k]}))
	}
	for i, model := range spec.Models {
		doc.AddComponent(components.SpiceModel(fmt.Sprintf("SpiceModel%d", i+1), 520, yModels+60*(i+1), model))
	}
	if len(spec.Includes) > 0 {
		doc.AddComponent(components.SpiceInclude("SpiceInclude1", 760, yInclude, spec.Includes...))
	}
	return nil
}

// HasAnalysisBlock reports whether the document contains a block of the
// given analysis type ("ac", "dc", "tran").
func HasAnalysisBlock(doc *sch.Document, kind string) (bool, error) {
	prefix := map[string]string{"ac": ".AC", "tran": ".TR", "dc": ".DC"}[kind]
	if prefix == "" {
		return false, fmt.Errorf("unknown analysis kind %q", kind)
	}
	for _, c := range doc.Components {
		if c.Type == prefix {
			return true, nil
		}
	}
	return false, nil
}

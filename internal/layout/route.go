// Pretty router for two-terminal circuits.
//
// The legacy layout wires every pin with a short labeled stub, so a
// schematic reads as scattered fragments. This file draws pure
// two-terminal circuits the way a human would: devices joined into
// left-to-right chains, continuous rails between adjacent pins, shunt
// (grounded) devices hanging vertically below junction slots, and GND
// symbols under the grounded pins. Key properties:
//
//   - the plan is all-or-nothing: if a single device cannot be laid out
//     by the chain walker (bridges, loops, semiconductors, degenerate
//     nets) routePretty leaves the document untouched and returns
//     false, and BuildPlaced falls back to the legacy stub layout;
//   - connectivity is still carried the calibrated way: a wire joins a
//     pin only when an endpoint sits exactly on the pin point, junction
//     drops share their top endpoint with two rail segments, and every
//     rail carries its net label on exactly one segment (identical
//     labels merge nets);
//   - grounded hangers rotate so the rail-side pin is on top; the ROT
//     values come from components.TopPinRot (empirically probed against
//     Qucs-S 26.1.1: passives pin1-top = Rot 3, sources pin1-top =
//     Rot 1, with the other pin landing on the mirrored bottom spot).
package layout

import (
	"sort"

	"github.com/dedomorozoff/qucs-mcp/internal/components"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

// Pretty-layout geometry. Every value keeps each coordinate a multiple
// of the grid step (10), which the round-trip tests assert.
const (
	prettyRailY  = yPassive         // 220: the horizontal chain row
	prettyHangY  = prettyRailY + 80 // anchor row of the vertical shunts
	prettyStartX = 140              // anchor of the very first chain device
	chainLead    = 60               // pin-to-pin rail advance: 2× the 30 pin offset
	junctionPad  = 40               // rail tip → first junction slot
	junctionGap  = 70               // room for one vertical shunt and its value
	stubLen      = 40               // dangling labeled tail
	groupGap     = 120              // spacing between independent chains
	leftMargin   = 60               // nothing draws left of this x
)

// chainDev is one link of the signal chain: the spec component index,
// which spec pins are the entry (left) and exit (right) terminals, and
// whether the link begins a new chain.
type chainDev struct {
	idx   int
	entry int // spec pin index on the left
	exit  int // spec pin index on the right
	start bool
}

// hangDev is a grounded shunt: the rail-side pin is the TOP pin, the
// "0" pin hangs below toward a GND symbol.
type hangDev struct {
	comp int // spec component index
	top  int // spec pin index that must point UP into the rail
}

// twoTermType reports whether a catalog type can join a two-pin chain.
func twoTermType(t string) bool {
	switch t {
	case "R", "C", "L", "DIODE_SPICE", "S4Q_V", "S4Q_I":
		return true
	}
	return false
}

// directiveLike mirrors conn.go: these spec entries render outside the
// device loop, so they neither participate in nor block the pretty
// layout.
func directiveLike(t string) bool {
	switch t {
	case ".AC", ".TR", ".DC", "SpicePar", "SpiceInclude", "SpiceModel":
		return true
	}
	return false
}

// isSource reports the source family: its "+" pin (pin1) is the right
// side of a horizontal body, so the chain enters through pin2.
func isSource(t string) bool { return t == "S4Q_V" || t == "S4Q_I" }

// twoTermEntry returns (entry, exit) spec pin indices: catalog
// horizontals keep pin1 on the left for passives and diodes, while a
// source keeps its "+" (pin1) on the right, so pin2 enters from the
// left. This is purely about placement geometry — every device keeps
// its netlist pin order, which validate.Compare checks verbatim.
func twoTermEntry(t string) (entry, exit int) {
	if isSource(t) {
		return 1, 0
	}
	return 0, 1
}

// planPretty walks the connectivity graph: grounded two-pin devices
// become vertical hangers registered under their rail net; everything
// else is consumed into chains by greedily extending from each unused
// device (sources first, then spec order) through devices whose entry
// pin sits on the current exit net.
//
// ok=false means "this circuit still needs the legacy stub layout" —
// the caller discards everything drawn so far, so a half-pretty
// schematic can never be written.
func planPretty(spec *model.CircuitSpec) (chain []chainDev, hangers map[string][]hangDev, ok bool) {
	comps := spec.Components
	chain = []chainDev{}
	hangers = map[string][]hangDev{}
	used := map[int]bool{}

	for i := range comps {
		c := comps[i]
		if c.Type == "GND" || directiveLike(c.Type) {
			continue
		}
		if !twoTermType(c.Type) || len(c.Pins) != 2 {
			return nil, nil, false // _BJT/_MOSFET or anything unknown
		}
		n0, n1 := c.Pins[0].Net, c.Pins[1].Net
		if n0 == n1 || (n0 == "0" && n1 == "0") {
			// a device whose both ends share one net (self-loop) or
			// both touch ground: neither shape is reachable by the
			// chain walker
			return nil, nil, false
		}
		switch {
		case n0 == "0":
			// grounded left pin: hang with pin2 on top (rail side).
			// Includes a source whose "+" rests on ground (a shunt
			// current source): its "-" still needs the rail.
			hangers[n1] = append(hangers[n1], hangDev{comp: i, top: 1})
			used[i] = true
		case n1 == "0" && !isSource(c.Type):
			// grounded right pin: hang with pin1 on top
			hangers[n0] = append(hangers[n0], hangDev{comp: i, top: 0})
			used[i] = true
		}
		// A source with its "-" (pin2) grounded stays a chain device and
		// leads the chain textbook-style, its ground below the left pin.
	}

	var seeds []int
	for i := range comps {
		if !used[i] && isSource(comps[i].Type) {
			seeds = append(seeds, i)
		}
	}
	for i := range comps {
		if !used[i] && !isSource(comps[i].Type) {
			seeds = append(seeds, i)
		}
	}

	for _, seed := range seeds {
		if used[seed] {
			continue // already consumed as a successor of another chain
		}
		used[seed] = true
		entry, exit := twoTermEntry(comps[seed].Type)
		chain = append(chain, chainDev{idx: seed, entry: entry, exit: exit, start: true})
		for {
			tail := chain[len(chain)-1]
			exitNet := comps[tail.idx].Pins[tail.exit].Net
			found := -1
			for i := range comps {
				if used[i] || comps[i].Type == "GND" || directiveLike(comps[i].Type) {
					continue
				}
				e, _ := twoTermEntry(comps[i].Type)
				// Only the LEFT pin may take an incoming rail: the
				// catalog bodies are fixed (pin1 left for passives,
				// pin2 left for sources) and the netlist pin order is
				// validated verbatim, so a net arriving on the exit
				// side would need a mirrored body the MVP cannot emit.
				if comps[i].Pins[e].Net == exitNet {
					found = i
					break
				}
			}
			if found < 0 {
				break
			}
			used[found] = true
			entry, exit := twoTermEntry(comps[found].Type)
			chain = append(chain, chainDev{idx: found, entry: entry, exit: exit})
		}
	}

	for i := range comps {
		if comps[i].Type == "GND" || directiveLike(comps[i].Type) {
			continue
		}
		if !used[i] {
			return nil, nil, false // loop/bridge the walker cannot express
		}
	}
	return chain, hangers, true
}

// routePretty renders the planned chain into a scratch document and
// copies it into doc only on success, so a false return cannot leave
// partial geometry behind.
func routePretty(doc *sch.Document, spec *model.CircuitSpec) bool {
	chain, hangers, ok := planPretty(spec)
	if !ok {
		return false
	}
	out := sch.NewDocument(spec.Name)

	// maxRight tracks the rightmost x used so far; it spaces
	// independent chains and the leftover-hanger groups apart.
	// rendered holds nets whose hangers are already attached to some
	// rail (each net's shunts must be drawn exactly once).
	maxRight := 0
	rendered := map[string]bool{}

	// pending carries the exit rail from one device to the next: true
	// while the current exit net must flow into the next device's left
	// pin entryX.
	pending := struct {
		active bool
		entryX int
	}{}

	for ci := range chain {
		d := chain[ci]
		c := spec.Components[d.idx]
		entryNet := c.Pins[d.entry].Net
		exitNet := c.Pins[d.exit].Net

		var anchor int
		if !pending.active {
			// Start of a chain: the head rail carries the hangers of
			// the entry net to the left of the first device.
			k := 0
			if entryNet != "0" && !rendered[entryNet] {
				k = len(hangers[entryNet])
			}
			headSpace := 0
			if k > 0 {
				headSpace = junctionPad + junctionGap*(k-1)
			}
			defaultEntry := prettyStartX - 30
			if ci > 0 {
				defaultEntry = maxRight + groupGap - 30
			}
			minLeft := max(leftMargin, maxRight+40)
			entryX := max(defaultEntry, minLeft+headSpace)
			anchor = entryX + 30

			switch {
			case entryNet == "0":
				// A source whose "-" terminal is grounded: the ground
				// sits straight below the entry pin, nothing rail-like.
				drawGroundAt(out, entryX, prettyRailY)
			case k > 0:
				// leftward head rail with junction slots for hangers
				js := headJunctions(entryX, k)
				pts := make([][2]int, 0, len(js)+1)
				for _, x := range js {
					pts = append(pts, [2]int{x, prettyRailY})
				}
				pts = append(pts, [2]int{entryX, prettyRailY})
				drawRail(out, pts, entryNet)
				renderHangers(out, spec, hangers[entryNet], js)
				rendered[entryNet] = true
			default:
				// floating head: a short labeled tail pointing left
				drawStub(out, entryX-stubLen, prettyRailY, entryX, prettyRailY, entryNet)
				rendered[entryNet] = true
			}
		} else {
			// Rail continuing from the previous exit; its junction
			// slots (if any) were drawn with that rail.
			anchor = pending.entryX + 30
		}

		// The device body itself, horizontal, catalog horizontals.
		out.AddComponent(twoPinLine(c.Type, c.Ref, anchor, prettyRailY, c.Value))
		exitX := anchor + 30

		// onward: into the next device, into shunts, or nowhere.
		haveNext := ci+1 < len(chain) && !chain[ci+1].start
		switch {
		case exitNet == "0":
			// Unreachable by construction (grounded-exit devices are
			// hangers), kept as a defensive shape.
			drawGroundAt(out, exitX, prettyRailY)
			maxRight = max(maxRight, exitX+gndOffset)
			pending.active = false
		case haveNext:
			nc := spec.Components[chain[ci+1].idx]
			ne, _ := twoTermEntry(nc.Type)
			if nc.Pins[ne].Net != exitNet {
				return false // plan invariant; never expected to fire
			}
			k := 0
			if !rendered[exitNet] {
				k = len(hangers[exitNet])
			}
			nextEntry := exitX + chainLead
			var js []int
			if k > 0 {
				js = exitJunctions(exitX, k)
				nextEntry = js[len(js)-1] + chainLead
			}
			drawRail(out, railPoints(exitX, js, nextEntry), exitNet)
			if k > 0 {
				renderHangers(out, spec, hangers[exitNet], js)
				rendered[exitNet] = true
			}
			maxRight = max(maxRight, nextEntry)
			pending.active = true
			pending.entryX = nextEntry
		default:
			// End of the chain: junction rail for the exit shunts, or
			// a labeled stub for a floating exit.
			k := 0
			if !rendered[exitNet] {
				k = len(hangers[exitNet])
			}
			switch {
			case k > 0:
				js := exitJunctions(exitX, k)
				drawRail(out, railPoints(exitX, js, -1), exitNet)
				renderHangers(out, spec, hangers[exitNet], js)
				rendered[exitNet] = true
				maxRight = max(maxRight, js[len(js)-1])
			default:
				sx := exitX + stubLen
				drawStub(out, exitX, prettyRailY, sx, prettyRailY, exitNet)
				rendered[exitNet] = true
				maxRight = max(maxRight, sx)
			}
			pending.active = false
		}
	}

	// Hangers on nets no chain rail ever carried (e.g. a net built only
	// of grounded shunts): each gets its own labeled stub above.
	for _, net := range sortedNets(spec) {
		if net == "0" || net == "" || rendered[net] {
			continue
		}
		hs := hangers[net]
		if len(hs) == 0 {
			continue
		}
		base := maxRight + groupGap
		xs := make([]int, len(hs))
		for i := range xs {
			xs[i] = base + i*junctionGap
		}
		for i, h := range hs {
			addHang(out, spec, h, xs[i], net)
		}
		rendered[net] = true
		maxRight = base + junctionGap*(len(hs)-1)
	}

	for _, c := range out.Components {
		doc.AddComponent(c)
	}
	for _, w := range out.Wires {
		doc.AddWire(w)
	}
	return true
}

// twoPinLine builds the horizontal .sch line of a chain device; the
// value text travels into the body verbatim like the legacy layout.
func twoPinLine(t, ref string, x, y int, value string) sch.ComponentLine {
	switch t {
	case "R":
		return components.R(ref, x, y, value, false)
	case "C":
		return components.C(ref, x, y, value, false)
	case "L":
		return components.L(ref, x, y, value, false)
	case "DIODE_SPICE":
		return components.Diode(ref, x, y, value, false)
	case "S4Q_V":
		return components.S4QV(ref, x, y, value, false)
	case "S4Q_I":
		return components.S4QI(ref, x, y, value, false)
	}
	panic("twoPinLine: not a chainable type " + t)
}

// addHang draws one vertical shunt at junction x: the drop from the
// rail to the top pin (carrying the net label only for rail-less
// groups), the device rotated so the rail-side pin points up, and the
// bottom pin wired into a fresh GND symbol.
func addHang(doc *sch.Document, spec *model.CircuitSpec, h hangDev, x int, label string) {
	c := spec.Components[h.comp]
	line := twoPinLine(c.Type, c.Ref, x, prettyHangY, c.Value)
	if rot, ok := components.TopPinRot(c.Type, h.top); ok {
		line.Rot = rot // plan guarantees two-terminal hangers only
	}
	doc.AddWire(sch.WireLine{
		X1: x, Y1: prettyRailY, X2: x, Y2: prettyHangY - 30,
		Label: label, LX: x + 5, LY: prettyRailY - 10,
	})
	doc.AddComponent(line)
	drawGroundAt(doc, x, prettyHangY+30)
}

// renderHangers attaches the rail-side drops of one net's shunts to
// their junction slots (the drops are physically continuous with the
// rail segments ending exactly at each junction).
func renderHangers(doc *sch.Document, spec *model.CircuitSpec, hs []hangDev, js []int) {
	for i, h := range hs {
		addHang(doc, spec, h, js[i], "")
	}
}

// drawRail connects consecutive points with one wire per segment,
// labeling the first segment with the net name; one label is enough
// because the segments share their endpoints.
func drawRail(doc *sch.Document, pts [][2]int, net string) {
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		w := sch.WireLine{X1: a[0], Y1: a[1], X2: b[0], Y2: b[1]}
		if i == 0 {
			w.Label = net
			w.LX = (a[0] + b[0]) / 2
			w.LY = a[1] - 10
		}
		doc.AddWire(w)
	}
}

// drawStub draws a standalone labeled tail between two points.
func drawStub(doc *sch.Document, x1, y1, x2, y2 int, net string) {
	doc.AddWire(sch.WireLine{
		X1: x1, Y1: y1, X2: x2, Y2: y2,
		Label: net, LX: (x1 + x2) / 2, LY: min(y1, y2) - 10,
	})
}

// drawGroundAt wires a pin point straight down into a fresh GND
// symbol, which produces SPICE node "0" (legacy calibrated fact).
func drawGroundAt(doc *sch.Document, x, y int) {
	doc.AddWire(sch.WireLine{X1: x, Y1: y, X2: x, Y2: y + gndOffset, LX: x - 10, LY: y + gndOffset - 10})
	doc.AddComponent(components.GND(x, y+gndOffset))
}

// exitJunctions places k junction slots to the right of the rail tip.
func exitJunctions(tipX, k int) []int {
	xs := make([]int, k)
	x := tipX + junctionPad
	for i := range xs {
		xs[i] = x
		x += junctionGap
	}
	return xs
}

// headJunctions places k junction slots to the left of the entry pin,
// with the rightmost slot nearest the pin.
func headJunctions(entryX, k int) []int {
	xs := make([]int, k)
	x := entryX - junctionPad
	for i := k - 1; i >= 0; i-- {
		xs[i] = x
		x -= junctionGap
	}
	return xs
}

// railPoints assembles the point list of one rail: from the tip through
// every junction to endX; endX < 0 leaves the rail ending at the last
// junction (a tail rail with nothing beyond it).
func railPoints(tipX int, js []int, endX int) [][2]int {
	pts := make([][2]int, 0, len(js)+2)
	pts = append(pts, [2]int{tipX, prettyRailY})
	for _, x := range js {
		pts = append(pts, [2]int{x, prettyRailY})
	}
	if endX >= 0 {
		pts = append(pts, [2]int{endX, prettyRailY})
	}
	return pts
}

// sortedNets lists the distinct pin-net names deterministically.
func sortedNets(spec *model.CircuitSpec) []string {
	set := map[string]bool{}
	for i := range spec.Components {
		for _, p := range spec.Components[i].Pins {
			set[p.Net] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

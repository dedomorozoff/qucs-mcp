// Pretty router for two-terminal circuits plus single-transistor
// stages.
//
// The legacy layout wires every pin with a short labeled stub, so a
// schematic reads as scattered fragments. This file draws circuits the
// way a human would: devices joined into left-to-right chains,
// continuous rails between adjacent pins, shunt (grounded) devices
// hanging vertically below junction slots, and GND symbols under the
// grounded pins. Semiconductors (_BJT/_MOSFET) sit in the chain as
// stages: the base/gate pin takes the incoming rail on the left, the
// collector/drain pin hands the chain on to an exit rail one row above,
// and the emitter/source leg drops to its own grounded shunts. Leftover
// two-terminal devices that join two chain nets (the classic
// collector-load resistor Rc) render as vertical feeders off the exit
// rail, their far pin becoming a labeled stub. Key properties:
//
//   - the plan is all-or-nothing: if a single device cannot be laid out
//     by the chain walker (bridges, loops, degenerate nets) routePretty
//     leaves the document untouched and returns false, and BuildPlaced
//     falls back to the legacy stub layout;
//   - connectivity is still carried the calibrated way: a wire joins a
//     pin only when an endpoint sits exactly on the pin point, junction
//     drops share their top endpoint with two rail segments, and every
//     rail carries its net label on exactly one segment (identical
//     labels merge nets);
//   - vertical devices get their ROT from components.TopPinRot
//     (empirically probed against Qucs-S 26.1.1: passives pin1-top =
//     Rot 3, sources pin1-top = Rot 1, with the other pin landing on
//     the mirrored bottom spot).
package layout

import (
	"sort"

	"github.com/dedomorozoff/qucs-mcp/internal/components"
	"github.com/dedomorozoff/qucs-mcp/internal/model"
	"github.com/dedomorozoff/qucs-mcp/internal/sch"
)

// Pretty-layout geometry. Every value keeps each coordinate a multiple
// of the grid step (10), which the round-trip tests assert. Y values
// are chain-relative: rows shift up by one pin offset (30) when the
// chain passes through a semiconductor stage.
const (
	prettyRailY   = yPassive // 220: the horizontal chain row
	prettyHangOff = 80       // rail row → anchor row of its vertical shunts
	prettyStartX  = 140      // anchor of the very first chain device
	chainLead     = 60       // pin-to-pin rail advance: 2× the 30 pin offset
	junctionPad   = 40       // rail tip → first junction slot
	junctionGap   = 70       // room for one vertical shunt and its value
	stubLen       = 40       // dangling labeled tail
	groupGap      = 120      // spacing between independent chains
	leftMargin    = 60       // nothing draws left of this x
)

// chainDev is one link of the signal chain: the spec component index,
// which spec pins are the entry (left) and exit (right) terminals, and
// whether the link begins a new chain. Stage links are semiconductors:
// the emitter/source pin index travels in emit for the leg below.
type chainDev struct {
	idx   int
	entry int // spec pin index on the left
	exit  int // spec pin index on the exit rail
	emit  int // spec pin index of the emitter/source leg (stage links)
	start bool
	stage bool
}

// hangDev is a grounded shunt: the rail-side pin is the TOP pin, the
// "0" pin hangs below toward a GND symbol.
type hangDev struct {
	comp int // spec component index
	top  int // spec pin index that must point UP into the rail
}

// feederDev is a leftover two-terminal device joining two chain nets
// (the classic collector-load resistor Rc of a one-transistor stage):
// its rail pin points DOWN into a junction slot of some exit rail, the
// other pin points UP into a labeled stub naming that second net.
type feederDev struct {
	comp    int // spec component index
	railPin int // spec pin index attached to the rail (bottom)
}

// layoutPlan is the complete pretty plan; everything in it must be
// renderable, otherwise planPretty refuses the circuit.
type layoutPlan struct {
	chain   []chainDev
	hangers map[string][]hangDev
	feeders []feederDev
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

// stagePinIdx returns (entry, exit, emitter) spec pin indices of a
// three-terminal semiconductor: entry = B/G side pin, exit = C/D top
// pin, emitter = E/S bottom pin. ok=false when the pin set does not
// match one of the recognized three-terminal forms.
func stagePinIdx(c *model.Component) (entry, exit, emit int, ok bool) {
	if (c.Type != "_BJT" && c.Type != "_MOSFET") || len(c.Pins) != 3 {
		return 0, 0, 0, false
	}
	side, top, bottom := "B", "C", "E"
	if c.Type == "_MOSFET" {
		side, top, bottom = "G", "D", "S"
	}
	for i := range c.Pins {
		switch c.Pins[i].Name {
		case side:
			entry = i
		case top:
			exit = i
		case bottom:
			emit = i
		default:
			return 0, 0, 0, false
		}
	}
	return entry, exit, emit, true
}

// planPretty walks the connectivity graph: grounded two-pin devices
// become vertical hangers registered under their rail net; everything
// else is consumed into chains by greedily extending from each unused
// device (sources first, then spec order) through devices whose entry
// pin sits on the current exit net. Leftover two-terminal devices are
// feeders hanging off link exit rails.
//
// ok=false means "this circuit still needs the legacy stub layout" —
// the caller discards everything drawn so far, so a half-pretty
// schematic can never be written.
func planPretty(spec *model.CircuitSpec) (plan layoutPlan, ok bool) {
	comps := spec.Components
	plan = layoutPlan{
		chain:   []chainDev{},
		hangers: map[string][]hangDev{},
		feeders: []feederDev{},
	}
	used := map[int]bool{}

	// Pass 1: classify two-pin devices; stages wait for the walker.
	for i := range comps {
		c := &comps[i]
		if c.Type == "GND" || directiveLike(c.Type) {
			continue
		}
		if _, _, _, sok := stagePinIdx(c); sok {
			continue
		}
		if !twoTermType(c.Type) || len(c.Pins) != 2 {
			return plan, false // unknown device or pin count
		}
		n0, n1 := c.Pins[0].Net, c.Pins[1].Net
		if n0 == n1 || (n0 == "0" && n1 == "0") {
			// a device whose both ends share one net (self-loop) or
			// both touch ground: neither shape is reachable by the
			// chain walker
			return plan, false
		}
		switch {
		case n0 == "0":
			// grounded left pin: hang with pin2 on top (rail side).
			// Includes a source whose "+" rests on ground (a shunt
			// current source): its "-" still needs the rail.
			plan.hangers[n1] = append(plan.hangers[n1], hangDev{comp: i, top: 1})
			used[i] = true
		case n1 == "0" && !isSource(c.Type):
			// grounded right pin: hang with pin1 on top
			plan.hangers[n0] = append(plan.hangers[n0], hangDev{comp: i, top: 0})
			used[i] = true
		}
		// A source with its "-" (pin2) grounded stays a chain device and
		// leads the chain textbook-style, its ground below the left pin.
	}

	// Every non-ground stage emitter net needs at least one grounded
	// shunt; a floating emitter leg has no renderable shape.
	for i := range comps {
		_, _, emit, sok := stagePinIdx(&comps[i])
		if !sok {
			continue
		}
		if en := comps[i].Pins[emit].Net; en != "0" && len(plan.hangers[en]) == 0 {
			return plan, false
		}
	}

	// Seeds: sources lead their chains, then the remaining two-pin
	// devices in spec order, then stages whose entry net no rail ever
	// reaches. Feeder candidates (below) stay out of the seeding: they
	// cannot chain (only a left pin takes an incoming rail) and hang
	// off stage exit rails instead.
	stageExits := map[string]bool{}
	for i := range comps {
		_, exit, _, sok := stagePinIdx(&comps[i])
		if sok {
			stageExits[comps[i].Pins[exit].Net] = true
		}
	}
	var seeds []int
	for i := range comps {
		if !used[i] && isSource(comps[i].Type) && !isFeederCandidate(&comps[i], stageExits) {
			seeds = append(seeds, i)
		}
	}
	for i := range comps {
		if isFeederCandidate(&comps[i], stageExits) {
			continue
		}
		if _, _, _, sok := stagePinIdx(&comps[i]); sok {
			continue
		}
		if !used[i] && comps[i].Type != "GND" && !directiveLike(comps[i].Type) {
			seeds = append(seeds, i)
		}
	}
	for i := range comps {
		_, _, _, sok := stagePinIdx(&comps[i])
		if sok && !used[i] {
			seeds = append(seeds, i)
		}
	}

	for _, seed := range seeds {
		if used[seed] {
			continue // already consumed as a successor of another chain
		}
		used[seed] = true
		plan.chain = append(plan.chain, newLink(seed, &comps[seed], true))
		for {
			tail := plan.chain[len(plan.chain)-1]
			exitNet := comps[tail.idx].Pins[tail.exit].Net
			found := -1
			var foundLink chainDev
			for i := range comps {
				if used[i] || comps[i].Type == "GND" || directiveLike(comps[i].Type) {
					continue
				}
				link := newLink(i, &comps[i], false)
				// Only the LEFT pin may take an incoming rail: the
				// catalog bodies are fixed (pin1 left for passives,
				// pin2 left for sources, B/G left for stages) and the
				// netlist pin order is validated verbatim, so a net
				// arriving on the exit side would need a mirrored
				// body the MVP cannot emit.
				if comps[i].Pins[link.entry].Net != exitNet {
					continue
				}
				found, foundLink = i, link
				break
			}
			if found < 0 {
				break
			}
			used[found] = true
			plan.chain = append(plan.chain, foundLink)
		}
	}

	exitNets := linkExitNets(plan.chain, comps)

	// No chain rail may exit into a stage emitter net: that rail would
	// double-draw the emitter leg.
	for _, l := range plan.chain {
		if !l.stage {
			continue
		}
		if en := comps[l.idx].Pins[l.emit].Net; en != "0" && exitNets[en] {
			return plan, false
		}
	}

	// Leftover two-terminal devices: grounded ones are already hangers;
	// the rest must work as feeders — a passive whose exit (right) pin
	// rests on a stage-exit net (the collector-load resistor Rc joins
	// vcc to the collector rail) or equivalently shaped source.
	for i := range comps {
		if used[i] || comps[i].Type == "GND" || directiveLike(comps[i].Type) {
			continue
		}
		if isFeederCandidate(&comps[i], stageExits) {
			_, railPin := twoTermEntry(comps[i].Type)
			plan.feeders = append(plan.feeders, feederDev{comp: i, railPin: railPin})
			used[i] = true
			continue
		}
		return plan, false // loop/bridge the walker cannot express
	}

	// Every feeder's rail pin must rest on a rail some link really
	// draws as its exit.
	for _, f := range plan.feeders {
		if !exitNets[comps[f.comp].Pins[f.railPin].Net] {
			return plan, false // feeder without a renderable exit rail
		}
	}
	return plan, true
}

// isFeederCandidate reports whether a two-terminal device whose exit
// (right) pin rests on a stage-exit net while its entry pin comes from
// elsewhere can be drawn as a vertical feeder off that exit rail.
func isFeederCandidate(c *model.Component, stageExits map[string]bool) bool {
	if isSource(c.Type) || !twoTermType(c.Type) || len(c.Pins) != 2 {
		return false
	}
	return stageExits[c.Pins[1].Net] && !stageExits[c.Pins[0].Net]
}

// newLink builds the chain link of the device at spec index idx; stage
// devices get their entry/exit/emitter from the stage pin map,
// two-terminal devices from the catalog horizontals.
func newLink(idx int, c *model.Component, start bool) chainDev {
	if entry, exit, emit, sok := stagePinIdx(c); sok {
		return chainDev{idx: idx, entry: entry, exit: exit, emit: emit, start: start, stage: true}
	}
	entry, exit := twoTermEntry(c.Type)
	return chainDev{idx: idx, entry: entry, exit: exit, start: start}
}

// linkExitNets collects the exit-pin nets of all chain links.
func linkExitNets(chain []chainDev, comps []model.Component) map[string]bool {
	out := map[string]bool{}
	for _, l := range chain {
		out[comps[l.idx].Pins[l.exit].Net] = true
	}
	return out
}

// routePretty renders the planned chain into a scratch document and
// copies it into doc only on success, so a false return cannot leave
// partial geometry behind.
func routePretty(doc *sch.Document, spec *model.CircuitSpec) bool {
	plan, ok := planPretty(spec)
	if !ok {
		return false
	}
	comps := spec.Components
	out := sch.NewDocument(spec.Name)

	// maxRight tracks the rightmost x used so far; it spaces
	// independent chains apart. rendered holds nets whose shunts are
	// already attached to some rail (each net's shunts must be drawn
	// exactly once); feederDone mirrors that for feeders.
	maxRight := 0
	rendered := map[string]bool{}
	feederDone := map[int]bool{}
	// busAnchors remembers the dangling labeled-stub end points that
	// earlier chains rendered for a net; a later chain's exit on the same
	// net joins to the nearest such point through an overhead bus wire.
	busAnchors := map[string][][2]int{}

	// pending carries the exit rail from one device to the next: true
	// while the current exit net must flow into the next device's left
	// pin entryX.
	pending := struct {
		active bool
		entryX int
	}{}

	row := prettyRailY
	for ci := range plan.chain {
		d := plan.chain[ci]
		if d.start {
			row = prettyRailY // every chain begins on the main row
		}
		c := &comps[d.idx]
		entryNet := c.Pins[d.entry].Net
		exitNet := c.Pins[d.exit].Net

		var anchorX int
		if !pending.active {
			// Start of a chain: the head rail carries the hangers of
			// the entry net to the left of the first device.
			k := 0
			if entryNet != "0" && !rendered[entryNet] {
				k = len(plan.hangers[entryNet])
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
			anchorX = entryX + 30

			switch {
			case entryNet == "0":
				// A source whose "-" terminal is grounded: the ground
				// sits straight below the entry pin, nothing rail-like.
				drawGroundAt(out, entryX, row)
			case k > 0:
				// leftward head rail with junction slots for hangers
				js := headJunctions(entryX, k)
				pts := make([][2]int, 0, len(js)+1)
				for _, x := range js {
					pts = append(pts, [2]int{x, row})
				}
				pts = append(pts, [2]int{entryX, row})
				drawRail(out, pts, entryNet)
				renderHangers(out, spec, plan.hangers[entryNet], js, row, "")
				rendered[entryNet] = true
			default:
				// floating head: a short labeled tail pointing left
				drawStub(out, entryX-stubLen, row, entryX, row, entryNet)
				rendered[entryNet] = true
				busAnchors[entryNet] = append(busAnchors[entryNet], [2]int{entryX - stubLen, row})
			}
		} else {
			// Rail continuing from the previous exit; its junction
			// slots (if any) were drawn with that rail.
			anchorX = pending.entryX + 30
		}

		// The device body itself, horizontal, catalog horizontals; a
		// stage also draws its emitter leg below the body.
		if d.stage {
			line := stageLine(c, anchorX, row)
			applyModelParams(out, &line, c)
			out.AddComponent(line)
			if x := renderEmitLeg(out, spec, plan, d, anchorX, row, rendered); x > maxRight {
				maxRight = x
			}
		} else {
			out.AddComponent(twoPinLine(c.Type, c.Ref, anchorX, row, c.Value))
		}

		// onward: the exit point is one pin past the body, one row up
		// for a stage (collector/drain) or straight right otherwise.
		exitX, exitY := anchorX+30, row
		if d.stage {
			exitX, exitY = anchorX, row-30
		}
		row = exitY

		haveNext := ci+1 < len(plan.chain) && !plan.chain[ci+1].start
		switch {
		case exitNet == "0":
			// Unreachable by construction (grounded-exit devices are
			// hangers), kept as a defensive shape.
			drawGroundAt(out, exitX, exitY)
			maxRight = max(maxRight, exitX+gndOffset)
			pending.active = false
		case haveNext:
			ne := plan.chain[ci+1].entry
			if comps[plan.chain[ci+1].idx].Pins[ne].Net != exitNet {
				return false // plan invariant; never expected to fire
			}
			nextEntry, maxJX := drawExitRail(out, spec, plan, exitNet, exitX, exitY,
				feederDone, rendered, true, busAnchors)
			if nextEntry < 0 {
				return false // plan invariant; never expected to fire
			}
			maxRight = max(maxRight, maxJX, nextEntry)
			connectBus(out, exitNet, exitX, exitY, busAnchors)
			pending.active = true
			pending.entryX = nextEntry
		default:
			// End of the chain: junction rail for shunts and feeders,
			// or a labeled stub for a floating exit.
			k := 0
			if !rendered[exitNet] {
				k = len(plan.hangers[exitNet])
			}
			fs := pendingFeeders(plan, comps, exitNet, feederDone)
			if k == 0 && len(fs) == 0 {
				sx := exitX + stubLen
				if connectBus(out, exitNet, exitX, exitY, busAnchors) {
					// bridged to an earlier chain's dangling stub: the bus
					// wire already reaches it, so no fresh tail stub is
					// drawn
					rendered[exitNet] = true
					maxRight = max(maxRight, sx)
				} else {
					drawStub(out, exitX, exitY, sx, exitY, exitNet)
					rendered[exitNet] = true
					maxRight = max(maxRight, sx)
					busAnchors[exitNet] = append(busAnchors[exitNet], [2]int{sx, exitY})
				}
			} else {
				_, maxJX := drawExitRail(out, spec, plan, exitNet, exitX, exitY,
					feederDone, rendered, false, busAnchors)
				maxRight = max(maxRight, maxJX)
				connectBus(out, exitNet, exitX, exitY, busAnchors)
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
		hs := plan.hangers[net]
		if len(hs) == 0 {
			continue
		}
		base := maxRight + groupGap
		xs := make([]int, len(hs))
		for i := range xs {
			xs[i] = base + i*junctionGap
		}
		for i, h := range hs {
			addHang(out, spec, h, xs[i], prettyRailY, net)
		}
		rendered[net] = true
		maxRight = base + junctionGap*(len(hs)-1)
	}

	copyPretty(out, doc)
	return true
}

// drawExitRail renders one link's exit rail: from the exit pin through
// a junction slot per outstanding shunt and per outstanding feeder.
// Hangers hang down from their slots, feeders rise up. When contin is
// true the rail must reach beyond its last slot (the next chain device
// follows there) and the function returns that x; otherwise it returns
// -1 and the rail simply ends at the last slot. The second result is
// the rightmost x of anything drawn.
func drawExitRail(out *sch.Document, spec *model.CircuitSpec, plan layoutPlan,
	exitNet string, exitX, exitY int, feederDone map[int]bool, rendered map[string]bool, contin bool, busAnchors map[string][][2]int) (int, int) {

	k := 0
	if !rendered[exitNet] {
		k = len(plan.hangers[exitNet])
	}
	fs := pendingFeeders(plan, spec.Components, exitNet, feederDone)
	slots := k + len(fs)
	if slots == 0 {
		if !contin {
			return -1, 0
		}
		// plain rail to the next device, nothing attached
		end := exitX + chainLead
		drawRail(out, railPoints(exitX, nil, end, exitY), exitNet)
		return end, exitX
	}
	js := exitJunctions(exitX, slots)
	var end int
	if contin {
		end = js[len(js)-1] + chainLead
	} else {
		end = -1
	}
	drawRail(out, railPoints(exitX, js, end, exitY), exitNet)
	if k > 0 {
		renderHangers(out, spec, plan.hangers[exitNet], js[:k], exitY, "")
		rendered[exitNet] = true
	}
	maxX := exitX
	if k > 0 {
		maxX = js[k-1]
	}
	if len(fs) > 0 {
		if x := renderFeeders(out, spec, fs, js[k:], exitY, feederDone, busAnchors); x > maxX {
			maxX = x
		}
	}
	return end, maxX
}

// pendingFeeders collects the not-yet-drawn feeders whose rail pin
// rests on the given net.
func pendingFeeders(plan layoutPlan, comps []model.Component, net string, done map[int]bool) []feederDev {
	var fs []feederDev
	for _, f := range plan.feeders {
		if !done[f.comp] && comps[f.comp].Pins[f.railPin].Net == net {
			fs = append(fs, f)
		}
	}
	return fs
}

// copyPretty moves a successful scratch render into the target
// document, remarks included.
func copyPretty(out, doc *sch.Document) {
	for _, c := range out.Components {
		doc.AddComponent(c)
	}
	for _, w := range out.Wires {
		doc.AddWire(w)
	}
	for _, r := range out.Remarks {
		doc.SetRemark(r)
	}
}

// renderEmitLeg draws the emitter/source leg below a stage anchor: a
// straight drop into ground, or a labeled sub-rail carrying the leg's
// shunts. Returns the rightmost x drawn.
func renderEmitLeg(out *sch.Document, spec *model.CircuitSpec, plan layoutPlan,
	d chainDev, anchorX, row int, rendered map[string]bool) int {

	emNet := spec.Components[d.idx].Pins[d.emit].Net
	ey := row + 30
	if emNet == "0" {
		drawGroundAt(out, anchorX, ey)
		return anchorX
	}
	// plan guarantees at least one shunt here and no rail claiming emNet
	hs := plan.hangers[emNet]
	js := exitJunctions(anchorX, len(hs))
	drawRail(out, railPoints(anchorX, js, -1, ey), emNet)
	renderHangers(out, spec, hs, js, ey, "")
	rendered[emNet] = true
	return js[len(js)-1]
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

// stageLine builds the horizontal .sch line of a semiconductor stage.
func stageLine(c *model.Component, x, y int) sch.ComponentLine {
	if c.Type == "_BJT" {
		return components.BJT(c.Ref, x, y, c.Props["flavor"] == "pnp")
	}
	return components.MOSFET(c.Ref, x, y, c.Props["flavor"] == "pmos")
}

// addHang draws one vertical shunt at junction x over rail row railY:
// the drop from the rail to the top pin (carrying the net label only
// for rail-less groups), the device rotated so the rail-side pin
// points up, and the bottom pin wired into a fresh GND symbol.
func addHang(doc *sch.Document, spec *model.CircuitSpec, h hangDev, x, railY int, label string) {
	c := spec.Components[h.comp]
	hy := railY + prettyHangOff
	line := twoPinLine(c.Type, c.Ref, x, hy, c.Value)
	if rot, ok := components.TopPinRot(c.Type, h.top); ok {
		line.Rot = rot // plan guarantees two-terminal hangers only
	}
	doc.AddWire(sch.WireLine{
		X1: x, Y1: railY, X2: x, Y2: hy - 30,
		Label: label, LX: x + 5, LY: railY - 10,
	})
	doc.AddComponent(line)
	drawGroundAt(doc, x, hy+30)
}

// renderHangers attaches the rail-side drops of one net's shunts to
// their junction slots (the drops are physically continuous with the
// rail segments ending exactly at each junction).
func renderHangers(doc *sch.Document, spec *model.CircuitSpec, hs []hangDev, js []int, railY int, label string) {
	for i, h := range hs {
		addHang(doc, spec, h, js[i], railY, label)
	}
}

// renderFeeders places vertical feeder devices above the rail row: the
// body sits one pin above the junction (anchor railY-30), so the rail
// pin rests exactly on the rail slot while the far pin rises at
// railY-60 into a labeled stub naming the second net.
func renderFeeders(doc *sch.Document, spec *model.CircuitSpec, fs []feederDev, js []int, railY int, done map[int]bool, anchors map[string][][2]int) int {
	for i, f := range fs {
		x := js[i]
		c := spec.Components[f.comp]
		top := 1 - f.railPin // the stub pin points UP, the rail pin DOWN
		line := twoPinLine(c.Type, c.Ref, x, railY-30, c.Value)
		if rot, ok := components.TopPinRot(c.Type, top); ok {
			line.Rot = rot
		}
		doc.AddComponent(line)
		stubNet := c.Pins[top].Net
		doc.AddWire(sch.WireLine{
			X1: x, Y1: railY - 60, X2: x, Y2: railY - 60 - stubLen,
			Label: stubNet, LX: x + 5, LY: railY - 60 - 10,
		})
		anchors[stubNet] = append(anchors[stubNet], [2]int{x, railY - 60 - stubLen})
		done[f.comp] = true
	}
	return js[len(js)-1]
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

// connectBus draws the overhead bus that joins a chain exit to a
// dangling labeled stub left by an earlier chain on the same net: the
// wire rises from the exit point to the stub row and runs along it to
// the stub's dangling end. The stub already carries the net label, so
// no label is added here. Returns true when the exit was bridged this
// way and the caller must not draw a fresh floating tail stub.
func connectBus(out *sch.Document, exitNet string, exitX, exitY int, anchors map[string][][2]int) bool {
	var p [2]int
	found := false
	for _, a := range anchors[exitNet] {
		if a[0] >= exitX || a[1] >= exitY {
			continue // bridges only approach from the left, from above
		}
		if !found || a[0] > p[0] {
			p, found = a, true
		}
	}
	if !found {
		return false
	}
	out.AddWire(sch.WireLine{X1: exitX, Y1: exitY, X2: exitX, Y2: p[1]})
	out.AddWire(sch.WireLine{X1: exitX, Y1: p[1], X2: p[0], Y2: p[1]})
	return true
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

// railPoints assembles the point list of one rail at row y: from the
// tip through every junction to endX; endX < 0 leaves the rail ending
// at the last junction (a tail rail with nothing beyond it).
func railPoints(tipX int, js []int, endX, y int) [][2]int {
	pts := make([][2]int, 0, len(js)+2)
	pts = append(pts, [2]int{tipX, y})
	for _, x := range js {
		pts = append(pts, [2]int{x, y})
	}
	if endX >= 0 {
		pts = append(pts, [2]int{endX, y})
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

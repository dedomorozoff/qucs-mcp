package spice

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dedomorozoff/qucs-mcp/internal/model"
)

// Parse parses the supported SPICE subset into a CircuitSpec.
// Errors are returned with line numbers; unsupported devices (X) and
// directives fail with a clear message and the list of supported ones.
// Continuation lines ("+" leading) are merged into the previous
// device/directive before tokenizing, so multi-line source specs and
// .model lines keep their meaning.
func Parse(text string) (*model.CircuitSpec, error) {
	spec := &model.CircuitSpec{Params: map[string]string{}}
	physical := splitLines(text)
	merged := mergeContinuations(physical)
	nets := &netNames{canon: map[string]string{}}

	rawModels := make([]string, 0, 4)
	seenRefs := map[string]struct{}{}

	for _, m := range merged {
		toks := tokenize(m.text)
		if len(toks) == 0 || strings.HasPrefix(m.text, "*") {
			continue
		}
		first := toks[0]
		ln := m.line
		if strings.HasPrefix(first, ".") {
			switch strings.ToLower(first) {
			case ".end":
				// silently ignored
			case ".title", ".options", ".temp", ".global", ".ic":
				spec.Warnings = append(spec.Warnings,
					fmt.Sprintf("line %d: %s accepted but not applied", ln, first))
			case ".model":
				// The whole directive is kept verbatim: qucs-s writes the
				// SpiceModel property straight into the netlist, so the
				// leading ".model" must stay in it.
				rawModels = append(rawModels, strings.TrimSpace(m.text))
			case ".param":
				for _, kv := range toks[1:] {
					k, v, found := strings.Cut(kv, "=")
					if !found {
						return nil, fmt.Errorf("line %d: .param expects KEY=VALUE, got %q", ln, kv)
					}
					spec.Params[k] = v // expressions are NOT normalized
				}
			case ".include", ".lib":
				if len(toks) < 2 {
					return nil, fmt.Errorf("line %d: %s expects a file", ln, first)
				}
				spec.Includes = append(spec.Includes, toks[1])
			case ".op":
				spec.Analyses = append(spec.Analyses, model.Analysis{Type: "op", Params: map[string]string{}})
			case ".dc", ".ac", ".tran":
				a, err := parseAnalysis(first, toks, ln)
				if err != nil {
					return nil, err
				}
				spec.Analyses = append(spec.Analyses, *a)
			default:
				return nil, fmt.Errorf("line %d: unsupported directive %s (supported: .op .dc .ac .tran .model .param .include .end)",
					ln, first)
			}
			continue
		}

		comp, err := parseDevice(toks, ln, nets)
		if err != nil {
			return nil, err
		}
		key := strings.ToUpper(comp.Ref)
		if _, dup := seenRefs[key]; dup {
			return nil, fmt.Errorf("line %d: duplicated device ref %s", ln, comp.Ref)
		}
		seenRefs[key] = struct{}{}
		if w, ok := comp.Props[warnProp]; ok {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf("line %d: %s", ln, w))
			delete(comp.Props, warnProp)
		}
		spec.Components = append(spec.Components, *comp)
	}

	for _, w := range nets.warnings {
		spec.Warnings = append(spec.Warnings, w)
	}
	applyModels(spec, rawModels)
	if spec.Name == "" {
		spec.Name = "schematic"
	}
	return spec, nil
}

// mergedLine is one logical line after continuation merging.
type mergedLine struct {
	text string
	line int // 1-based number of the first physical line
}

func splitLines(text string) []string {
	return strings.Split(text, "\n")
}

// mergeContinuations joins "+"-leading lines onto the preceding logical
// line and drops inline ";" comments (SPICE end-of-line comment marker).
func mergeContinuations(physical []string) []mergedLine {
	res := make([]mergedLine, 0, len(physical))
	var pending *mergedLine
	for i, raw := range physical {
		line := stripComment(strings.TrimRight(raw, "\r"))
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "+") {
			if pending != nil {
				pending.text += " " + strings.TrimSpace(trim[1:])
			}
			continue
		}
		res = append(res, mergedLine{line: i + 1, text: line})
		pending = &res[len(res)-1]
	}
	return res
}

// stripComment removes a ";"-initiated end-of-line comment, honouring
// double-quoted spans.
func stripComment(l string) string {
	inQuote := false
	for i := 0; i < len(l); i++ {
		switch {
		case l[i] == '"':
			inQuote = !inQuote
		case l[i] == ';' && !inQuote:
			return l[:i]
		}
	}
	return l
}

func tokenize(l string) []string {
	return strings.Fields(l)
}

func parseAnalysis(dir string, toks []string, ln int) (*model.Analysis, error) {
	a := &model.Analysis{Params: map[string]string{}}
	rest := toks[1:]
	switch strings.ToLower(dir) {
	case ".dc":
		if len(rest) < 4 {
			return nil, fmt.Errorf("line %d: .dc needs: source start stop step", ln)
		}
		a.Type = "dc"
		a.Params["src"] = rest[0]
		a.Params["vstart"] = NormalizeValue(rest[1])
		a.Params["vstop"] = NormalizeValue(rest[2])
		a.Params["vstep"] = NormalizeValue(rest[3])
	case ".ac":
		if len(rest) < 4 {
			return nil, fmt.Errorf("line %d: .ac needs: sweep points start stop", ln)
		}
		sweep := strings.ToLower(rest[0])
		if sweep != "lin" && sweep != "dec" && sweep != "oct" {
			return nil, fmt.Errorf("line %d: .ac sweep %q not supported (lin/dec/oct)", ln, rest[0])
		}
		pts, err := strconv.Atoi(rest[1])
		if err != nil || pts <= 0 {
			return nil, fmt.Errorf("line %d: .ac points must be a positive count", ln)
		}
		a.Type = "ac"
		a.Params["sweep"] = sweep
		a.Params["points"] = rest[1]
		a.Params["start"] = NormalizeValue(rest[2])
		a.Params["stop"] = NormalizeValue(rest[3])
	case ".tran":
		if len(rest) < 2 {
			return nil, fmt.Errorf("line %d: .tran needs: step stop", ln)
		}
		a.Type = "tran"
		a.Params["tstep"] = NormalizeValue(rest[0])
		a.Params["tstop"] = NormalizeValue(rest[1])
		if len(rest) >= 3 {
			a.Params["tstart"] = NormalizeValue(rest[2])
		}
		if len(rest) >= 4 {
			a.Params["tmax"] = NormalizeValue(rest[3])
		}
	}
	return a, nil
}

// warnProp holds a one-line non-fatal note about a device, moved out of
// the component and into the spec warnings by Parse.
const warnProp = "_warn"

// deviceSpec: device letters and required node counts.
var deviceSpec = map[byte]struct {
	catalog string
	nodes   int
}{
	'R': {"R", 2}, 'C': {"C", 2}, 'L': {"L", 2},
	'V': {"S4Q_V", 2}, 'I': {"S4Q_I", 2}, 'D': {"DIODE_SPICE", 2},
	'Q': {"_BJT", 3}, 'M': {"_MOSFET", 3},
}

func parseDevice(toks []string, ln int, nets *netNames) (*model.Component, error) {
	name := toks[0]
	if len(name) < 2 {
		return nil, fmt.Errorf("line %d: device name too short: %q", ln, name)
	}
	letter := upperByte(name[0])
	if letter == 'X' {
		return nil, fmt.Errorf("line %d: %s: subcircuits (X) are phase 2 and not supported in MVP", ln, name)
	}
	d, ok := deviceSpec[letter]
	if !ok {
		return nil, fmt.Errorf("line %d: unknown device %s (supported: R C L V I D Q M)", ln, name)
	}
	if len(toks) < d.nodes+2 {
		return nil, fmt.Errorf("line %d: %s expects at least %d nodes and a value", ln, name, d.nodes)
	}
	comp := &model.Component{
		Ref:         name,
		Type:        d.catalog,
		Pins:        make([]model.Pin, 0, d.nodes),
		Props:       map[string]string{},
		ModelParams: map[string]string{},
	}
	nodes := make([]string, d.nodes)
	for i, n := range toks[1 : 1+d.nodes] {
		nodes[i] = nets.resolve(n, ln)
	}
	// Model index: SPICE allows a 4th net for M (bulk) and Q (substrate),
	// so the model name may sit one token later. A trailing "=" in the
	// candidate token means it's a KEY=VALUE parameter, not a net.
	modelIdx := 1 + d.nodes
	// 4-terminal MOSFET: drain gate source bulk — qucs-s has no bulk pin
	// in this template, the bulk node is folded into the source.
	if letter == 'M' && len(toks) >= 1+d.nodes+2 && !strings.Contains(toks[modelIdx+1], "=") {
		bulk := toks[modelIdx]
		if nets.resolve(bulk, ln) != nodes[len(nodes)-1] && bulk != "0" {
			comp.Props[warnProp] = "MOSFET " + name + ": bulk net " + bulk +
				" merged into source " + nodes[len(nodes)-1]
		}
		modelIdx++
	}
	// 5-token BJT (4 nets): c b e s — substrate merged into base.
	if letter == 'Q' && len(toks) >= 1+d.nodes+2 && !strings.Contains(toks[modelIdx+1], "=") {
		sub := toks[modelIdx]
		if nets.resolve(sub, ln) != nodes[1] && sub != "0" {
			comp.Props[warnProp] = "BJT " + name + ": substrate net " + sub + " merged into base " + nodes[1]
		}
		modelIdx++
	}
	pinNames := catalogPins(d.catalog)
	for i, n := range nodes {
		comp.Pins = append(comp.Pins, model.Pin{Name: pinNames[i], Net: n})
	}

	switch letter {
	case 'V', 'I':
		// raw SPICE source spec, continuation merged; S4Q_* carries it all.
		comp.Value = strings.TrimSpace(strings.Join(toks[1+d.nodes:], " "))
	case 'R', 'C', 'L':
		comp.Value = NormalizeValue(toks[1+d.nodes])
	case 'D':
		comp.Value = toks[modelIdx] // model name
	case 'Q', 'M':
		comp.Value = toks[modelIdx] // model name; flavor resolved later
	}
	// Instance parameters: trailing KEY=VALUE tokens after the model name
	// (M1 d g s 0 Mmod L=1u W=10u, Q1 c b e QM AREA=2, R1 a b 1k TC=...).
	for _, kv := range toks[modelIdx+1:] {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			continue
		}
		comp.ModelParams[strings.ToUpper(k)] = NormalizeValue(v)
	}
	return comp, nil
}

// catalogPins returns pin names in Qucs netlist order per catalog.
func catalogPins(catalog string) []string {
	switch catalog {
	case "R", "C", "L", "S4Q_V", "S4Q_I", "DIODE_SPICE":
		return []string{"1", "2"}
	case "_BJT":
		return []string{"C", "B", "E"}
	case "_MOSFET":
		return []string{"D", "G", "S"}
	}
	return nil
}

func upperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}

// netNames canonicalizes SPICE node names.
//
// SPICE node names are case insensitive and Qucs-S resolves "gnd"
// (any spelling) to node 0 while leaving the old name in the probe list,
// which kills the simulation; characters outside [A-Za-z0-9_] survive the
// netlist but disappear from the dataset, so both are normalized here and
// reported once.
type netNames struct {
	canon    map[string]string // folded name -> canonical label
	warnings []string
}

func (n *netNames) resolve(raw string, ln int) string {
	if raw == "0" {
		return "0"
	}
	key := strings.ToLower(raw)
	if key == "gnd" {
		return "0"
	}
	if disp, ok := n.canon[key]; ok {
		return disp
	}
	clean := sanitizeNet(raw)
	n.canon[key] = clean
	if clean != raw {
		n.warnings = append(n.warnings, fmt.Sprintf(
			"line %d: net %q renamed to %q (node names keep only letters, digits and underscore)", ln, raw, clean))
	}
	return clean
}

func sanitizeNet(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			b.WriteByte(c)
		} else {
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" {
		out = "n"
	}
	return out
}

// modelCard is one parsed .model directive.
type modelCard struct {
	name   string
	kind   string
	params map[string]string
}

// parseModelCard splits ".model NAME TYPE (params…)" text. The type word
// is optional (`.model M 0 NCH VTO=.7 …`), parameter values may be
// space separated inside parentheses and bare words are flags worth 1.
func parseModelCard(text string) modelCard {
	f := strings.Fields(text)
	c := modelCard{name: "-", params: map[string]string{}}
	if len(f) > 1 {
		c.name = f[1]
	}
	for _, tok := range f[2:] {
		// "D(IS=7n" — the type word is glued to the parameter list.
		if i := strings.IndexByte(tok, '('); i > 0 {
			if head := tok[:i]; isWord(head) && c.kind == "" {
				c.kind = strings.ToUpper(head)
			}
			tok = tok[i+1:]
			if tok == "" {
				continue
			}
		}
		tok = strings.Trim(tok, "()")
		if tok == "" {
			continue
		}
		if k, v, found := strings.Cut(tok, "="); found {
			if k != "" {
				c.params[strings.ToUpper(k)] = NormalizeValue(v)
			}
			continue
		}
		if isWord(tok) {
			if c.kind == "" {
				c.kind = strings.ToUpper(tok)
				continue
			}
			c.params[strings.ToUpper(tok)] = "1" // bare flag
		}
	}
	return c
}

func isWord(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_') {
			return false
		}
	}
	return s != ""
}

// applyModels resolves BJT pnp/npn and MOSFET p/n channel from the
// collected .model lines and carries the card parameters into the
// device, because qucs-s builds the transistor model from the component
// properties, not from a named .model line.
func applyModels(spec *model.CircuitSpec, rawModels []string) {
	spec.Models = rawModels
	cards := map[string]modelCard{}
	for _, m := range rawModels {
		c := parseModelCard(m)
		cards[strings.ToLower(c.name)] = c
	}
	for i := range spec.Components {
		c := &spec.Components[i]
		if c.ModelParams == nil {
			c.ModelParams = map[string]string{}
		}
		switch c.Type {
		case "_BJT", "_MOSFET", "DIODE_SPICE":
			c.Props["model"] = c.Value
			card, found := cards[strings.ToLower(c.Value)]
			if !found {
				if len(spec.Includes) == 0 {
					spec.Warnings = append(spec.Warnings, fmt.Sprintf(
						"%s: model %q is not defined in the netlist text; the device has no model card",
						c.Ref, c.Value))
				}
				continue
			}
			switch c.Type {
			case "_BJT":
				if strings.Contains(card.kind, "PNP") {
					c.Props["flavor"] = "pnp"
				} else {
					c.Props["flavor"] = "npn"
				}
			case "_MOSFET":
				if isPChannel(card) {
					c.Props["flavor"] = "pmos"
				} else {
					c.Props["flavor"] = "nmos"
				}
			default:
				// Diodes are emitted by model name, qucs-s passes the card
				// through, so nothing has to be copied into the component.
				continue
			}
			for k, v := range card.params {
				if _, overridden := c.ModelParams[k]; !overridden {
					c.ModelParams[k] = v
				}
			}
		}
	}
}

// isPChannel reports whether a MOSFET card is p-channel: the type word
// (pmos/pfet) or the level-3 pchan flag.
func isPChannel(card modelCard) bool {
	if strings.Contains(card.kind, "PMOS") || strings.Contains(card.kind, "PFET") {
		return true
	}
	return card.params["PCHAN"] == "1"
}

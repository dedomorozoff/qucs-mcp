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
			case ".title", ".options", ".temp":
				spec.Warnings = append(spec.Warnings,
					fmt.Sprintf("line %d: %s accepted but not applied", ln, first))
			case ".model":
				rawModels = append(rawModels, strings.Join(toks[1:], " "))
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

		comp, err := parseDevice(toks, ln)
		if err != nil {
			return nil, err
		}
		key := strings.ToUpper(comp.Ref)
		if _, dup := seenRefs[key]; dup {
			return nil, fmt.Errorf("line %d: duplicated device ref %s", ln, comp.Ref)
		}
		seenRefs[key] = struct{}{}
		if warn, ok := comp.Props["_bulk_warn"]; ok {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf("line %d: %s", ln, warn))
			delete(comp.Props, "_bulk_warn")
		}
		if warn, ok := comp.Props["_sub_warn"]; ok {
			spec.Warnings = append(spec.Warnings, fmt.Sprintf("line %d: %s", ln, warn))
			delete(comp.Props, "_sub_warn")
		}
		spec.Components = append(spec.Components, *comp)
	}

	applyModelFlavors(spec, rawModels)
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

func mergeContinuations(physical []string) []mergedLine {
	res := make([]mergedLine, 0, len(physical))
	var pending *mergedLine
	for i, raw := range physical {
		line := strings.TrimRight(raw, "\r")
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

// deviceSpec: device letters and required node counts.
var deviceSpec = map[byte]struct {
	catalog string
	nodes   int
}{
	'R': {"R", 2}, 'C': {"C", 2}, 'L': {"L", 2},
	'V': {"S4Q_V", 2}, 'I': {"S4Q_I", 2}, 'D': {"DIODE_SPICE", 2},
	'Q': {"_BJT", 3}, 'M': {"_MOSFET", 3},
}

func parseDevice(toks []string, ln int) (*model.Component, error) {
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
		Ref:   name,
		Type:  d.catalog,
		Pins:  make([]model.Pin, 0, d.nodes),
		Props: map[string]string{},
	}
	nodes := toks[1 : 1+d.nodes]
	// Model index: SPICE allows a 4th net for M (bulk) and Q (substrate),
	// so the model name may sit one token later. A trailing "=" in the
	// candidate token means it's a KEY=VALUE parameter, not a net.
	modelIdx := 1 + d.nodes
	// 4-terminal MOSFET: drain gate source bulk — bulk merges into source
	if letter == 'M' && len(toks) >= 1+d.nodes+2 && !strings.Contains(toks[modelIdx+1], "=") {
		bulk := toks[modelIdx]
		src := nodes[d.nodes-1]
		if bulk != src && bulk != "0" {
			comp.Props["_bulk_warn"] = "MOSFET " + name + ": bulk net " + bulk + " merged into source " + src
		}
		modelIdx++
	}
	// 5-token BJT (4 nets): c b e s — substrate merges into base
	if letter == 'Q' && len(toks) >= 1+d.nodes+2 && !strings.Contains(toks[modelIdx+1], "=") {
		sub := toks[modelIdx]
		base := nodes[1]
		if sub != base && sub != "0" {
			comp.Props["_sub_warn"] = "BJT " + name + ": substrate net " + sub + " merged into base " + base
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

// applyModelFlavors resolves BJT pnp/npn and MOSFET p/n channel from the
// collected raw .model lines.
func applyModelFlavors(spec *model.CircuitSpec, rawModels []string) {
	spec.Models = rawModels
	byName := map[string]string{}
	for _, m := range rawModels {
		fields := strings.Fields(m)
		if len(fields) == 0 {
			continue
		}
		byName[strings.ToLower(fields[0])] = strings.Join(fields[1:], " ")
	}
	for i := range spec.Components {
		c := &spec.Components[i]
		switch c.Type {
		case "_BJT", "_MOSFET":
			upper := strings.ToUpper(byName[strings.ToLower(c.Value)])
			if c.Type == "_BJT" {
				if strings.Contains(upper, "PNP") {
					c.Props["flavor"] = "pnp"
				} else {
					c.Props["flavor"] = "npn"
				}
			} else {
				if strings.Contains(upper, "PMOS") {
					c.Props["flavor"] = "pmos"
				} else {
					c.Props["flavor"] = "nmos"
				}
			}
			c.Props["model"] = c.Value
		case "DIODE_SPICE":
			c.Props["model"] = c.Value
		}
	}
}

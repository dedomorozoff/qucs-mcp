package sch

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadDocument is a parsed schematic. It keeps what matters for the
// MCP tools: component lines with positions/bodies, wire lines with
// labels and the raw tail of unknown sections (design §13). Unknown
// component types are kept too — reading a schematic must not break on
// documents the layout package cannot build itself.
type ReadDocument struct {
	Version        string
	Name           string
	Components     []ComponentLine
	Wires          []WireLine
	ExtraPaintings []string // raw lines inside <Paintings>
}

// Read parses the .sch file at path.
func Read(path string) (*ReadDocument, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseSCH(string(b), fileName(path))
}

func fileName(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return path
	}
	return path[i+1:]
}

// ParseSCH parses .sch text; name is used for the ReadDocument.Name.
func ParseSCH(text, name string) (*ReadDocument, error) {
	d := &ReadDocument{Name: name}
	section := ""
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if section == "" {
			section = openSection(line)
			if section == "header" {
				d.Version = strings.TrimSuffix(strings.TrimPrefix(line, "<Qucs Schematic "), ">")
				section = ""
			}
			continue
		}
		if closesSection(line) {
			section = ""
			continue
		}
		switch section {
		case "Components":
			if c, err := parseComponentLine(line); err == nil {
				d.Components = append(d.Components, *c)
			}
			// unparsable component lines are skipped: reading tolerates
			// unknown content (design §13)
		case "Wires":
			if w, err := parseWireLine(line); err == nil {
				d.Wires = append(d.Wires, *w)
			}
		case "Paintings":
			d.ExtraPaintings = append(d.ExtraPaintings, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

func openSection(line string) string {
	for _, s := range []string{"Components", "Wires", "Diagrams", "Paintings", "Symbol", "Properties"} {
		if line == "<"+s+">" {
			return s
		}
	}
	if strings.HasPrefix(line, "<Qucs Schematic ") {
		return "header"
	}
	return ""
}

func closesSection(line string) bool {
	for _, s := range []string{"Components", "Wires", "Diagrams", "Paintings", "Symbol", "Properties"} {
		if line == "</"+s+">" {
			return true
		}
	}
	return false
}

// nthToken returns the start and end offsets of the n-th (1-based)
// space-separated token of line; ok is false when there are fewer
// tokens. A token that begins with a quote runs until the matching
// closing quote, so labels like `"a b"` count as one token.
func nthToken(line string, n int) (start, end int, ok bool) {
	i := 0
	for n > 0 && i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		start = i
		if i < len(line) && line[i] == '"' {
			i++
			for i < len(line) && line[i] != '"' {
				i++
			}
			if i < len(line) {
				i++ // past the closing quote
			}
		} else {
			for i < len(line) && line[i] != ' ' {
				i++
			}
		}
		if i > start {
			end = i
			n--
		} else {
			break
		}
	}
	if n > 0 {
		return 0, 0, false
	}
	return start, end, true
}

// parseComponentLine parses `<TYPE REF 1 X Y LX LY ROT MIRROR body…>`.
// The leading "1" after REF is Qucs's occurrence counter.
func parseComponentLine(line string) (*ComponentLine, error) {
	if !strings.HasPrefix(line, "<") {
		return nil, fmt.Errorf("not an angle-bracket line: %q", line)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
	fields := strings.Fields(inner)
	// 9 tokens is valid for property-less symbols (GND: `<GND * 1 X Y
	// 0 0 0 0>`); everything after the 9th token is the body.
	if len(fields) < 9 {
		return nil, fmt.Errorf("component line too short: %q", line)
	}
	c := &ComponentLine{Type: fields[0], Ref: fields[1]}
	if fields[2] != "1" {
		return nil, fmt.Errorf("unsupported component line form in %q", line)
	}
	nums := make([]int, 6)
	for j := 0; j < 6; j++ { // X Y LX LY ROT MIRROR = fields 3..8
		v, err := strconv.Atoi(fields[3+j])
		if err != nil {
			return nil, fmt.Errorf("component %s %q: %w", fields[0], fields[3+j], err)
		}
		nums[j] = v
	}
	c.X, c.Y, c.LX, c.LY, c.Rot, c.Mirror = nums[0], nums[1], nums[2], nums[3], nums[4], nums[5]
	// Body: everything past the 9th token, internal whitespace preserved.
	if _, end, ok := nthToken(inner, 9); ok && end < len(inner) {
		c.Body = strings.TrimSpace(inner[end:])
	}
	return c, nil
}

// parseWireLine parses `<X1 Y1 X2 Y2 "label" LX LY ROT "">`.
func parseWireLine(line string) (*WireLine, error) {
	if !strings.HasPrefix(line, "<") {
		return nil, fmt.Errorf("not an angle-bracket line: %q", line)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
	fields := strings.Fields(inner)
	if len(fields) < 8 {
		return nil, fmt.Errorf("wire line too short: %q", line)
	}
	w := &WireLine{}
	vals := make([]int, 4)
	for j := 0; j < 4; j++ { // X1 Y1 X2 Y2
		v, err := strconv.Atoi(fields[j])
		if err != nil {
			return nil, fmt.Errorf("wire %q: %w", line, err)
		}
		vals[j] = v
	}
	w.X1, w.Y1, w.X2, w.Y2 = vals[0], vals[1], vals[2], vals[3]
	// From the 5th token: `"label" LX LY ROT ""` — the label may contain
	// spaces, so it is located as one quoted token.
	start, end, ok := nthToken(inner, 5)
	if !ok || start >= len(inner) || inner[start] != '"' || end-start < 2 {
		return nil, fmt.Errorf("wire label missing: %q", line)
	}
	w.Label = inner[start+1 : end-1]
	rest := strings.Fields(inner[end:]) // LX LY ROT ""
	if len(rest) < 3 {
		return nil, fmt.Errorf("wire label tail missing: %q", line)
	}
	for j, dst := range []*int{&w.LX, &w.LY, &w.Rot} {
		v, err := strconv.Atoi(rest[j])
		if err != nil {
			return nil, fmt.Errorf("wire %q: %w", line, err)
		}
		*dst = v
	}
	return w, nil
}

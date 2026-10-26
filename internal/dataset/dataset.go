// Package dataset parses Qucs-S simulation datasets (.dat files).
//
// Format (calibrated against Qucs-S 26.1.1 output):
//
//	<Qucs Dataset 26.1.1>
//	<indep frequency N>
//	  ... N numeric lines ...
//	</indep>
//	<dep ac.v(out) frequency>
//	  ... N lines, complex as "re+jim" (or bare real) ...
//	</dep>
package dataset

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// Series holds one independent or dependent variable.
type Series struct {
	Name   string // "frequency", "ac.v(out)"
	Indep  bool
	Values []complex128
}

// Dataset is a parsed .dat file.
type Dataset struct {
	Version string
	Vars    []Series
}

// Var returns the series with the given name (case sensitive).
func (d *Dataset) Var(name string) (Series, bool) {
	for _, s := range d.Vars {
		if s.Name == name {
			return s, true
		}
	}
	return Series{}, false
}

// Parse reads a Qucs dataset from text.
func Parse(text string) (*Dataset, error) {
	d := &Dataset{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	first := true // header line
	lineNo := 0   // 1-based line counter
	order := []*Series{}
	blocks := map[string]*Series{}

	var cur *Series

	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			if !strings.HasPrefix(line, "<Qucs Dataset ") {
				return nil, fmt.Errorf("line %d: header is not <Qucs Dataset ...>: %q", lineNo, line)
			}
			d.Version = strings.TrimSuffix(strings.TrimPrefix(line, "<Qucs Dataset "), ">")
			first = false
			continue
		}
		switch {
		case strings.HasPrefix(line, "</indep>"), strings.HasPrefix(line, "</dep>"):
			cur = nil
		case strings.HasPrefix(line, "<indep "):
			name, count, ok := parseBlockHeader(line, "<indep ")
			if !ok {
				return nil, fmt.Errorf("line %d: bad indep header %q", lineNo, line)
			}
			s := &Series{Name: name, Indep: true}
			s.Values = reserve(s.Values, count)
			blocks[name] = s
			order = append(order, s)
			cur = s
		case strings.HasPrefix(line, "<dep "):
			name, indepName, ok := parseDepHeader(line)
			if !ok {
				return nil, fmt.Errorf("line %d: bad dep header %q", lineNo, line)
			}
			s := &Series{Name: name, Indep: false}
			if in, known := blocks[indepName]; known {
				s.Values = reserve(s.Values, len(in.Values))
			}
			blocks[name] = s
			order = append(order, s)
			cur = s
		default:
			if cur == nil {
				return nil, fmt.Errorf("line %d: value %q outside a declared block", lineNo, line)
			}
			z, err := parseValue(line, lineNo)
			if err != nil {
				return nil, err
			}
			cur.Values = append(cur.Values, z)
		}
	}
	if first {
		return nil, fmt.Errorf("empty dataset (missing <Qucs Dataset ...> header)")
	}
	d.Vars = make([]Series, 0, len(order))
	for _, s := range order {
		d.Vars = append(d.Vars, *s)
	}
	return d, nil
}

// parseBlockHeader parses `<indep name count>`.
func parseBlockHeader(line, prefix string) (string, int, bool) {
	inner := strings.TrimPrefix(line, prefix)
	inner = strings.TrimSuffix(strings.TrimPrefix(inner, "<"), ">")
	fields := strings.Fields(inner)
	if len(fields) != 2 {
		return "", 0, false
	}
	n, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", 0, false
	}
	return fields[0], n, true
}

// parseDepHeader parses `<dep name indepname [flag...]>`; the trailing
// formatting flags are ignored.
func parseDepHeader(line string) (string, string, bool) {
	inner := strings.TrimPrefix(line, "<dep")
	inner = strings.TrimSuffix(strings.TrimPrefix(inner, "<"), ">")
	fields := strings.Fields(inner)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// parseValue reads one numeric line: "1.0e0" or "1.0e0+j-2.5e-3".
func parseValue(s string, lineNo int) (complex128, error) {
	re, im := s, "0"
	if idx := strings.Index(s, "+j"); idx >= 0 {
		re, im = s[:idx], s[idx+2:]
	} else if idx := strings.Index(s, "-j"); idx >= 0 {
		re, im = s[:idx], "-"+s[idx+2:]
	}
	r, err := strconv.ParseFloat(strings.TrimSpace(re), 64)
	if err != nil {
		return 0, fmt.Errorf("line %d: bad numeric %q: %w", lineNo, s, err)
	}
	i, err := strconv.ParseFloat(strings.TrimSpace(im), 64)
	if err != nil {
		return 0, fmt.Errorf("line %d: bad imaginary %q: %w", lineNo, s, err)
	}
	return complex(r, i), nil
}

func reserve(v []complex128, n int) []complex128 {
	if n > 0 && n <= 10*1024*1024 {
		v = make([]complex128, 0, n)
	}
	return v
}

// Package server — MCP circuit tools (design §14). Eight tools wrap the
// pipeline SPICE text → schematic → netlist validation → simulation:
//
//	create_schematic   SPICE netlist text → .sch (+ optional validate/simulate)
//	update_schematic   regenerate an existing .sch from new netlist text,
//	                   preserving component positions by ref
//	validate_schematic compare a .sch's ngspice netlist against the model
//	                   (or an expected netlist text)
//	simulate_schematic run the ngspice simulation and summarize the dataset
//	read_schematic     model view of an existing .sch
//	list_components    catalog of supported component types
//	describe_component detailed catalog entry (pins, geometry, example)
//	open_schematic     launch the qucs-s GUI on a file (non-blocking)
package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lau/qucs-mcp/internal/components"
	"github.com/lau/qucs-mcp/internal/conn"
	"github.com/lau/qucs-mcp/internal/dataset"
	"github.com/lau/qucs-mcp/internal/layout"
	"github.com/lau/qucs-mcp/internal/model"
	"github.com/lau/qucs-mcp/internal/netlist"
	"github.com/lau/qucs-mcp/internal/runner"
	"github.com/lau/qucs-mcp/internal/sch"
	"github.com/lau/qucs-mcp/internal/spice"
	"github.com/lau/qucs-mcp/internal/validate"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------- create

type createIn struct {
	Netlist   string `json:"netlist" jsonschema:"SPICE netlist text (elements, ground, .ac/.tran directives)"`
	Name      string `json:"name,omitempty" jsonschema:"Base file name without extension; derived from base_path or defaults to 'circuit'"`
	OutputDir string `json:"output_dir,omitempty" jsonschema:"Directory for the generated .sch file; defaults to the configured work dir"`
	BasePath  string `json:"base_path,omitempty" jsonschema:"Existing .sch to overwrite in place, keeping component positions by ref"`
	Validate  *bool  `json:"validate,omitempty" jsonschema:"Run netlist connectivity validation (default true)"`
	Simulate  *bool  `json:"simulate,omitempty" jsonschema:"Run the simulation after validation (default false)"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"Allow overwriting an existing target file (default false)"`
}

type createOut struct {
	Path        string           `json:"path" jsonschema:"Generated .sch file"`
	NetlistPath string           `json:"netlist_path" jsonschema:"Generated SPICE netlist (.cir)"`
	Valid       bool             `json:"valid" jsonschema:"Validation result"`
	Report      *validate.Report `json:"report,omitempty" jsonschema:"Pin-level connectivity report"`
	Warnings    []string         `json:"warnings,omitempty" jsonschema:"Non-fatal notes (unsupported directives, stray refs)"`
	Simulation  *simOut          `json:"simulation,omitempty" jsonschema:"Simulation summary when simulate=true"`
}

type updateIn struct {
	Path     string `json:"path" jsonschema:"Existing .sch file to regenerate"`
	Netlist  string `json:"netlist" jsonschema:"New SPICE netlist text"`
	Validate *bool  `json:"validate,omitempty" jsonschema:"Run netlist connectivity validation (default true)"`
	Simulate *bool  `json:"simulate,omitempty" jsonschema:"Run the simulation after validation (default false)"`
}

func (s *Server) registerTools() error {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "create_schematic",
		Description: "Generate a Qucs-S schematic (.sch) from SPICE netlist text. Optionally validates pin connectivity via a real ngspice netlist and runs the simulation.",
	}, s.handleCreate)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "update_schematic",
		Description: "Regenerate an existing Qucs-S schematic from new SPICE netlist text, writing it back to the same file and preserving component positions by ref.",
	}, s.handleUpdate)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "validate_schematic",
		Description: "Validate an existing schematic: regenerate the ngspice netlist and compare pin connectivity against the schematic model (or an expected SPICE netlist).",
	}, s.handleValidate)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "simulate_schematic",
		Description: "Run the ngspice simulation for a schematic headlessly and summarize the Qucs dataset (variable names, point counts; full data when include_data=true).",
	}, s.handleSimulate)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "read_schematic",
		Description: "Read a Qucs-S schematic and return the circuit model: components (ref, type, value, position) and labeled wires. Unknown components are reported as warnings, not errors.",
	}, s.handleRead)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "list_components",
		Description: "List the supported Qucs-S component types with their SPICE letters and pins.",
	}, s.handleListComponents)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "describe_component",
		Description: "Describe a supported component type in detail: pins with offsets, orientation rules and a schematic line example.",
	}, s.handleDescribeComponent)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        "open_schematic",
		Description: "Open a schematic in the qucs-s GUI as a separate process (does not block the server).",
	}, s.handleOpen)
	return nil
}

// defaultTrue dereferences an optional flag defaulting to true.
func defaultTrue(p *bool) bool { return p == nil || *p }

// ---------------------------------------------------------------- flow

var nameRe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func sanitizeName(s string) string {
	out := nameRe.ReplaceAllString(strings.TrimSpace(s), "_")
	out = strings.Trim(out, "_")
	if out == "" {
		out = "circuit"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// generate runs the full create/update pipeline. When writePath is ""
// the target is derived from name/outputDir. It always returns the
// outcome even when validation fails (valid=false in the payload);
// hard failures (parse errors, IO) come back as errors.
func (s *Server) generate(ctx context.Context, name, outputDir, basePath, netlistText, writePath string, doValidate, doSimulate, overwrite bool) (createOut, error) {
	spec, err := spice.Parse(netlistText)
	if err != nil {
		return createOut{}, fmt.Errorf("parse netlist: %w", err)
	}

	// Target path and document name.
	base := ""
	dir := ""
	if basePath != "" {
		base = filepath.Base(basePath)
		dir = filepath.Dir(basePath)
		if strings.HasSuffix(strings.ToLower(base), ".sch") {
			base = strings.TrimSuffix(base[:len(base)-3], ".")
		}
	}
	docName := name
	if docName == "" && base != "" {
		docName = base
	}
	if docName == "" {
		docName = "circuit"
	}
	docName = sanitizeName(docName)
	if dir == "" && outputDir != "" {
		dir = outputDir
	}
	if dir == "" {
		dir = s.cfg.WorkDir
	}
	if writePath == "" {
		writePath = filepath.Join(dir, docName+".sch")
	}

	// Keep prior positions for refs still present.
	var placements layout.Placements
	var warnings []string
	if basePath != "" {
		prev, rerr := sch.Read(basePath)
		if rerr != nil {
			return createOut{}, fmt.Errorf("read base_path %s: %w", basePath, rerr)
		}
		placements = layout.Placements{}
		for _, c := range prev.Components {
			if c.Type == "GND" {
				continue
			}
			placements[c.Ref] = [2]int{c.X, c.Y}
			if !components.Supported(c.Type) {
				warnings = append(warnings, fmt.Sprintf(
					"base schematic has unsupported component %s (type %s); it will drop out of the regenerated file", c.Ref, c.Type))
			}
		}
	}

	if !overwrite && (basePath == "" || writePath != basePath) {
		if st, serr := os.Stat(writePath); serr == nil && !st.IsDir() {
			return createOut{}, fmt.Errorf("%s already exists; pass overwrite=true or use update_schematic", writePath)
		}
	}

	spec.Name = docName
	doc, err := layout.BuildPlaced(spec, placements)
	if err != nil {
		return createOut{}, err
	}
	warnings = append(warnings, spec.Warnings...)
	warnings = append(warnings, doc.Remarks...)
	if err := doc.WriteTo(writePath); err != nil {
		return createOut{}, err
	}

	out := createOut{
		Path:     writePath,
		Valid:    true,
		Warnings: warnings,
	}
	out.NetlistPath = strings.TrimSuffix(writePath, filepath.Ext(writePath)) + ".cir"

	rn := s.runner(s.cfg.Timeout)
	res, err := rn.Netlist(ctx, writePath, out.NetlistPath)
	if err != nil {
		return out, err
	}
	cir, rerr := os.ReadFile(out.NetlistPath)
	if rerr != nil {
		return out, fmt.Errorf("netlist generation produced no .cir: %v; qucs-s said: %s %s",
			rerr, strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))
	}
	parsed, perr := netlist.Parse(string(cir))
	if perr != nil {
		return out, fmt.Errorf("parse generated netlist: %w", perr)
	}
	if doValidate {
		rep := validate.Compare(spec, parsed)
		out.Report = &rep
		if !rep.Valid {
			out.Valid = false
		}
	}
	if doSimulate && out.Valid {
		sim, serr := s.simulate(ctx, writePath, s.cfg.Timeout, -1)
		if serr != nil {
			return out, serr
		}
		out.Simulation = sim
	}
	return out, nil
}

// circuitPath maps a .sch path to its .cir sibling.
func circuitPath(schPath string) string {
	return strings.TrimSuffix(schPath, filepath.Ext(schPath)) + ".cir"
}

func datasetPath(schPath string) string {
	return strings.TrimSuffix(schPath, filepath.Ext(schPath)) + ".dat"
}

// runner builds a runner against the configured installation, with an
// optional per-call timeout override.
func (s *Server) runner(timeout time.Duration) runner.Runner {
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return runner.Runner{QucsExe: s.cfg.QucsExe(), Timeout: timeout}
}

func (s *Server) handleCreate(ctx context.Context, _ *mcp.CallToolRequest, in createIn) (*mcp.CallToolResult, createOut, error) {
	if strings.TrimSpace(in.Netlist) == "" {
		return nil, createOut{}, fmt.Errorf("netlist is required")
	}
	out, err := s.generate(ctx, in.Name, in.OutputDir, in.BasePath, in.Netlist,
		"", defaultTrue(in.Validate), boolOr(in.Simulate, false), in.Overwrite)
	if err != nil {
		return nil, createOut{}, err
	}
	return nil, out, nil
}

func (s *Server) handleUpdate(ctx context.Context, _ *mcp.CallToolRequest, in updateIn) (*mcp.CallToolResult, createOut, error) {
	if strings.TrimSpace(in.Netlist) == "" {
		return nil, createOut{}, fmt.Errorf("netlist is required")
	}
	if in.Path == "" {
		return nil, createOut{}, fmt.Errorf("path is required")
	}
	if _, err := os.Stat(in.Path); err != nil {
		return nil, createOut{}, fmt.Errorf("schematic %s not found: %w", in.Path, err)
	}
	out, err := s.generate(ctx, "", "", in.Path, in.Netlist,
		in.Path, defaultTrue(in.Validate), boolOr(in.Simulate, false), true)
	if err != nil {
		return nil, createOut{}, err
	}
	return nil, out, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// ---------------------------------------------------------------- validate

type validateIn struct {
	Path            string `json:"path" jsonschema:"Schematic to validate"`
	ExpectedNetlist string `json:"expected_netlist,omitempty" jsonschema:"Expected SPICE netlist text; when given, it is re-derived into the model and used instead of the schematic's own components"`
}

type validateOut struct {
	Path     string           `json:"path" jsonschema:"Validated schematic"`
	Valid    bool             `json:"valid" jsonschema:"true when the pin-level netlist matches the expectation"`
	Report   *validate.Report `json:"report,omitempty" jsonschema:"Pin-level report"`
	Warnings []string         `json:"warnings,omitempty" jsonschema:"Non-fatal notes"`
}

func (s *Server) handleValidate(ctx context.Context, _ *mcp.CallToolRequest, in validateIn) (*mcp.CallToolResult, validateOut, error) {
	if in.Path == "" {
		return nil, validateOut{}, fmt.Errorf("path is required")
	}
	spec, err := readSpec(in.Path, in.ExpectedNetlist)
	if err != nil {
		return nil, validateOut{}, err
	}
	out := validateOut{Path: in.Path, Valid: true, Warnings: spec.Warnings}

	rn := s.runner(s.cfg.Timeout)
	cir := circuitPath(in.Path)
	res, err := rn.Netlist(ctx, in.Path, cir)
	if err != nil {
		return nil, validateOut{}, err
	}
	text, rerr := os.ReadFile(cir)
	if rerr != nil {
		return nil, validateOut{}, fmt.Errorf("netlist generation produced no .cir: %v; qucs-s said: %s %s",
			rerr, strings.TrimSpace(res.Stdout), strings.TrimSpace(res.Stderr))
	}
	parsed, perr := netlist.Parse(string(text))
	if perr != nil {
		return nil, validateOut{}, err
	}
	rep := validate.Compare(spec, parsed)
	out.Report = &rep
	out.Valid = rep.Valid
	return nil, out, nil
}

// readSpec builds the validation model: from the expected netlist when
// provided, otherwise rebuilt from the schematic's own components,
// wires and labels (design §13).
func readSpec(schPath, expectedNetlist string) (*model.CircuitSpec, error) {
	if strings.TrimSpace(expectedNetlist) != "" {
		return spice.Parse(expectedNetlist)
	}
	rd, err := sch.Read(schPath)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(schPath), filepath.Ext(schPath))
	spec, warnings := conn.Rebuild(rd, name)
	spec.Warnings = warnings
	return spec, nil
}

// unknownComponents lists base-file component types outside the catalog.
func unknownComponents(rd *sch.ReadDocument) []string {
	var out []string
	for _, c := range rd.Components {
		if !components.Supported(c.Type) && c.Type != "GND" {
			out = append(out, fmt.Sprintf("component %s has unsupported type %s", c.Ref, c.Type))
		}
	}
	return out
}

// firstQuoted extracts the first "…" string from a body.
func firstQuoted(body string) string {
	i := strings.IndexByte(body, '"')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(body[i+1:], '"')
	if j < 0 {
		return ""
	}
	return body[i+1 : i+1+j]
}

// ---------------------------------------------------------------- simulate

type simulateIn struct {
	Path        string `json:"path" jsonschema:"Schematic to simulate"`
	TimeoutS    *int   `json:"timeout_s,omitempty" jsonschema:"Subprocess timeout in seconds (default: configured, otherwise 60)"`
	IncludeData bool   `json:"include_data,omitempty" jsonschema:"Include the full data arrays in the response (default false: names and counts only)"`
}

type simVar struct {
	Name   string       `json:"name" jsonschema:"Variable name, e.g. 'ac.v(out)'"`
	Indep  bool         `json:"indep" jsonschema:"true for independent variables (sweeps)"`
	Points int          `json:"points" jsonschema:"Number of stored points"`
	Data   [][2]float64 `json:"data,omitempty" jsonschema:"[re, im] pairs when include_data=true"`
}

type simOut struct {
	Path      string   `json:"path" jsonschema:"Dataset file"`
	OK        bool     `json:"ok" jsonschema:"true when the dataset was produced and parsed"`
	Variables []simVar `json:"variables,omitempty"`
	Stdout    string   `json:"stdout,omitempty" jsonschema:"Tail of the simulator output (on failures)"`
	Stderr    string   `json:"stderr,omitempty" jsonschema:"Simulator stderr (on failures)"`
}

func (s *Server) handleSimulate(ctx context.Context, _ *mcp.CallToolRequest, in simulateIn) (*mcp.CallToolResult, simOut, error) {
	if in.Path == "" {
		return nil, simOut{}, fmt.Errorf("path is required")
	}
	var timeout time.Duration
	if in.TimeoutS != nil && *in.TimeoutS > 0 {
		timeout = time.Duration(*in.TimeoutS) * time.Second
	}
	if in.IncludeData {
		sim, err := s.simulate(ctx, in.Path, timeout, -1)
		if err != nil {
			return nil, simOut{}, err
		}
		return nil, *sim, nil
	}
	sim, err := s.simulate(ctx, in.Path, timeout, 0)
	if err != nil {
		return nil, simOut{}, err
	}
	return nil, *sim, nil
}

// simulate runs qucs-s --run and parses the produced dataset;
// includeData < 0 adds the full value arrays.
func (s *Server) simulate(ctx context.Context, schPath string, timeout time.Duration, includeData int) (*simOut, error) {
	datPath := datasetPath(schPath)
	_ = os.Remove(datPath) // stale data must not leak into the result

	rn := s.runner(timeout)
	res, err := rn.Simulate(ctx, schPath, datPath)
	if err != nil {
		return &simOut{Path: datPath, Stdout: tail(res.Stdout), Stderr: tail(res.Stderr)}, err
	}
	raw, rerr := os.ReadFile(datPath)
	if rerr != nil {
		return &simOut{
			Path:   datPath,
			OK:     false,
			Stdout: tail(res.Stdout),
			Stderr: tail(res.Stderr),
		}, fmt.Errorf("simulation produced no dataset: %v", rerr)
	}

	d, perr := dataset.Parse(string(raw))
	if perr != nil {
		return &simOut{Path: datPath, OK: false}, fmt.Errorf("parse dataset: %w", perr)
	}
	out := &simOut{Path: datPath, OK: true}
	for _, v := range d.Vars {
		sv := simVar{Name: v.Name, Indep: v.Indep, Points: len(v.Values)}
		if includeData != 0 {
			sv.Data = make([][2]float64, len(v.Values))
			for i, z := range v.Values {
				sv.Data[i] = [2]float64{real(z), imag(z)}
			}
		}
		out.Variables = append(out.Variables, sv)
	}
	return out, nil
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		s = "…" + s[len(s)-2000:]
	}
	return s
}

// ---------------------------------------------------------------- read

type readIn struct {
	Path string `json:"path" jsonschema:"Schematic to read"`
}

type readOut struct {
	Path       string   `json:"path" jsonschema:"Schematic read"`
	Version    string   `json:"version" jsonschema:"Schematic format version"`
	Components []any    `json:"components" jsonschema:"Components with ref/type/value/position/body"`
	Wires      []any    `json:"wires" jsonschema:"Wires with labels"`
	Warnings   []string `json:"warnings,omitempty" jsonschema:"Unsupported components found"`
}

func (s *Server) handleRead(_ context.Context, _ *mcp.CallToolRequest, in readIn) (*mcp.CallToolResult, readOut, error) {
	if in.Path == "" {
		return nil, readOut{}, fmt.Errorf("path is required")
	}
	rd, err := sch.Read(in.Path)
	if err != nil {
		return nil, readOut{}, err
	}
	out := readOut{Path: in.Path, Version: rd.Version}
	out.Warnings = unknownComponents(rd)
	for _, c := range rd.Components {
		item := map[string]any{
			"ref":    c.Ref,
			"type":   c.Type,
			"x":      c.X,
			"y":      c.Y,
			"rot":    c.Rot,
			"mirror": c.Mirror,
		}
		if v := firstQuoted(c.Body); v != "" || c.Body != "" {
			item["value"] = v
			item["body"] = c.Body
		}
		out.Components = append(out.Components, item)
	}
	for _, w := range rd.Wires {
		out.Wires = append(out.Wires, map[string]any{
			"x1": w.X1, "y1": w.Y1, "x2": w.X2, "y2": w.Y2,
			"label": w.Label,
		})
	}
	return nil, out, nil
}

// ---------------------------------------------------------------- catalog

type listOut struct {
	Types []compEntry `json:"types" jsonschema:"Supported component types"`
}

type compEntry struct {
	Type        string   `json:"type" jsonschema:"Schematic type"`
	SpiceLetter string   `json:"spice_letter" jsonschema:"SPICE device letter"`
	Pins        []string `json:"pins" jsonschema:"Pin names in netlist order"`
}

var catalogOrder = []string{"R", "C", "L", "S4Q_V", "S4Q_I", "DIODE_SPICE", "_BJT", "_MOSFET", "GND",
	".AC", ".TR", ".DC", "SpicePar", "SpiceInclude"}

var typeShort = map[string]string{
	"R": "Resistor", "C": "Capacitor", "L": "Inductor",
	"S4Q_V": "SPICE independent voltage source", "S4Q_I": "SPICE independent current source",
	"DIODE_SPICE": "SPICE diode", "_BJT": "SPICE BJT (npn/pnp)", "_MOSFET": "SPICE MOSFET (nfet/pfet)",
	"GND": "Ground symbol (SPICE node 0)",
	".AC": "AC frequency sweep", ".TR": "Transient analysis", ".DC": "DC operating point",
	"SpicePar": ".param block", "SpiceInclude": ".include block",
}

func (s *Server) handleListComponents(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, listOut, error) {
	out := listOut{}
	for _, t := range catalogOrder {
		e := compEntry{Type: t, SpiceLetter: components.SpiceLetter(t)}
		for _, p := range components.PinsOf(t, false) {
			e.Pins = append(e.Pins, p.Name)
		}
		out.Types = append(out.Types, e)
	}
	return nil, out, nil
}

type describeIn struct {
	Type string `json:"type" jsonschema:"Component type, as reported by list_components"`
}

type describeOut struct {
	Type        string    `json:"type"`
	Description string    `json:"description" jsonschema:"What the component is"`
	SpiceLetter string    `json:"spice_letter,omitempty" jsonschema:"SPICE device letter"`
	Pins        []pinDesc `json:"pins,omitempty" jsonschema:"Pins with local offsets and orientation notes"`
	Example     string    `json:"example,omitempty" jsonschema:"Example schematic line"`
	Notes       []string  `json:"notes,omitempty" jsonschema:"Calibration notes"`
}

type pinDesc struct {
	Name string `json:"name"`
	DX   int    `json:"dx" jsonschema:"Offset along x"`
	DY   int    `json:"dy" jsonschema:"Offset along y"`
}

func (s *Server) handleDescribeComponent(_ context.Context, _ *mcp.CallToolRequest, in describeIn) (*mcp.CallToolResult, describeOut, error) {
	t := strings.TrimSpace(in.Type)
	if t == "" {
		return nil, describeOut{}, fmt.Errorf("type is required")
	}
	if !components.Supported(t) {
		return nil, describeOut{}, fmt.Errorf("unsupported type %q; call list_components for the catalog", t)
	}
	out := describeOut{
		Type:        t,
		Description: typeShort[t],
		SpiceLetter: components.SpiceLetter(t),
	}
	for _, p := range components.PinsOf(t, false) {
		out.Pins = append(out.Pins, pinDesc{Name: p.Name, DX: p.DX, DY: p.DY})
	}
	if ex, note := exampleLine(t); ex != "" {
		out.Example = ex
		out.Notes = append(out.Notes, note)
	}
	switch t {
	case "S4Q_V", "S4Q_I":
		out.Notes = append(out.Notes,
			"pin 1 carries the '+' marking; SPICE node order is (1, 2)",
			"vertical placement uses Mirror=1 so pin 1 lands on top")
	case "R", "C", "L":
		out.Notes = append(out.Notes, "vertical placement uses Rot=3 so pin 1 lands on top")
	case "_BJT":
		out.Notes = append(out.Notes, "body[0] selects npn/pnp; bulk nodes are not exposed")
	case "_MOSFET":
		out.Notes = append(out.Notes, "body[0] selects nfet/pfet; qucs-s merges the bulk node into S")
	case "GND":
		out.Notes = append(out.Notes, "the only pin is at the symbol origin; produces SPICE node 0")
	}
	return nil, out, nil
}

// exampleLine renders a catalog example for the type.
func exampleLine(t string) (string, string) {
	switch t {
	case "R":
		c := components.R("R1", 100, 220, "1k", false)
		return c.Render(), "horizontal; Rot=3 flips to vertical"
	case "C":
		c := components.C("C1", 100, 220, "100n", false)
		return c.Render(), "horizontal; Rot=3 flips to vertical"
	case "L":
		c := components.L("L1", 100, 220, "1u", false)
		return c.Render(), "horizontal; Rot=3 flips to vertical"
	case "S4Q_V":
		c := components.S4QV("V1", 100, 100, "DC 1 AC 1", false)
		return c.Render(), "side pin 1 = '+' (SPICE first node)"
	case "S4Q_I":
		c := components.S4QI("I1", 100, 100, "DC 1m", false)
		return c.Render(), "pin 1 = current exit, oriented like S4Q_V"
	case "GND":
		return components.GND(100, 250).Render(), ""
	case "DIODE_SPICE":
		c := components.Diode("D1", 100, 220, "1N4148", false)
		return c.Render(), "anode on the left (pin 1)"
	case "_BJT":
		c := components.BJT("Q1", 100, 360, false)
		return c.Render(), "nodes C, B, E"
	case "_MOSFET":
		c := components.MOSFET("M1", 100, 360, false)
		return c.Render(), "nodes D, G, S"
	case ".AC":
		c := components.AC("AC1", 40, 560, "log", "10", "1e+06", 456)
		return c.Render(), "log points = points_per_decade*decades+1"
	case ".TR":
		c := components.TR("TR1", 40, 560, "0", "1e-3", 51, "0")
		return c.Render(), "points = round((stop-start)/step)+1"
	case ".DC":
		c := components.DCOrOp("DC1", 40, 560)
		return c.Render(), "SPICE mode: plain OP"
	case "SpicePar":
		c := components.SpicePar("PARAM1", 40, 660, map[string]string{"g": "1.234"})
		return c.Render(), "name=value pairs appear in the body"
	case "SpiceInclude":
		c := components.SpiceInclude("INC1", 40, 820, "models.lib")
		return c.Render(), "file paths are body properties"
	}
	return "", ""
}

// ---------------------------------------------------------------- open

type openIn struct {
	Path string `json:"path" jsonschema:"Schematic to open in the GUI"`
}

type openOut struct {
	Launched bool   `json:"launched" jsonschema:"true when the GUI process started"`
	Path     string `json:"path" jsonschema:"Schematic opened"`
}

func (s *Server) handleOpen(_ context.Context, _ *mcp.CallToolRequest, in openIn) (*mcp.CallToolResult, openOut, error) {
	if in.Path == "" {
		return nil, openOut{}, fmt.Errorf("path is required")
	}
	if _, err := os.Stat(in.Path); err != nil {
		return nil, openOut{}, fmt.Errorf("schematic %s not found: %w", in.Path, err)
	}
	// Fire-and-forget GUI: start the process detached so the server
	// keeps serving while the editor is open.
	cmd := exec.Command(s.cfg.QucsExe(), in.Path)
	if err := cmd.Start(); err != nil {
		return nil, openOut{}, fmt.Errorf("launch GUI: %w", err)
	}
	go func() { _ = cmd.Wait() }() // reap the process to avoid zombies
	return nil, openOut{Launched: true, Path: in.Path}, nil
}

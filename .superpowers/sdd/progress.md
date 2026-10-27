# SDD ledger — plan: C:\Users\lau\Desktop\Soft\qucs-mcp\docs\superpowers\plans/2026-10-24-qucs-mcp.md

- Plan: `C:\Users\lau\Desktop\Soft\qucs-mcp\docs\superpowers/plans/2026-10-24-qucs-mcp.md`
- Design: `C:\Users\lau\Desktop\Soft\qucs-mcp\docs/superpowers/specs/2026-10-24-qucs-mcp-design.md`
- Executor: subagent-driven (partner accepted controller's recommendation)
- Base for Task 1: empty tree `4b825dc642cb6eb9a060e54bf8d69288fbee4904` (fresh repo, no commits)

## Tasks

- [x] Task 1: Scaffold — module, config, runner, MCP server + doctor tool
- [x] Task 2: `.sch` document writer
- [x] Task 3: Component catalog
- [x] Task 4: Calibration against Qucs-S and writer→netlist integration
- [x] Task 5: SPICE text parser + value normalization
- [x] Task 6: Layout → `.sch` → netlist parsing/validation
- [x] Task 7: Dataset parsing + simulation
- [x] Task 8: MCP tools, read/update, docs, smoke, build

## Pre-flight scan

| Task pair / task self-check | Result |
|---|---|
| Task 1 files vs Task 8 files (internal/server/server.go) | OK — Task 8 modifies Task 1's file by design; interface stable |
| Task 1 vs Task 8 (produce `mcp.Server`) | OK — Task 1 produces `server.NewServer`-style hook; Task 8 extends |
| Task 2 vs Task 4/6 (produce sch.Document) | OK — consumers declared in plan |
| Task 3 vs Task 6/8 (produce catalog) | OK — |
| Task 5 vs Task 6 (consume model, produce nothing) | OK — |
| Tasks vs Global Constraints | OK |

No blocking findings.

## Rulings

- Qucs-S install in repo is **26.1.1** (not 25.1.1); format compatible, go.mod says go 1.25.
- Calibration findings (Task 4, `calibration_test.go`):
  - wires connect to pins only when a wire **endpoint** coincides with the pin point;
    passing over a pin mid-wire does not connect.
  - S4Q_V/S4Q_I mirror=0: pin1=+(+30,0) right, pin2=(−30,0) left; mirror=1 vertical:
    pin1=(0,−30) top, pin2=(0,+30) bottom (= SPICE n+/n− order).
  - R/C/L pin1=left(−30,0), pin2=right(+30,0); vertical via Rot=3 (pin1 top).
  - DIODE_SPICE pin1=anode(left), pin2=cathode; _BJT nodes in order C,B,E;
    _MOSFET D,G,S (substrate merges into S in netlist).
  - qucs-s prefixes the ref with the device letter (RcalR) unless the ref already
    starts with it → `components.SpiceLetter` helper; layout uses already-prefixed
    SPICE refs so no mangling.
  - installed binary does NOT apply the upstream `rotate()` historical flaw; trust
    empirics over github master for pin geometry.
- AC/TR mapping calibrations (Task 6, temp/cal/acmap*, trmap):
  - SPICE `.ac dec N s e` → Qucs `.AC Sweep=log Points=round(N*decades)+1`
    (spice4qucs emits `ac dec <(Points-1)/decades>`); `.ac lin N` passes
    Points straight through; `.tran step stop` → `.TR lin start stop
    round((stop-start)/step)+1`, spice4qucs recomputes the step itself.
  - identical labels on disjoint wires MERGE nets (short risk; each net must
    carry exactly one consistent label everywhere).
  - label offsets (LX/LY) may be off-grid ("208 270"); only wire endpoints
    and component anchors need multiples of 10.
- STOP-SUFFIX BUG (Task 7 calibration): spice4qucs translates a Qucs
  ".AC" stop "1M" into `1m` in the ngspice .control line (mega lost);
  ngspice then simulates 10→10k (274 pts) — silently WRONG. Suffix-free
  forms ("1e6", "1e+06", "1000000") pass through verbatim and give the
  exact foot (10→1e6, 456 pts). layout now emits start/stop through
  `neutralNumber` (FormatFloat 'g'). .TR values ("1 ms") translate
  correctly by themselves but are also neutralized for safety.
- Dataset format (Task 7): header `<Qucs Dataset 26.1.1>`; blocks
  `<indep name N>…</indep>` (real numbers) and `<dep name indepname>…</dep>`
  (complex "re+jim", sign inside the mantissa after "+j"). rclow AC dataset:
  indep `frequency` n=456, deps `ac.v(in)`, `ac.v(out)`.
- Task 8 rulings:
  - SPICE input `1M` = milli (mega = `1MEG`); the spice parser normalizes
    correctly, test netlists must use `MEG`.
  - designer parser (`internal/sch` reader) tolerates unknown components;
    every `<GND * 1 x y 0 0 0 0>` line has exactly 9 tokens (no body).
  - wire label-less lines always carry an empty `""` label token; any real
    Qucs file does too.
  - connectivity rebuild (`internal/conn`): union-find on grid points;
    net = wire label, GND origins → node "0"; orientations reconstructed
    from the two layout-emitted forms (passives Rot=3, sources Mirror=1).
  - position preservation on regenerate: `layout.BuildPlaced` +
    `Placements map[ref][2]int` from the previous file (orientation stays
    layout-controlled because stub wires regenerate from the catalog).
  - MCP SDK v1.8: `CallToolParams.Arguments` is `any` — pass the map, NOT
    pre-marshaled JSON (double encoding otherwise).
- Suffix-digit bug (post-Task 8, live demo): SPICE `3k3`-style values lost
  their fractional tail (spice parser kept `3k3` verbatim; qucs-s designer
  shows `3k`). `NormalizeValue` now expands suffix-digits (`3k3`→`3.3k`,
  `10k02`→`10.02k`, `1M5`→`1.5m` [SPICE M=milli], `4R7`→`4.7`); unit junk
  after digits (e.g. `10u2F`) is dropped.
- BJT native-template ruling (live demo, CE cascade): the `_BJT` body
  reconstructed from `--list-entries` was off by one property slot — values
  after pos ~13 misaligned, so the spice4qucs netlist came out with
  `AREA=26.85 TEMP=0.0` (kT=0 → dead transistor, CE gain −168.6 dB) and
  Cjc/Re/Rb/Rc silently zero. Fix: `bjtBody` now carries the body verbatim
  from the native example `ngspice/Devices/BJT_dc.sch` (49 value/show
  pairs, ends at `"yes" 0`, no trailing `"Generic"` slots); the npn/pnp
  selector stays first with show=1. Verified: netlist `Q1 … AREA=1.0` (no
  TEMP), Bf=200, Cjc=8e-12; CE amp gain −7.9 dB at 1 kHz (live ngspice).
- MOSFET native-template ruling (same pattern): the `_MOSFET` recon body
  had 43 correct pairs + 3 trailing extras (`"yes"`/`"Generic"` ×2, plus
  stray show-flags on `2e-5`/`0.0`). Trailing slots dropped; body now ends
  at the second `26.85` temperature entry exactly like native examples.
  Ruling: for SPICE-mapped (SPICE-letter) components always quote a native
  `.sch` example body verbatim — `--list-entries` reconstruction adds
  tail slots and is not pin/value faithful.

## Activity

- design approved by partner
- builder check answered; all 11 questions settled in design §18
- plan written; partner chose subagent-driven (accepted recommendation)
- Task 1+2 implemented natively and committed (652a9f3). Env notes: Go 1.25.4
  (not 1.27.3; go.mod says 1.25), Qucs-S lives in repo `qucs/` dir (config
  auto-detects `<cwd>/qucs`), MCP SDK API v1.8.0 verified against module cache:
  AddTool[In,Out], CallToolResult with IsError, StdioTransport, in-memory
  transports via Server.Connect (Run blocks); doctor answers over real stdio.

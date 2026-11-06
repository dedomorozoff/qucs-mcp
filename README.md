# qucs-mcp
![GitHub Release](https://img.shields.io/github/v/release/dedomorozoff/qucs-mcp)
[![Go Version](https://img.shields.io/github/go-mod/go-version/dedomorozoff/dmed)](https://github.com/dedomorozoff/qucs-mcp)
[![License](https://img.shields.io/github/license/dedomorozoff/dmed)](https://github.com/dedomorozoff/qucs-mcp/blob/main/LICENSE)
[![Last Commit](https://img.shields.io/github/last-commit/dedomorozoff/dmed)](https://github.com/dedomorozoff/qucs-mcp)
![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/dedomorozoff/qucs-mcp/total)
An MCP server that builds Qucs-S schematics (`.sch`) from SPICE netlist
text, validates them against the netlist Qucs-S itself generates, and runs
the simulation. Fully headless — `qucs-s` is used as a subprocess only.

```
SPICE text → internal/spice → model → internal/layout → .sch
           → qucs-s -n --ngspice → .cir → internal/netlist → internal/validate
           → qucs-s -n --ngspice --run → .dat → internal/dataset
```

Connectivity is carried by **wire labels**, not by geometry: identical
labels on physically separate wires merge into one net, so repositioning a
component never changes the circuit.

## Build

```sh
go build -o bin/qucs-mcp ./cmd/qucs-mcp     # Windows: bin/qucs-mcp.exe
```

The binary answers `--version` with the stamped build identity, which is
worth checking before debugging an MCP registration:

```
$ bin/qucs-mcp --version
qucs-mcp v0.2.0 (commit 1a4c918, go1.25.4, windows/amd64)
```

Requires Go 1.25+ and a local Qucs-S installation. The binary is looked up
in this order: `$QUCS_S_ROOT`, a `qucs/` directory in or above the working
directory, `PATH`, then the usual system install locations. Run the `doctor`
tool first — it reports which locations were searched.

## MCP tools

| Tool | Purpose |
|---|---|
| `doctor` | Qucs-S installation, paths and configuration; call it first |
| `create_schematic(netlist, name?, output_dir?, base_path?, validate=true, simulate=false, overwrite=false)` | SPICE text → `.sch`; returns `path`, `netlist_path`, `valid`, `report`, `warnings`, `simulation` |
| `update_schematic(path, netlist, validate?, simulate?)` | regenerate an existing `.sch` from new SPICE text, keeping component positions by reference |
| `validate_schematic(path, expected_netlist="")` | netlist of an existing schematic against its own model, or against explicitly given SPICE text |
| `simulate_schematic(path, timeout_s?, include_data=false)` | simulation; dataset summary — variable names and point counts (`include_data=true` adds the arrays) |
| `read_schematic(path)` | model of an existing schematic: components (ref/type/value/x/y) plus labelled wires; unknown types are warnings, not errors |
| `list_components()` | supported types: R, C, L, S4Q_V, S4Q_I, DIODE_SPICE, _BJT, _MOSFET, GND, .AC, .TR, .DC, SpicePar, SpiceInclude, SpiceModel |
| `describe_component(type)` | catalog deep dive: pins with offsets, orientation, example schematic line |
| `open_schematic(path)` | open the schematic in the Qucs-S GUI as a separate process (does not block the server) |

Tool errors are returned structurally: the SDK replies with `IsError=true`
and the server keeps running.

## Registering in opencode

`~/.config/opencode/opencode.json` (or `%USERPROFILE%\.config\opencode\opencode.json`):

```json
{
  "mcp": {
    "qucs": {
      "type": "local",
      "command": ["C:\\path\\to\\qucs-mcp\\bin\\qucs-mcp.exe"],
      "enabled": true,
      "environment": {
        "QUCS_S_ROOT": "C:\\path\\to\\Qucs-S-26.1.1-win64"
      }
    }
  }
}
```

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `QUCS_S_ROOT` | local `./qucs`, then the system install | Qucs-S installation root |
| `QUCS_MCP_WORKDIR` | `~/Documents/qucs-mcp` | where schematics, netlists and datasets are written |
| `QUCS_MCP_TIMEOUT` | `120` | subprocess timeout for `qucs-s`, in seconds |

## Netlist semantics

These rules are what make a generated schematic *whole*; each one is
covered by an end-to-end test in `internal/e2e`.

- **Device letters**: `R C L V I D Q M` are supported. Anything else, and
  `.subckt`/`.ends` in particular, is rejected with a message rather than
  silently dropped.
- **`.model` cards reach the netlist verbatim**, directive dot included.
  Qucs-S copies the card text straight into the `.cir`.
- **Transistor models are per device.** In spice mode Qucs-S builds
  `QMOD_<ref>` / `MMOD_<ref>` from the component properties, not from the
  named card, so card parameters are carried into the device. PNP/NPN and
  PMOS/NMOS are resolved from the card type word or its `pchan`/`nchan`
  flags. Diodes keep referring to the card by name.
- **Parameters without a Qucs-S property slot are reported, not dropped
  silently.** Model and instance parameters (including `L`, `W`, `AREA`)
  are mapped onto numbered properties; anything unmapped, e.g. level-3
  `THETA`, becomes a remark on the result.
- **No analysis directive means a DC operating point.** A schematic with no
  `.op`/`.ac`/`.tran` used to simulate to an empty dataset; now a default
  operating point is added and announced in the remarks.
- **An empty dataset is a failure, not a success.** ngspice can abort
  midway — an undefined model, a bad card — exit 0 and leave a `.dat` with
  nothing but the sweep axis. `simulate_schematic` reports that as an error.
- **Net names.** A net called `gnd` (any spelling) becomes node `0`:
  Qucs-S resolves it to ground but still prints `v(gnd)`, which kills the
  simulation. Node names are case insensitive, so `A` and `a` are one net.
  Characters outside `[A-Za-z0-9_]` survive the netlist but vanish from the
  dataset, so such a net is renamed (e.g. `$n$1` → `_n_1`) and the rename is
  reported as a warning.
- **`.dc` sweeps are not available** in Qucs-S spice mode: no sweep variable
  exists, so the schematic runs a single operating point and the tool says
  so in its remarks.
- **Inline `;` comments** are stripped from the line they sit on; they never
  reach a value or a node name.

## Concurrency

Qucs-S headless runs do not tolerate overlap. Two `qucs-s -n --ngspice`
processes started close together pick up each other's intermediate ngspice
deck: the dataset comes back with the requesting schematic's analysis but
*another* schematic's node names (reproduced on Qucs-S 26.1.1, roughly one
run in three). The server therefore serializes every Qucs-S invocation in
`internal/runner`, so concurrent `create_schematic`/`validate_schematic`/
`simulate_schematic` calls queue instead of racing, and `internal/qtest`
takes a lock file for the same reason so `go test ./...` stays
deterministic across packages. `internal/server/concurrency_test.go`
guards the behaviour: two clients simulating two different dividers at the
same time must each get their own node names back.

The lock only covers this process. Do not run the Qucs-S GUI, a second
qucs-mcp server, or a manual `qucs-s -n` on the same machine while a
simulation is in flight — the results can come from the other circuit.
`open_schematic` is deliberately not part of the queue: it only launches
the GUI.

## Calibrated quirks

- **AC stop suffix**: spice4qucs turns a Qucs `.AC` stop of `1M` into ngspice
  `1m` — mega is silently lost and the sweep comes up short. Layout writes
  start/stop in suffix-free scientific form (`1e+06`); verified on
  10 Hz → 1 MHz with 456 points.
- In SPICE **text**, `1M` means **milli**; mega is `1MEG`/`10MEG`. Value
  normalization translates between the SPICE and Qucs conventions,
  including suffix-digit literals (`3k3` = 3.3k, `4R7` = 4.7).
- Qucs log points are `round(points_per_decade × decades) + 1` and transient
  points are `round((stop − start) / step) + 1`.
- Pin calibration: `S4Q_V`/`S4Q_I` with `mirror=0` put pin 1 (the `+`
  terminal) at (+30, 0); vertical passives with `Rot=3` have pin 1 on top;
  `_BJT` pins are C, B, E and `_MOSFET` pins are D, G, S with the bulk folded
  into the source. A wire connects to a pin only when its endpoint sits
  exactly on the pin.

## Tests

```sh
go test ./... -timeout 300s
```

Unit tests run everywhere. Integration tests (`internal/e2e`,
`internal/validate`, `internal/dataset`, `internal/server`) drive the real
`qucs-s` binary and skip when it is not installed. The schematic reader
regression runs over every `.sch` in `qucs/share/qucs-s/examples` (248
files).

## CI

`.github/workflows/ci.yml` builds and tests on Ubuntu and Windows: `gofmt`,
`go vet`, `go build` and `go test`, plus a release binary uploaded as an
artifact. Without a Qucs-S installation on the runner, the integration tests
skip and the unit suite still gates the merge.

## Releases

`.github/workflows/release.yml` runs on a `v*` tag: it cross-compiles
windows/linux/darwin × amd64/arm64, packs each into
`qucs-mcp-<tag>-<os>-<arch>.(zip|tar.gz)` next to a copy of this README,
writes `SHA256SUMS` and publishes a GitHub Release with generated notes.
Version and commit are stamped into the binary with `-ldflags -X`, so a
downloaded build reports the tag it came from.

```sh
git tag -a v0.2.0 -m "coherent schematics" && git push origin v0.2.0
```

Only first-party actions and the runner's own `gh` are used, and a `checks`
job (gofmt, vet, build, test) gates the packaging. The Qucs-S integration
tests still skip on a runner without Qucs-S, so the numeric behaviour
remains gated by CI on a runner where `QUCS_S_ROOT` points at a real
installation.

## Russian documentation

See [README.ru.md](README.ru.md).

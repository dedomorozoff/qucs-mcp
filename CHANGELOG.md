# Change log

The short version of what each published release contains. The release
pages carry the commit list for the build itself, so this file only
records the releases, not every commit.

## v0.1.4 — 2026-11-09

The pretty router draws transistor stages. `_BJT` and `_MOSFET` join the
signal chain like any other device: base/gate on the current row,
collector/drain on an exit rail one row up — where the chain continues —
and the emitter/source leg dropping below into its own labeled rail
carrying its grounded bypasses (or reaching straight into a ground
symbol). Load resistors that join two chain nets (the classic collector
resistor `Rc`) rise as vertical feeders off the exit rail with their
supply pin becoming a labeled stub. The one-transistor TRF receiver —
tank into a biased stage with a bypassed emitter, detector at the
collector — now comes out as one continuous circuit instead of falling
back to fragments. Load resistors that join two chain nets (the classic
collector resistor `Rc`) rise as vertical feeders off the exit rail with
their supply pin becoming a labeled stub. The bias source V1/Rb1 of the
TRF stage is attached to the feeder stub through an overhead bus wire
(rising from the source's exit to the stub row and running along it),
so the supply block is no longer a separate floating piece.

The simple op-amp-style amplifier joins the showcase as case `opamp`:
source → R1 → BJT stage (emitter feedback via Re/Ce, collector load via
Rc) with a flat AC gain asserted end to end against the real simulator,
which covers the amplifier's dynamic-range response (gain vs frequency).

The all-or-nothing contract is unchanged: topologies the stage walker
cannot express (bridges, floating emitter legs, loops) still fall back
to the per-pin stub layout.

## v0.1.3 — 2026-11-07

Schematics look hand-drawn by default. `create_schematic` now lays out
pure two-terminal circuits (R, C, L, diodes, SPICE sources) as a
continuous left-to-right chain: one rail per net with a single label,
vertical shunts under their junctions with ground symbols below, and
the grounded source leading the chain. Topologies the new router cannot
express (semiconductors, loops with right-side entry, double-grounded
devices) fall back to the previous per-pin stub layout; regenerating an
existing schematic keeps its placed positions and stays stub-based.

Under the hood this exposed a real file-format bug: the two orientation
fields of a component line are MIRROR, then ROT (measured on Qucs-S
26.1.1), and the server used to write and read them swapped. Vertical
passives are now `Rot=3` (pin 1 on top), vertical sources `Rot=1`, and
the pin reconstruction follows the same rule, so what qucs-s produces
from a generated schematic matches the model exactly. Electrical
results are unchanged — the e2e suite drives a part of the showcase
crystal netlist through the default path and asserts its ~1 MHz tank
resonance.

## v0.1.2 — 2026-11-06

Documentation release: the change log ships inside the published
archives, README gains licence sections and corrected badges, and the
release workflow became reproducible from a tag (`workflow_dispatch`
with an explicit tag input, publish step verified).

## v0.1.1 — 2026-11-06

First release with published binaries: windows, linux and darwin for
amd64 and arm64, each archive with the server binary, this documentation
and the licence, plus `SHA256SUMS` over all six. `qucs-mcp --version`
reports the tag and the commit the binary was stamped from.

## v0.1.0 — 2026-11-06

No published release. The tag was moved several times while the release
workflow was being fixed — deleting a tag deletes the release that points
at it — and it now names the same commit as v0.1.1. Binaries for this
version are the v0.1.1 assets.

## What v0.1.x is

An MCP server that takes SPICE netlist text, builds a Qucs-S schematic
from it, validates the netlist Qucs-S generates back from that schematic,
and runs the simulation. Nine tools; the rules the pipeline guarantees
(`.model` cards reaching the device they name, suffix-free scientific
notation in analysis directives, normalized node names, an empty dataset
reported as a failure) are described in [README.md](README.md), and each
one is covered by the end-to-end suite in `internal/e2e`.

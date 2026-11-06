# Change log

The short version of what each published release contains. The release
pages carry the commit list for the build itself, so this file only
records the releases, not every commit.

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

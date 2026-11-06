// Package e2e drives the whole pipeline over real netlist text:
// SPICE text → model → layout → .sch → Qucs-S → .cir → .dat.
//
// The cases here are the failure modes that used to produce plausible
// but incoherent schematics: a missing analysis, a .model card that
// lost its directive dot, transistor parameters that never reached the
// device, node names Qucs-S resolves differently and nets it cannot
// resolve at all. Tests that only need Qucs-S to be absent skip; the
// rest run everywhere.
package e2e

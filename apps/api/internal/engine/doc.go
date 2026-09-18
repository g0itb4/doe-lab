// Package engine computes dynamic operating envelopes for a low-voltage
// feeder: an unbalanced 4-wire power flow, constraint evaluation, and a search
// for the largest export and import limit that keeps the feeder within its
// voltage, transformer and cable ratings.
//
// The package is pure. It does no I/O, reads no clock and holds no global
// state, so the same inputs always give the same envelopes. cmd/engine feeds it
// from the API over RPC.
package engine

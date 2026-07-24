// Command module-aiostreams runs this module as its own process, for a Platform
// that hosts it out of process (ADR 0064, ADR 0077).
//
// The whole of it is one line, and that is the point rather than a
// simplification. ADR 0064 is arranged around the property that crossing the
// process boundary must not change what a module author writes: the Capability
// below is the same plain Go value the Platform used to link into its own
// binary, its provider roles are the same methods, and its tests still run with
// no transport at all. What changes is that something now serves it.
//
// This module builds and behaves identically whether or not this file is used.
// Nothing else here imports it, and `aiostreams.New` remains what a
// statically-composed Platform would call; the switch to out-of-process is a
// composition change in the Platform, not a change here.
//
// Two things a module author inherits by using host.Serve, both easy to trip
// over and neither obvious from this file:
//
//   - **Nothing may be written to stdout.** go-plugin writes its handshake
//     there, and anything else corrupts it. Use the Telemetry reached from the
//     invocation's context (ADR 0059).
//   - **The Caller is a handle, not a session.** It is minted per invocation and
//     stops resolving when that invocation returns, so it cannot usefully be
//     stored. Module code forwards what it was given, exactly as ADR 0017
//     already required.
package main

import (
	"github.com/mosaic-media/sdk/host"

	aiostreams "github.com/mosaic-media/module-aiostreams"
)

func main() {
	// nil takes the module's default HTTP client. Out of process the Platform
	// cannot hand one in — an *http.Client does not cross a process boundary — so
	// egress is contained by the forward proxy the Platform operates and forces
	// this process's default transport through (ADR 0064; sdk/host's egress path).
	host.Serve(aiostreams.New(nil))
}

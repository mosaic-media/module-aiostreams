// Command module-aiostreams runs this module as its own process, for a Platform
// that hosts it out of process (platform#39, sdk#7).
//
// It is one line because crossing the process boundary must not change what a
// module author writes (platform#39): the Capability below is the same plain Go
// value a statically composed Platform would link in, its provider roles are the
// same methods, and its tests run with no transport at all. This module builds and
// behaves identically whether or not this file is used — nothing else here
// imports it, and aiostreams.New remains what a host calls.
//
// Two constraints a module inherits from host.Serve, neither of them obvious
// from this file:
//
//   - Nothing may be written to stdout. go-plugin writes its handshake there,
//     and anything else corrupts it. Use the Telemetry reached from the
//     invocation's context (sdk#5).
//   - The Caller is a handle, not a session. It is minted per invocation and
//     stops resolving when that invocation returns, so it cannot usefully be
//     stored. Forward what you were given (platform#13).
package main

import (
	"github.com/mosaic-media/sdk/host"

	aiostreams "github.com/mosaic-media/module-aiostreams"
)

func main() {
	// nil takes the module's default HTTP client. Out of process the Platform
	// cannot hand one in — an *http.Client does not cross a process boundary — so
	// egress is contained by the forward proxy the Platform operates and forces
	// this process's default transport through (platform#39; sdk/host's egress path).
	host.Serve(aiostreams.New(nil))
}

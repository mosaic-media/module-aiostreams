// Package aiostreams is the AIOStreams module: a **dedicated stream provider**
// for one named upstream, as distinct from `module-stremio-addons`, which is a
// host for whatever addons a user pastes in.
//
// The distinction is the reason this module exists. The addon module can source
// from any Stremio addon, and that is its value and its problem: the addon
// ecosystem is community-made, unreviewed, and unbounded, so nothing about a
// configured addon can be guaranteed — not its behaviour, not its reachability,
// not what it does with the request. An install that wants streams should not
// have to adopt that whole surface to get them. This module talks to exactly one
// upstream, AIOStreams, which is itself an aggregator: the breadth stays, and the
// trust decision collapses from "every addon a user might add" to "one instance,
// named in settings".
//
// It fills the stream and subtitle roles and nothing else (ADR 0027, ADR 0037).
// It sources no metadata, contributes no search results and declares no catalogs,
// so it never puts a title into the library — it fills in what plays for titles
// some other module described (ADR 0073). An install with only this module and a
// metadata module is exactly the intended shape.
//
// Its own Go module (github.com/mosaic-media/module-aiostreams) importing only
// the published SDK, the published SDUI contract and the standard library, and
// an anti-corruption layer for AIOStreams' dialect of the Stremio addon protocol
// (ADR 0051): the addressing, the release-text conventions and the instance URL
// shape stop here, and the Platform learns none of them.
package aiostreams

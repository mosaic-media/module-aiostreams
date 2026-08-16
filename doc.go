// Package aiostreams is a dedicated stream provider for one named upstream, as
// distinct from module-stremio-addons, which is a host for whatever addons a
// user pastes in.
//
// That distinction is the reason this module exists. The addon ecosystem is
// community-made, unreviewed and unbounded, so nothing about a configured addon
// can be guaranteed, and an install that only wants streams should not have to
// adopt that whole surface to get them. AIOStreams is itself an aggregator, so
// pointing at one instance keeps the breadth and collapses the trust decision to
// a single URL named in settings.
//
// It fills the stream and subtitle roles and nothing else (sdk#2, module-stremio-addons#1).
// It sources no metadata, contributes no search results and declares no catalogs,
// so it never puts a title into the library — it fills in what plays for titles
// some other module described (platform#46). An install with only this module and a
// metadata module is the intended shape.
//
// It is its own Go module (github.com/mosaic-media/module-aiostreams) importing
// only the published SDK, the published SDUI contract and the standard library,
// and an anti-corruption layer for AIOStreams' dialect of the Stremio addon
// protocol (module-stremio-addons#2): the addressing, the release-text conventions and
// the instance URL shape stop here, and the Platform learns none of them.
package aiostreams

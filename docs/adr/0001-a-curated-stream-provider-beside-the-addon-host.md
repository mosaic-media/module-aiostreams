# A curated stream provider beside the addon host

**Status:** Accepted
**Date:** 2026-07-24

Builds on [platform#46](https://github.com/mosaic-media/platform/blob/main/docs/adr/0046-stream-resolution-is-decoupled-from-metadata-provenance.md),
which made a stream provider something the Platform asks rather than something a
ref names. Applies [module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md) per
upstream. Does not supersede [sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)'s
addon browse surface; it sits beside it.

## Context

`module-stremio-addons` is a **host**: a user pastes an addon manifest URL and
Mosaic sources from whatever that addon serves. It has been the only stream
source since the first optional module, and its openness is genuinely the reason
it is useful — the Stremio addon ecosystem is where the streams are.

It is also the reason it cannot be recommended as a default.

**Mosaic has no access-control story that makes an arbitrary addon list safe.**
An addon is a URL a user typed, and everything past that point is unbounded:

- **Nothing constrains what an addon does with a request.** The Platform hands it
  a content id and a User-Agent, and a URL is enough for the addon operator to
  learn that this deployment is looking up that title, at that time.
- **The manifest is the only declaration, and it is self-asserted.** [sdk#4](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0004-module-contributed-settings-ui.md)
  already records that Stremio has no field separating a content source from an
  enhancement addon, which is why the browse grid carries a curated deny-list of
  overlay, watch-party and status addons and a disclaimer covering everything the
  list misses. That disclaimer is honest and it is not a control.
- **The set is unbounded and moves.** A deny-list of sixteen ids against a
  community ecosystem is a best effort by construction, and the register says so.

So the affordance Mosaic offers today for "I would like streams" is: browse a
grid of community-made addons, read a warning, and pick. That is a reasonable
thing to *allow*. It is not a reasonable thing to be the only path.

**AIOStreams changes what the choice costs.** It is an aggregator that speaks the
same protocol: it searches many sources, applies the user's own filters and
sorting, and returns one list through one endpoint. Pointing at it keeps the
breadth of the addon ecosystem while collapsing the trust decision from "every
addon a user might add, individually" to "one instance, named in settings, whose
URL is visible on screen".

## Decision

**A dedicated stream provider is a module per upstream, not a configuration of
the addon host.**

`module-aiostreams` is its own repository and its own Go module, registered
alongside `module-stremio-addons`:

- It fills **`stream`**, **`subtitles`** and **`settings_ui`**, and nothing else.
  It fills no metadata, search or catalog role, so it cannot produce a
  `ContentRef` and cannot put a title into the library. `Import` refuses. It is
  reached only through [platform#46](https://github.com/mosaic-media/platform/blob/main/docs/adr/0046-stream-resolution-is-decoupled-from-metadata-provenance.md)'s enrichment fan-out — titles come from a
  metadata module and this fills in what plays.
- It addresses content by the **shared IMDB identity** and composes an episode id
  at its own boundary. A title it cannot address is declined with an empty
  response and no error, which [platform#46](https://github.com/mosaic-media/platform/blob/main/docs/adr/0046-stream-resolution-is-decoupled-from-metadata-provenance.md) already established as the normal answer.
- **One setting: the instance URL**, defaulting to the public instance ElfHosted
  runs. Everything about which sources are searched, which debrid service is
  used and how results are filtered lives on the instance, behind the user's
  profile — not mirrored into Mosaic.

**The default is a host, not a working configuration, and the module says so.**
AIOStreams serves nothing until a profile exists: the manifest at the bare
instance declares `configurationRequired` with an empty `resources` array. The
module therefore distinguishes three states and renders each differently —
unreachable, reachable-but-unconfigured, serving — because the middle one is
indistinguishable from the first by behaviour alone (no streams) and one click
from the third.

**Precedence is module-id order**, which the enrichment fan-out already uses:
`aiostreams` sorts before `stremio`, so on an install running both, the curated
aggregator is asked first and the addon host is the fallback. This is the
intended order arrived at alphabetically, exactly like the cinemeta/tmdb
ordering, and it is recorded here so the next person does not read it as policy
the Platform holds.

**Duplication between the two modules is accepted.** Both parse the same wire
shapes; neither may import the other. That is [module-stremio-addons#2](https://github.com/mosaic-media/module-stremio-addons/blob/main/docs/adr/0002-modules-as-anti-corruption-layers.md) working as intended — a
module is an anti-corruption layer for *its own* upstream — and the two have
already diverged where it matters: no per-addon routing, no addon catalog, no
candidate sampling, and a token-based release parser here because AIOStreams
lets a user write their own result formatter.

## Alternatives considered

**Add AIOStreams as a recommended entry in the addon browse grid.** Zero new
code: it is a Stremio addon, and the grid already installs addons by URL.
Rejected because it changes nothing about the property that matters. The install
still runs the open host, the settings screen still presents an unbounded list
beside the recommendation, and "we suggest this one" is not a different trust
posture from "here are ninety, at your own risk". It also leaves the
unconfigured-instance state unexplained: pasted into the addon list, the public
instance is an addon that silently serves nothing.

**Give `module-stremio-addons` an allow-list mode.** Keep one module and let a
deployment restrict which addons may be configured. Rejected because the
allow-list is the thing nobody can write: the ecosystem moves, the manifest is
self-asserted, and the deny-list already in that module is documented as
non-exhaustive for exactly that reason. It also makes the safe path a
configuration a user has to know to turn on, which inverts the default.

**A generic "aggregator" module configured by URL, upstream-agnostic.** One
module, several possible upstreams, since they all speak the protocol. Rejected
as the same openness in a smaller box: the value here is that the module knows
*which* upstream it is talking to — that a bare instance means "no profile yet"
rather than "broken", that the path segments are a credential, that the
`/configure` URL is a paste a user will make. A module that could point anywhere
knows none of that and is `module-stremio-addons` with one slot.

**Fold it in as a second core module.** Rejected: [architecture#3](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0003-two-module-tiers.md)'s core tier is the
guarantee clause — what Mosaic is not Mosaic without. Streams are not that; a
metadata-only install is a working Mosaic.

## Consequences

- **A deployment can offer streams without adopting the addon ecosystem.** The
  addon host stays for users who want it; it stops being the only door.
- **The trust surface is a URL a user can read**, and the module never holds a
  debrid credential — the instance does, behind the profile.
- **The instance URL is a credential.** Its path carries the profile id and its
  encrypted password, so anyone holding it holds that user's configuration. It is
  masked in the settings screen and telemetry records only the host. It survives
  whole in the Configure control's action payload, because a link to somebody
  else's configuration page is not a link — the same class of gap [platform#17](https://github.com/mosaic-media/platform/blob/main/docs/adr/0017-module-settings.md) leaves
  open around `configureModule` replacing a whole settings document, reached by a
  different route.
- **Mosaic depends on a third party's default.** The public instance is run by
  ElfHosted, is rate-limited, and can change or go away. That is why the setting
  exists and why the settings screen names who runs the default rather than
  presenting it as Mosaic's own.
- **A settings index became necessary.** The settings host named one module by
  constant, so this module's screen — the only path from installed to resolving —
  would have shipped unreachable. Enumerating `settings_ui` providers fixes it
  here and retroactively for `module-tmdb`, whose credential form was in that
  state.
- **What a stream provider can express is now visibly narrower than what it
  knows.** This module parses container, codec, resolution and swarm health at
  its boundary, and `StreamLink` carries none of the first two while the
  enrichment pass writes only the label, order, location and size — so a Part
  materialised through enrichment loses the fields [platform#27](https://github.com/mosaic-media/platform/blob/main/docs/adr/0027-stream-selection-against-a-client-profile.md)'s playability
  decision reads, and [platform#29](https://github.com/mosaic-media/platform/blob/main/docs/adr/0029-probing-and-the-per-stream-playback-decision.md)'s probe becomes the only source of them. Recorded
  rather than worked around; closing it is an additive SDK bump plus a
  pass-through.

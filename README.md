# module-aiostreams

A **dedicated stream provider** for the [Mosaic](https://github.com/mosaic-media)
platform, sourcing from [AIOStreams](https://github.com/Viren070/AIOStreams).

It is an optional Mosaic module: its own Go module importing only
[`sdk`](https://github.com/mosaic-media/sdk) and
[`sdui`](https://github.com/mosaic-media/contracts), compiled into a Mosaic Platform
binary and invoked through the Platform's capability registry. It owns no schema.

## Why this exists beside `module-stremio-addons`

[`module-stremio-addons`](https://github.com/mosaic-media/module-stremio-addons)
can source from *any* Stremio addon, which is both its value and its problem. The
addon ecosystem is community-made and unreviewed: nothing about an addon a user
pastes in can be guaranteed — not its behaviour, not its reachability, not what
it does with the request — and Mosaic has no access-control story that makes an
open addon list safe to recommend. An install that just wants streams should not
have to adopt that whole surface to get them.

This module talks to exactly **one** upstream. AIOStreams is itself an
aggregator — it searches many sources, applies the user's filters and sorting,
and returns one list — so the breadth is unchanged while the trust decision
collapses from "every addon a user might add" to "one instance URL, shown in
settings".

The two coexist. Both fill the stream role, and the Platform asks stream
providers in module-id order, so `aiostreams` is asked before `stremio` and its
results are the ones attached.

## What it does, and what it deliberately does not

It fills **stream** and **subtitles**, plus its own **settings screen**. It fills
no metadata, search or catalog role, so it never puts a title into the library.
Titles are added from a metadata module (Cinemeta, TMDB) and this fills in what
plays, through the Platform's stream-enrichment pass
([ADR 0073](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0073-stream-resolution-is-decoupled-from-metadata-provenance.md)).

It addresses content by **IMDB id**, which is what Stremio-protocol sources key
on, composing an episode as `<series>:<season>:<episode>` at this boundary. A
title whose IMDB id is unknown is declined — with no error, because being asked
about content it cannot address is normal, not a failure.

## Configuration

One setting: the instance.

```json
{ "instanceUrl": "https://aiostreams.elfhosted.com/stremio/<profile>/<secret>/manifest.json" }
```

Set through the module's settings screen at runtime
([ADR 0021](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0021-module-settings.md)),
not env vars and not Platform config. The base URL, the `/manifest.json` URL, the
`/configure` page URL and a `stremio://` install link are all accepted and
normalise to the same base — with the profile segments preserved, because those
*are* the configuration.

**The default is a host, not a working configuration.** With nothing set the
module points at the public instance ElfHosted runs,
`https://aiostreams.elfhosted.com/stremio`. AIOStreams serves nothing until a
profile exists: that manifest declares `configurationRequired` and an empty
`resources` array, so stream requests against it correctly return nothing. The
settings screen says so in as many words and links straight to the instance's
configuration page; a user creates a profile there and pastes back the manifest
URL it gives them.

Defaulting to it anyway is the point — a fresh install has a named instance and a
working "Configure" button rather than an empty box and a URL to go and find.

Everything else — which sources to aggregate, which debrid service, how to
filter, sort and format results — is configured on the instance, not here.
Mirroring any of it would be two places to change one setting.

**The instance URL is a credential.** Its path carries the profile id and its
encrypted password, so anyone holding the URL holds that user's whole
configuration, including whatever debrid key built it. The settings screen masks
those segments, and telemetry records the instance *host* and never the URL.

## Build and test

**Everything runs in a container; nothing is built or tested on the host.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That is gofmt, `go build`, `go vet` and `go test`. Append `bash` for a shell in
the same environment.

The tests are **hermetic** — a fake AIOStreams over `httptest`, reached by
putting the fake's URL in the module's settings, which needs no seam in the
production type because the instance *is* a setting. There is no instance CI
could point at that is not somebody's, and a profile URL is a credential no CI
should hold.

## A note on AIOStreams and ElfHosted

This is an **unofficial** module. It is not affiliated with, sponsored by or
endorsed by AIOStreams, its author, or ElfHosted. It contains no AIOStreams
source code; it is a client of the publicly documented
[Stremio addon protocol](https://stremio.github.io/stremio-addon-sdk) that
AIOStreams speaks. Mosaic bundles no content and indexes nothing — what a
configured instance returns is the instance owner's business.

## License

MIT (see [`LICENSE`](LICENSE)). It depends only on the Apache-2.0
[Mosaic SDK](https://github.com/mosaic-media/sdk) and
[SDUI contract](https://github.com/mosaic-media/contracts); it may be compiled into a
Mosaic Platform binary under the Platform's Module Linking Exception.

# Claude Instructions — module-aiostreams

A **dedicated stream provider for one named upstream**. It is an *extension*
module
([architecture#3](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0003-two-module-tiers.md)):
nothing requires it, it is **not a dependency of the Platform**, and a Platform
gains it only when a user installs it from the signed registry index
([platform#51](https://github.com/mosaic-media/platform/blob/main/docs/adr/0051-extension-installation-is-user-initiated-and-persistent.md),
[platform#40](https://github.com/mosaic-media/platform/blob/main/docs/adr/0040-module-distribution-and-trust.md)).
`cmd/module-aiostreams` is what serves it out of process
([platform#39](https://github.com/mosaic-media/platform/blob/main/docs/adr/0039-extension-module-boundary.md),
[sdk#7](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0007-go-plugin-as-the-extension-harness.md)).

## Read this first: why it exists beside `module-stremio-addons`

The two look redundant and are not.
[`module-stremio-addons`](https://github.com/mosaic-media/module-stremio-addons)
is a **host** for whatever addons a user pastes in; this is a **client of one
named upstream**. That difference is the whole reason for the repository: the
addon ecosystem is community-made and unreviewed, Mosaic has no access-control
story that makes an open addon list safe to recommend, and an install that only
wants streams should not have to adopt that surface to get them. AIOStreams is
itself an aggregator, so pointing at one instance keeps the breadth and collapses
the trust decision to a single URL a user can read
([module-aiostreams#1](docs/adr/0001-a-curated-stream-provider-beside-the-addon-host.md)).

**Do not "unify" them.** A shared Stremio-protocol client between two modules is
not available to build — `boundary_test.go` fails a `github.com/mosaic-media/module-`
import outright, with "modules compose through the Platform, never with each
other" — and it is not wanted either. Each module is an anti-corruption layer for
*its own* upstream, and the small overlap in wire shapes is the price of that,
not a defect. They already differ where it matters: this one has no per-addon
routing, no addon catalog, no candidate sampling, and its release parser is
**token-based** rather than regex-based, because AIOStreams lets a user write
their own result formatter and a scan for whole tokens degrades into "found
nothing" on an unfamiliar format instead of matching the wrong thing.

## Do not let it grow into a source

It fills **stream**, **subtitles** and **settings UI**, and must acquire no
metadata, search or catalog role. It has no way to make a `ContentRef`, and
`Import` **refuses on purpose** rather than returning an empty success — titles
come from a metadata module and this fills in what plays
([platform#46](https://github.com/mosaic-media/platform/blob/main/docs/adr/0046-stream-resolution-is-decoupled-from-metadata-provenance.md)).

AIOStreams *does* serve catalogs, and adding that role is exactly the change that
would turn this into a second content source; it is left out deliberately.

**An empty response with no error is a normal answer here, not a degraded one.**
This module is only ever called about content some other module sourced, so an
identity it cannot address, an instance with no profile and an upstream with
nothing for that title all end the same way. Erroring instead would fail a user's
import over a title that was never this module's to know.

## Three things about the settings, and each has bitten something already

- **The default instance is a host, not a working configuration.** The public
  ElfHosted instance declares `configurationRequired` and an empty `resources`
  array until a user creates a profile — which is why `Configured` is computed
  from both, not from reachability. Every path here has to treat "reachable but
  unconfigured" as its own state: it looks identical to broken from the outside
  — no streams — while being one click from working, and a screen that conflated
  them would teach a user the module does not work.
- **The instance URL is a credential.** Its path carries the profile id and its
  encrypted password; anyone holding it holds that user's whole configuration,
  including whatever debrid key built it. `maskInstanceURL` keeps the host and
  the short readable route segments and reduces anything long enough to be an
  opaque id to its last four characters — enough to tell two profiles apart,
  useless to anyone else. A settings screen is a page people screenshot when
  asking for help.

  **What that does not cover, and cannot:** the Configure button's action carries
  the whole URL, because a link to somebody else's configuration page is not a
  link. So the credential is absent from everything *rendered* and present in an
  action payload, out of reach of the Platform's redaction classes
  ([platform#34](https://github.com/mosaic-media/platform/blob/main/docs/adr/0034-redaction-classes-are-the-pii-boundary.md)),
  which cannot see inside a module's settings. It is written down rather than
  papered over, and the test that covers it asserts over rendered **text** rather
  than over the whole payload — deliberately, so it does not turn into a test
  that forbids the link.
- **Normalisation trims a suffix, never a path.** `normaliseInstanceURL` strips a
  trailing `/manifest.json` or `/configure` and accepts the `stremio://` scheme;
  the profile segments *before* those suffixes are the configuration and are
  preserved. A normaliser that dropped the path would silently turn a working URL
  into the unconfigured public instance, which fails as "no streams" rather than
  as an error — the worst possible failure mode here.

## Keep one setting

`configureModule` **replaces** the settings document; there is no merge. With one
field that costs nothing, because the document *is* the value being changed. A
second setting would drag the echo-the-secret pattern in here, and it is not
needed: everything about how AIOStreams behaves — which addons to aggregate,
which debrid service, how to filter, sort and format — lives on the instance,
behind the profile the URL names. Mirroring any of it would mean two places to
change one setting and a module that goes stale every time AIOStreams grows an
option.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.** This repository's gate runs inside its test container:

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in `docker-compose.test.yml`, which must stay equal to the one
in `go.mod`. `.github/workflows/verify.yml` runs the same four steps — **keep the
two in step.** Append `bash` for a shell in the same environment.

The container asserts **the boundary**: this module compiles against the
published SDK, the published SDUI contract and the standard library and nothing
else. A host with a populated module cache, a leftover `go.work` or a stray
`replace` can satisfy an import a third party's machine could not, and
`boundary_test.go` still passes because the import resolved.

**The tests are hermetic and must stay that way** — a fake AIOStreams over
`httptest`, reached by putting the fake's URL in the module's settings. That
needs no seam in the production type, because the instance is a setting rather
than a constant. There is no instance CI could point at that is not somebody's,
and a profile URL is a credential no CI should hold: a test against a real
instance would be skipped, flaky, or a leak. **A test that starts needing egress
is a design question, not a compose-file change.**

## Versioning and release

A change is a **minor** bump, tagged and pushed. **Nothing bumps a `require`
afterwards** — the Platform does not depend on this module, so there is no
version line anywhere to move. A release reaches people through the
**catalogue**: `release.yml` proves the tag resolvable through the public proxy,
its `binaries` job cross-compiles and assembles a `manifest.json` carrying each
binary's digest, and its `dispatch` job tells the registry there is a new version
to list.

Two things about that chain, both already got wrong once:

- **`dispatch` waits on `binaries`, not just `release`.** The registry catalogues
  a release by downloading `manifest.json` from its assets, so an earlier
  dispatch would point the catalogue at a release whose assets are still
  uploading, and the entry would be refused for a module that is fine.
- **A missing dispatch token fails rather than warns.** It used to exit 0, so an
  unset token reported green while nothing was ever sent. The tag and the
  binaries already exist by then, so a red run costs nothing that can be undone
  and is the only way a broken chain becomes visible.

Warm the Go proxy after tagging anyway — anything building this from source
resolves it as an ordinary Go module, and the proxy and checksum database are
eventually consistent with a just-pushed tag:

```bash
curl -s "https://proxy.golang.org/github.com/mosaic-media/module-aiostreams/@v/v0.1.0.info"
```

**A `replace` must never land in a commit.** The module reports the version that
was **actually linked**, via `v1.ModuleVersion` reading the build graph — not a
hand-maintained constant, which nothing forces to agree with anything.

## Decision records

[`docs/adr/README.md`](docs/adr/README.md) is the generated index of the records
this repository owns, with each one's status. **Read the index rather than
counting files, and do not restate a status here** — it is generated from the
records and this file is not.

The index script and the citation lint that
[`architecture`](https://github.com/mosaic-media/architecture) owns for the fleet
are **not vendored here**, so nothing in this repository's gate checks that the
index is current or that a citation resolves. Until they are, both are on you:
regenerate the index when you add a record, and write every citation as a
`repo#N` link.

## Modules are the forcing function for the SDK

When something cannot be expressed, that is a **finding**, not an obstacle to
work around. Take it to the SDK as an additive bump, or record it in the roadmap
as an open gap. **Do not simulate the missing surface locally**, and in
particular **do not attach Parts directly from this module** to get a field
through — that would make it a content source and duplicate the enrichment pass's
idempotence rules.

**Which side a finding lands on is not arbitrary.** The SDK says how a module
interacts with the Platform; the Platform holds the implementations. A gap closed
by a type or a verb is an SDK bump — a field is a shape. A gap that can only be
closed by moving values through a Platform pass is behaviour, so it is a Platform
change reached through a declarative surface. That is the ordinary division, not
a consolation prize for the half that did not fit.

**Check whether a gap is still open before repeating it.** Both of this module's
past findings were about somebody else's repository — one about the SDK's request
shapes, one about the Platform's enrichment pass — and a gap written down here
stays written down long after the other repository closes it. The roadmap and the
owning repository's records are where the current answer lives.

## Observability

Observability goes through the SDK's ambient `v1.Telemetry`
([sdk#5](https://github.com/mosaic-media/sdk/blob/main/docs/adr/0005-modules-observe-through-the-sdk.md)),
reached as `TelemetryFrom(ctx)`. **Do not print** — go-plugin writes its
handshake on stdout and anything else corrupts it — and do not configure an
exporter, a sink or retention; the Platform owns the observability plane. **The
instance URL is never written to telemetry; the host is.**

## Licensing

**MIT**, unlike the Platform's AGPL
([architecture#1](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0001-licensing.md)).
Files here carry **no SPDX header** — match the files already present.

<!-- shared-rules:begin -->
<!-- shared-rules:end -->

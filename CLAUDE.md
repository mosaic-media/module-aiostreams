# Claude Instructions — module-aiostreams

A **dedicated stream provider** for one named upstream. It is an *extension*
module ([ADR 0062](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0062-two-module-tiers.md)),
like `module-stremio-addons` and unlike the core metadata modules — nothing
requires it, and an install without it still works.

## Read this first: why it exists beside `module-stremio-addons`

The two look redundant and are not. `module-stremio-addons` is a **host** for
whatever addons a user pastes in; this is a **client of one named upstream**.
That difference is the whole reason for the repository: the addon ecosystem is
community-made and unreviewed, Mosaic has no access-control story that makes an
open addon list safe to recommend, and an install that only wants streams should
not have to adopt that surface to get them. AIOStreams is itself an aggregator,
so pointing at one instance keeps the breadth and collapses the trust decision to
a single URL a user can read.

**Do not "unify" them.** A shared Stremio-protocol client between two modules is
not available to build — a module may not import another module, and
`boundary_test.go` fails on it — and it is not wanted either. Each module is an
anti-corruption layer for *its own* upstream
([ADR 0051](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0051-modules-as-anti-corruption-layers.md)),
and the small overlap in wire shapes is the price of that, not a defect. They
already differ where it matters: this one has no per-addon routing, no addon
catalog, no candidate sampling, and its release parser is token-based because
AIOStreams lets a user write their own result formatter.

## Do not let it grow into a source

It fills **stream**, **subtitles** and **settings UI**, and must acquire no
metadata, search or catalog role. It has no way to make a `ContentRef` and
`Import` refuses on purpose — titles come from a metadata module and this fills
in what plays ([ADR 0073](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0073-stream-resolution-is-decoupled-from-metadata-provenance.md)).
AIOStreams *does* serve catalogs, and adding that role is exactly the change that
would turn this into a second content source; it is left out deliberately.

## Three things about the settings, and each has bitten something already

- **The default instance is a host, not a working configuration.** The public
  ElfHosted instance declares `configurationRequired` and an empty `resources`
  array until a user creates a profile. Every path here has to treat "reachable
  but unconfigured" as its own state: it looks identical to broken from the
  outside — no streams — while being one click from working, and a screen that
  conflated them would teach a user the module does not work.
- **The instance URL is a credential.** Its path carries the profile id and its
  encrypted password; anyone holding it holds that user's whole configuration,
  including whatever debrid key built it. It is masked in the settings screen,
  never written to telemetry (the *host* is), and never repeated in an error.
  The one place it survives whole is the Configure button's action payload,
  because a link to somebody else's configuration page is not a link — that is
  recorded on `maskInstanceURL` and covered by a test that asserts over rendered
  *text* rather than the whole payload.
- **Normalisation trims a suffix, never a path.** The profile segments are the
  configuration. A normaliser that dropped the path would silently turn a working
  URL into the unconfigured public instance, which fails as "no streams" rather
  than as an error — the worst possible failure mode here.

## Keep one setting

`configureModule` **replaces** the settings document; there is no merge
(`module-tmdb` records the gap). With one field that costs nothing, because the
document *is* the value being changed. A second setting would drag the
echo-the-secret pattern in here, and it is not needed: everything about how
AIOStreams behaves belongs on the instance, behind the profile.

## Everything runs in the container, nothing runs on the host

**Do not run `go build`, `go test`, `go vet` or `gofmt` directly on this
machine.**

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That runs gofmt, `go build ./...`, `go vet ./...` and `go test ./...` against the
Go version pinned in the compose file, which must stay equal to the one in
`go.mod`. Append `bash` for a shell in the same environment.

The container asserts **the boundary**: this module compiles against the
published SDK, the published SDUI contract and the standard library and nothing
else, enforced by `boundary_test.go` parsing every import. A host with a
populated module cache, a leftover `go.work` or a stray `replace` can satisfy an
import a third party's machine could not.

**The tests are hermetic and must stay that way** — a fake AIOStreams over
`httptest`, reached by putting the fake's URL in the module's settings. That
needs no seam in the production type, because the instance is a setting rather
than a constant. There is no instance CI could point at that is not somebody's,
and a profile URL is a credential no CI should hold. This is the opposite of
`module-stremio-addons`, whose container has egress on purpose; do not copy that
compose file's reasoning over.

## Versioning and release

The Platform requires this at a **tagged version with no `replace`** — a
`replace` must never land in a commit. A change is a minor bump, tagged and
pushed, then the Platform's `go.mod` require is bumped to match.

```bash
git tag v0.2.0 && git push origin main && git push origin v0.2.0
```

Pushing the tag is the whole publish. **For a newly published module, warm the
proxy before bumping the Platform**, or the Platform's build fails with a
`sum.golang.org` error that reads like a GitHub credentials problem:

```bash
curl -s "https://proxy.golang.org/github.com/mosaic-media/module-aiostreams/@v/v0.1.0.info"
```

The module reports the version that was **actually linked**, via
`v1.ModuleVersion` reading the build graph — not a hand-maintained constant,
which nothing forces to agree with anything.

## Workflow

- Commit and push this repository **separately** from `platform`.
- **Commit author identity** must be `AdamNi-7080 <anicholls41@gmail.com>`.
- The test container green before pushing.
- Observability goes through the SDK's ambient `v1.Telemetry`
  ([ADR 0059](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0059-modules-observe-through-the-sdk.md)),
  reached as `TelemetryFrom(ctx)`. Do not print, and do not configure an
  exporter, a sink or retention — the Platform owns the observability plane.
- **MIT-licensed**, unlike the Platform's AGPL
  ([ADR 0022](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0022-licensing.md)).
  Files here carry no SPDX header — match the files already present.

## Modules are the forcing function for the SDK

When something cannot be expressed, that is a **finding**, not an obstacle to
work around. Take it to the SDK as an additive `v0.x` bump, or record it in the
roadmap as an open gap. **Do not simulate the missing surface locally.** Two
were open from here and both were found this way; SDK `v0.26.0` closed one of
them and half of the other.

**Which side a finding lands on is not arbitrary.** The SDK says how a module
interacts with the Platform; the Platform holds the implementations. The SDK
therefore names no library and depends on nothing
([ADR 0135](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0135-the-sdk-carries-no-implementation.md)).
The second gap below is the worked example: the field half was an SDK bump
because a field is a shape, and the half still open is a Platform pass-through
because moving values through the enrichment pass is behaviour. That is the
ordinary division, not a consolation prize for the half that did not fit.

- **`SubtitlesRequest` carries no season or episode — closed in SDK `v0.26.0`.**
  `StreamRequest` grew them for
  [ADR 0073](https://github.com/mosaic-media/architecture/blob/main/docs/adr/0073-stream-resolution-is-decoupled-from-metadata-provenance.md)
  and the subtitles request did not, because nothing consumed subtitles yet, so
  this module could resolve subtitles for a film and for nothing else —
  `addressOf` was called with two literal zeroes and there was nothing to compose
  an episode id from. `Subtitles` now passes the request's own coordinates
  through, and `TestSubtitlesComposesTheEpisodeAddress` pins it against a fake
  that answers only the composed path.

- **Most of what `parseRelease` works out never reaches the Part — half closed.**
  This was *two* gaps stacked, and only the first is gone.

  The SDK half is closed: `StreamLink` gained `Container`, `VideoCodec` and
  `AudioCodec` in `v0.26.0`, so `streamLinkFrom` no longer narrows the parse to
  quality, size and seeders on the way out of `Streams`.

  **The Platform half is still open.** Its enrichment pass attaches only the
  edition label, the natural order, the location and the size, so everything
  else is dropped before the Part is written
  (`internal/platform/app/enrich_streams.go`, `attachResolvedStreams`).
  `AttachContentPartCommand` holds every one of those fields — a module
  attaching its own Parts fills them, which is why `module-stremio-addons` does —
  but a provider answering the enrichment pass still has no route to them, and
  now the reason is a missing pass-through rather than a missing field.

  It matters because container and codec are exactly what ADR 0048's playability
  decision reads, and an empty field there is not neutral: the same fields left
  empty once had the Platform relay ten gigabytes of Matroska to a browser.
  **Do not work around it here** — do not attach Parts directly from this module
  to get the fields in, which would make it a content source and duplicate the
  enrichment pass's idempotence rules.

## The roadmap and the decision records

These rules are identical in every Mosaic repository. They exist because the
state of the build and the reasons behind it are the two things that rot fastest
and report nothing when they do — no build fails, no test goes red.

### The roadmap is maintained, not consulted

**`docs/roadmap.md` in [`architecture`](https://github.com/mosaic-media/architecture)
is the single record of where the build is.** Read it before starting work, and
**update it in the same session as the change that dates it** — not in a
follow-up, which does not happen.

- **A slice that lands is marked landed, with what was left out.** "Built" with
  no qualifier is a claim that the whole slice shipped; if part of it did not,
  say which part and why in the same sentence.
- **Implementation that departs from the plan is recorded where it departed.**
  The roadmap is derived from the code, not from the intention that preceded it,
  and the surprises are the most valuable thing in it.
- **Do not restate the roadmap here.** A second copy of "what is built" in a
  `CLAUDE.md` is how the first copy goes stale unnoticed.
- **A capability with no client path is not done — it is
  [owed](https://github.com/mosaic-media/architecture/blob/main/docs/unreachable-capability.md).**
  If you delete or fail to build a client path to a working service, add its row
  to that register in the same change.

### Decision records are append-only

An ADR is an account of what was decided and why, at a time. It is evidence, not
documentation, and its value is that it was not edited afterwards.

- **Never rewrite a record's body to match what was built.** Not to correct it,
  not to annotate it, not to add "as built, this differs".
- **State changes in the `**Status:**` line, and nowhere else** — built, built in
  part (naming the part), or superseded, wholly or partly.
- **A changed decision needs a new record that supersedes it**, with its own
  Context / Decision / Alternatives / Consequences. Both records then stand.
- **An unbuilt decision is not a superseded one.** "Not done yet" belongs in the
  Status line and the roadmap.
- **Records live only in `architecture/docs/adr/`**, numbered sequentially in
  kebab-case. Adding one means adding it to `nav:` in `mkdocs.yml`, and
  `mkdocs build --strict` must pass.

**If the code and a record disagree, say so rather than quietly picking one.** An
honest "this is unresolved" is worth more than a plausible reconciliation that
reads as settled.

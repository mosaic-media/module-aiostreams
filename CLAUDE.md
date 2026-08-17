# Claude Instructions — module-aiostreams

Fleet-wide conventions — commits, decision records, citation form, the roadmap —
are in [`architecture`](https://github.com/mosaic-media/architecture/blob/main/CLAUDE.md).
This file is what is specific to `module-aiostreams`.

This repository is a **client of one named upstream**: a single AIOStreams
instance, named by a URL in the module's settings. AIOStreams is itself an
aggregator, so the breadth lives on the instance and the trust decision here is
one URL a user can read. **It is not a Stremio addon host** — that is
[`module-stremio-addons`](https://github.com/mosaic-media/module-stremio-addons)
— and the two are not to be unified: `boundary_test.go` fails a
`github.com/mosaic-media/module-` import outright, and each module is an
anti-corruption layer for its own upstream.

It is an **extension module**: nothing requires it, and a Platform gains it only
when a user installs it from the signed registry index — see Release below, and
`cmd/module-aiostreams`, which serves it out of process. **`README.md` still says
it is compiled into a Platform binary; that sentence is stale.**

## What it declares, and what it refuses

`Capability.Manifest` in `capability.go` declares `RoleStream`, `RoleSubtitles`
and `RoleSettingsUI`, and the compile-time assertions above it fail the build if
a declared role loses its method.

- **Do not let it grow into a source.** No metadata, search or catalog role, so
  it can never put a title into the library. AIOStreams does serve catalogs, and
  adding that role is the change that would make this a second content source.
- **`Import` refuses with an error** rather than returning an empty success:
  nothing can hand this module a ref it produced, so a caller routing an import
  here has made a mistake. `TestImportIsRefused` pins it.
- **An empty response with no error is a normal answer** from `Streams` and
  `Subtitles` — an identity that is not an IMDb id, an instance with no profile,
  and an instance whose sources have nothing all end the same way, and erroring
  would fail a user's import over a title that was never this module's to know.

## The boundary

`boundary_test.go` parses every non-test import and allows only the standard
library, `sdk/…` and `contracts/…` — the SDUI exemption is there because this
module authors its own settings screen. Platform, another module and any
third-party import all fail.

**It reads this directory only; it does not walk the tree.**
`cmd/module-aiostreams/main.go` imports `sdk/host` and is therefore *not*
covered, so a dependency added under `cmd/` is one nothing here catches — adding
a package outside the root means widening that read in the same change. That
command file is one line by design: the module builds and behaves identically
whether or not it is used, and `aiostreams.New` stays what a host calls.

## Settings: one field, and it is a credential

`moduleSettings` has one field, `instanceUrl`, and it stays one field.
`configureModule` replaces the whole settings document with no merge, so with a
single field the document *is* the value a control is changing and nothing has to
be echoed back through the client. Everything about how AIOStreams behaves —
which addons it aggregates, which debrid service, how it filters, sorts and
formats — lives on the instance behind the profile the URL names.

**The default instance is a host, not a working configuration.** An unconfigured
AIOStreams declares `configurationRequired` with an empty `resources` array, so
`InstanceInfo.Configured` is computed from the *manifest* — `declaresResource`
plus that flag — and never from reachability. Treat "reachable but unconfigured"
as its own state: from outside it looks identical to broken (no streams) while
being one click from working. That manifest shape is a claim about a live service
which the fake asserts rather than checks, so read the real document when
changing what `Manifest` decodes (`DefaultInstance` plus the suffix):

```bash
curl -sSL https://aiostreams.elfhosted.com/stremio/manifest.json | python3 -m json.tool
```

**The instance URL is a credential.** Its path carries the profile id and its
encrypted password, so anyone holding it holds that user's whole configuration,
including whatever debrid key built it.

- **`normaliseInstanceURL` trims a suffix, never a path.** It accepts
  `stremio://`, drops a query or fragment, and strips a trailing
  `/manifest.json` or `/configure` — the profile segments before those suffixes
  are preserved, and a path containing `configure` earlier survives (both are
  pinned in `aiostreams_internal_test.go`). A normaliser that dropped the path
  would silently turn a working URL into the unconfigured public instance, which
  fails as "no streams" rather than as an error.
- **`maskInstanceURL` keeps the host and the short readable route segments** and
  reduces anything longer than eight characters to `••••` plus its last four — a
  settings screen is a page people screenshot when asking for help.
- **The Configure button's action necessarily carries the whole URL**, because a
  link to somebody else's configuration page is not a link. So its test asserts
  over rendered **text**, not the whole payload; keep it that way or it becomes a
  test that forbids the link.
- **Telemetry records `instanceHost(...)`, never the URL**, and `getJSON`'s error
  reports the status without it for the same reason.

## Reading the upstream, and reaching it

- **The release parser is token-based, not regex-based.** AIOStreams lets a user
  write their own result formatter, so `tokenise` + the `find*` helpers scan for
  whole tokens: an unfamiliar layout degrades into "found nothing" where a
  pattern anchored on one formatter's punctuation would match the wrong span and
  answer confidently wrong. Leave a field empty rather than guessing.
- **Fill the typed fields anyway.** `Container`, `VideoCodec` and `AudioCodec` on
  the `StreamLink` are what a playability decision reads, and this module reaches
  the library only through the enrichment pass, so that link is their only route.
- **The User-Agent is load-bearing for reachability**, not courtesy: `getJSON`
  sets it on every request because the public instance is Cloudflare-fronted and
  answers Go's default `Go-http-client/1.1` with a 403. No test asserts it.

## The gate

```bash
docker compose -f docker-compose.test.yml run --rm test
```

That is the record-index check, the citation lint, gofmt, `go build`, `go vet`
and `go test`, against the Go version pinned in the compose file — keep that
version equal to `go.mod`'s. Append `bash` for a shell in the same environment.
`.github/workflows/verify.yml` runs the same checks on a `setup-go` runner and is
what refuses a push; keep the two in step. Do not run any of them on the host: a
populated module cache, a leftover `go.work` or a stray `replace` can satisfy an
import a third party's machine could not, and `boundary_test.go` passes anyway
because the import resolved.

**The tests are hermetic** — a fake AIOStreams over `httptest`, reached by
putting the fake's URL in the module's settings, which needs no seam in the
production type because the instance *is* a setting. There is no instance CI
could point at that is not somebody's, and a profile URL is a credential no CI
should hold; a test that starts needing egress is a design question, not a
compose-file change.

## Release

A change is a minor bump, tagged and pushed; **a `replace` must never land in a
commit**, and the version is read from the build graph by `v1.ModuleVersion`
rather than held in a constant. Nothing bumps a `require` afterwards.
`release.yml` reuses `verify.yml`, proves the tag resolvable through the public
proxy, cross-compiles binaries and a `manifest.json` in `binaries`, then
dispatches `module-released` at `mosaic-media/registry`. **`dispatch` needs
`[release, binaries]`**, because the registry catalogues by downloading that
`manifest.json` from the release assets; it fails rather than warns when
`REGISTRY_DISPATCH_TOKEN` is unset.

## Records, licence, observability

[`docs/adr/README.md`](docs/adr/README.md) is the generated index of the records
this repository owns; read it rather than counting files, and never hand-edit it.
`scripts/adr_index.py` and `scripts/adr_lint.py` are **vendored** from
`architecture/scripts/` and run by this gate — change them there and re-vendor.

MIT-licensed; files carry **no SPDX header**. Observability goes through
`v1.TelemetryFrom(ctx)`, and **nothing may be written to stdout**, where
go-plugin's handshake lives.

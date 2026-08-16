package aiostreams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

const (
	// CapabilityID is the id the Platform registers this module under and a
	// caller names to reach its settings screen.
	//
	// It sorts before "stremio", which decides precedence: the Platform resolves
	// stream providers in module-id order and stops at the first that answers
	// (platform#46), so on an install running both, AIOStreams is asked first and
	// its releases are the ones attached. That is the intended order — a single
	// curated aggregator ahead of an open-ended addon list — but it is
	// alphabetical accident rather than a policy the Platform holds, exactly like
	// the cinemeta/tmdb ordering beside it.
	CapabilityID = "aiostreams"
	// modulePath is this module's import path, which is how it reads its own
	// version out of the build graph rather than carrying a constant nothing
	// forces to stay true.
	modulePath = "github.com/mosaic-media/module-aiostreams"
	// streamProvider names the resolving service recorded on a RemoteLocation
	// Part. The binding records where the reference came from; the bytes are
	// resolved later, by a playback module (platform#25).
	streamProvider = "aiostreams"
	// imdbScheme is the shared external identity this module can address content
	// by. AIOStreams keys on Stremio ids, which are IMDB ids for film and
	// television, so an imdb identity is directly usable and nothing else is.
	imdbScheme = "imdb"
)

// moduleVersion is resolved once from the build graph — a fact about the binary,
// discovered at startup, not a literal somebody has to remember to bump.
var moduleVersion = v1.ModuleVersion(modulePath)

// Capability satisfies the SDK's capability contract and every provider role it
// declares. The assertions fail to compile if the module drifts from a role it
// claims to fill (sdk#2).
var (
	_ v1.Capability         = (*Capability)(nil)
	_ v1.StreamProvider     = (*Capability)(nil)
	_ v1.SubtitlesProvider  = (*Capability)(nil)
	_ v1.SettingsUIProvider = (*Capability)(nil)
)

// Capability is the AIOStreams module: a dedicated stream provider for one named
// upstream.
//
// What it does not fill is as much the shape as what it does. It declares no
// metadata, search or catalog role, so it can never put a title into the library
// or offer one in a search — an install adds titles through a metadata module
// and this fills in what plays (platform#46). That is a narrower surface than
// module-stremio-addons, which hosts whatever a user pastes in and therefore
// inherits whatever those addons do; here the trust decision is a single
// instance URL a user can read.
//
// It holds an HTTP client and a manifest cache. The instance it points at is a
// user-managed setting handed in per invocation (platform#17), so one registered
// module serves whatever each install configured.
type Capability struct {
	httpClient *http.Client
	manifests  *manifestCache
}

// New builds the capability over an HTTP client (nil for a default). The
// instance URL is not supplied here — it arrives as settings on each invocation.
//
// The Platform passes its own client, which routes through the dial guard that
// stops a user-supplied URL reaching the deployment's private network. A module
// that built its own would bypass it, which is precisely the seam this module
// sits on: the instance URL is text a user typed.
func New(httpClient *http.Client) *Capability {
	return &Capability{httpClient: httpClient, manifests: newManifestCache()}
}

// moduleSettings is the shape this module reads from its settings document.
//
// One field, and it stays one field. AIOStreams' own configuration — which
// addons to aggregate, which debrid service, how to filter, sort and format —
// lives on the instance, behind the profile the URL names. Mirroring any of it
// here would mean two places to change one setting and a module that goes stale
// every time AIOStreams grows an option.
type moduleSettings struct {
	// InstanceURL is the manifest URL of the AIOStreams instance, including the
	// profile segments that make it a working configuration. Empty means the
	// public default.
	InstanceURL string `json:"instanceUrl"`
}

// settingsFrom parses the module's settings document. An unset or blank instance
// URL is not an error — it is a fresh install, and it means the default.
func settingsFrom(settings []byte) (moduleSettings, error) {
	if len(settings) == 0 {
		return moduleSettings{}, nil
	}
	var s moduleSettings
	if err := json.Unmarshal(settings, &s); err != nil {
		return moduleSettings{}, fmt.Errorf("parse module settings: %w", err)
	}
	s.InstanceURL = strings.TrimSpace(s.InstanceURL)
	return s, nil
}

// instanceFrom resolves the settings document to the instance base URL requests
// are built from, falling back to the public default.
func instanceFrom(settings []byte) (string, error) {
	s, err := settingsFrom(settings)
	if err != nil {
		return "", err
	}
	if base := normaliseInstanceURL(s.InstanceURL); base != "" {
		return base, nil
	}
	return DefaultInstance, nil
}

// clientFrom builds a client for the configured instance.
//
// There is no "nothing configured" error path: this module always has an
// instance, because it ships with one. What it may not have is a configured
// instance, and that is not an error either — the manifest says so and the
// provider roles decline, which is the honest answer to "what do you have for
// this title" when the answer is nothing.
func (c *Capability) clientFrom(settings []byte) (*Client, error) {
	base, err := instanceFrom(settings)
	if err != nil {
		return nil, err
	}
	return newClient(c.httpClient, base, c.manifests), nil
}

// Manifest is the module's self-declaration (sdk#2): the two source roles it
// fills, and the settings screen that configures them.
func (c *Capability) Manifest() v1.Manifest {
	return v1.Manifest{
		ID: CapabilityID, Version: moduleVersion, Name: "AIOStreams",
		Description: "An independent, community-built aggregator: it searches many sources at once " +
			"and returns one filtered, sorted list. Point Mosaic at a public or self-hosted instance, " +
			"and everything about which sources it searches and which debrid service it uses is " +
			"configured on that instance.",
		Provides: []v1.Role{v1.RoleStream, v1.RoleSubtitles, v1.RoleSettingsUI},
	}
}

// Import is refused, and that refusal is the module's shape rather than an
// unimplemented corner.
//
// Import materialises a ref a read role produced (platform#18). This module fills
// no read role that produces one: it has no search, no catalogs and no metadata,
// so nothing can hand it a ref it made, and materialising from a stream listing
// would mean inventing a title out of release names. Titles come from a metadata
// module; this fills in what plays.
func (c *Capability) Import(_ context.Context, _ v1.ContentService, _ v1.ImportRequest) (v1.ImportResult, error) {
	return v1.ImportResult{}, fmt.Errorf("%s is a stream provider and imports no content; add the title from a metadata source and this module fills in its streams", CapabilityID)
}

// Streams resolves playable locations for a materialised item (RoleStream).
//
// It is called by the Platform's enrichment pass for content some other module
// sourced (platform#46), which is the only way it is ever called: a ref this module
// produced does not exist. So the ref carries a shared external identity, and
// addressOf decides whether it is one AIOStreams can speak.
//
// Returning an empty response with no error is a normal answer here, not a
// degraded one. Three ordinary situations produce it — an identity that is not
// an IMDB id, an instance with no profile configured yet, and a title the
// instance's sources simply do not have — and erroring on any of them would fail
// a user's import over a title that was never this module's to know.
func (c *Capability) Streams(ctx context.Context, req v1.StreamRequest) (v1.StreamResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.StreamResponse{}, err
	}
	typ, id, ok := addressOf(req.Ref, req.Season, req.Episode)
	if !ok {
		return v1.StreamResponse{}, nil
	}

	streams, err := client.Streams(ctx, typ, id)
	if err != nil {
		return v1.StreamResponse{}, fmt.Errorf("resolve streams: %w", err)
	}
	if len(streams) == 0 {
		return v1.StreamResponse{}, nil
	}

	// The instance is named but its URL is not: the path carries the user's
	// profile id, which is a credential in the same sense a session token is —
	// anyone holding it holds that user's configuration, including whatever
	// debrid key it was built with. The host is the useful half for diagnosis and
	// is safe (platform#34, sdk#5).
	v1.TelemetryFrom(ctx).Info("aiostreams resolved streams",
		v1.String("instance_host", instanceHost(client.Base())),
		v1.String("native_type", typ),
		v1.Int("candidates", len(streams)))

	out := make([]v1.StreamLink, 0, len(streams))
	for _, s := range streams {
		out = append(out, streamLinkFrom(s))
	}
	return v1.StreamResponse{Streams: out}, nil
}

// streamLinkFrom maps one AIOStreams result to the SDK's StreamLink, carrying
// the release detail parsed at this boundary so a consumer ranks on typed fields
// rather than re-deriving them from a URL (module-stremio-addons#1, module-stremio-addons#2).
func streamLinkFrom(s Stream) v1.StreamLink {
	d := parseRelease(s)
	return v1.StreamLink{
		// Name is AIOStreams' short label — the service and resolution badge it
		// puts in the first line of a result.
		Label: s.Name,
		// Title is the release name, which is what a source picker shows someone
		// choosing between two candidates by hand.
		Title:     releaseLabel(s),
		Quality:   d.quality,
		SizeBytes: d.sizeBytes,
		Seeders:   d.seeders,
		Location:  v1.MediaLocation{Scheme: v1.RemoteLocation, Provider: streamProvider, Ref: s.Ref()},
		// These three are what platform#27's playability decision reads, and this
		// module reaches the library only through the enrichment pass
		// (platform#46), so a StreamLink is the only route they have. An empty one
		// is not neutral: it is how ten gigabytes of Matroska reach a browser that
		// cannot decode it.
		Container:  d.container,
		VideoCodec: d.videoCodec,
		AudioCodec: d.audioCodec,
	}
}

// Subtitles resolves subtitle tracks for an item (RoleSubtitles, module-stremio-addons#1). Like
// Streams it is a source role and the consumer is a player; it returns an empty
// response, no error, when the instance serves none.
func (c *Capability) Subtitles(ctx context.Context, req v1.SubtitlesRequest) (v1.SubtitlesResponse, error) {
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.SubtitlesResponse{}, err
	}
	// The request's own coordinates, not zeroes: this module is only ever reached
	// about content it did not source (platform#46), so the ref carries a shared
	// identity and no native id, and addressOf composes an episode id out of the
	// season and episode numbers. Without them this can answer for a film and
	// nothing else. The dialect stays in addressOf.
	typ, id, ok := addressOf(req.Ref, req.Season, req.Episode)
	if !ok {
		return v1.SubtitlesResponse{}, nil
	}
	subs, err := client.Subtitles(ctx, typ, id)
	if err != nil {
		return v1.SubtitlesResponse{}, fmt.Errorf("resolve subtitles: %w", err)
	}
	out := make([]v1.Subtitle, 0, len(subs))
	for _, s := range subs {
		out = append(out, v1.Subtitle{Language: s.Lang, URL: s.URL, ID: s.ID})
	}
	return v1.SubtitlesResponse{Subtitles: out}, nil
}

// addressOf works out how to ask the instance about the item a request names.
//
// This is the anti-corruption layer doing its job (module-stremio-addons#2, platform#46). The ref
// arrives describing a title some other module sourced, carrying whatever shared
// identities that module wrote. Only an IMDB id is usable — it is the identity
// Stremio-protocol sources key on — and an episode's id is composed here as
// series:season:episode, which is why the coordinates arrive as numbers: that
// colon-separated form is the upstream's convention and nothing outside this
// file should know it.
//
// It reports false when there is nothing usable, which is a normal answer and
// not an error.
func addressOf(ref v1.ContentRef, season, episode int) (typ, id string, ok bool) {
	// A ref this module produced. It cannot happen today — nothing here makes one
	// — but a ref that already carries native addressing is usable as it stands,
	// and honouring it costs a comparison.
	if ref.NativeID != "" && ref.NativeType != "" {
		return ref.NativeType, ref.NativeID, true
	}

	if ref.ExternalScheme != imdbScheme || ref.ExternalID == "" {
		return "", "", false
	}

	switch ref.MediaType {
	case v1.MediaMovie:
		return "movie", ref.ExternalID, true
	case v1.MediaTVSeries, v1.MediaAnimeSeries:
		if episode <= 0 {
			// A series as a whole has no stream; only its episodes do.
			return "", "", false
		}
		return "series", fmt.Sprintf("%s:%d:%d", ref.ExternalID, season, episode), true
	default:
		return "", "", false
	}
}

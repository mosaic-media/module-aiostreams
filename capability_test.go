package aiostreams_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aiostreams "github.com/mosaic-media/module-aiostreams"
	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// The provider-role tests run against a fake AIOStreams over httptest, reached
// by putting the fake's URL in the module's settings. That needs no seam in the
// production type, because the instance is a setting rather than a constant.
//
// Hermetic on purpose. The public instance serves nothing without somebody's
// profile, and a profile URL is a credential no CI should hold; a test against
// it would either be skipped, flaky, or a secret leak.

const (
	// profilePath is a configured profile on the fake — two opaque segments, the
	// shape the real instance hands out.
	profilePath = "/stremio/6f1b2c9d/AAAAencrypted"
	// unconfiguredPath is the same instance with no profile.
	unconfiguredPath = "/stremio"
)

// fakeInstance is an AIOStreams instance serving one film and one episode.
func fakeInstance(t *testing.T) *httptest.Server {
	t.Helper()

	configured := map[string]any{
		"id":      "com.aiostreams.viren070",
		"name":    "AIOStreams | Test",
		"version": "2.31.1",
		"logo":    "https://example.invalid/logo.png",
		"types":   []string{"movie", "series"},
		"resources": []any{
			map[string]any{"name": "stream", "types": []string{"movie", "series"}, "idPrefixes": []string{"tt"}},
			map[string]any{"name": "subtitles", "types": []string{"movie", "series"}, "idPrefixes": []string{"tt"}},
		},
		"behaviorHints": map[string]any{"configurable": true},
	}
	unconfigured := map[string]any{
		"id":            "com.aiostreams.viren070",
		"name":          "AIOStreams | Test",
		"version":       "2.31.1",
		"resources":     []any{},
		"types":         []any{},
		"behaviorHints": map[string]any{"configurable": true, "configurationRequired": true},
	}
	movieStreams := map[string]any{"streams": []any{
		map[string]any{
			"name":  "[RD+] AIOStreams 2160p",
			"url":   "https://cdn.invalid/one.mkv",
			"title": "The Matrix 1999 2160p BluRay x265 EAC3\n💾 24.1 GB 👤 88",
			"behaviorHints": map[string]any{
				"videoSize": 24_100_000_000,
				"filename":  "The.Matrix.1999.2160p.BluRay.x265.EAC3.mkv",
			},
		},
		map[string]any{
			"name":     "[P2P] AIOStreams 1080p",
			"title":    "The Matrix 1999 1080p WEB-DL x264 AAC\n💾 4.2 GB 👤 12",
			"infoHash": "cafebabe",
		},
		// A result with nowhere to fetch from. It is dropped rather than attached
		// as a Part with an empty location.
		map[string]any{"name": "broken", "title": "no url and no hash"},
	}}
	episodeStreams := map[string]any{"streams": []any{
		map[string]any{"name": "AIOStreams 1080p", "url": "https://cdn.invalid/ep.mkv", "title": "Show S01E01 1080p"},
	}}
	subtitles := map[string]any{"subtitles": []any{
		map[string]any{"id": "1", "lang": "eng", "url": "https://subs.invalid/1.srt"},
		map[string]any{"id": "2", "lang": "eng", "url": "https://subs.invalid/1.srt"},
	}}

	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc(profilePath+"/manifest.json", func(w http.ResponseWriter, _ *http.Request) { write(w, configured) })
	mux.HandleFunc(unconfiguredPath+"/manifest.json", func(w http.ResponseWriter, _ *http.Request) { write(w, unconfigured) })
	mux.HandleFunc(profilePath+"/stream/movie/tt0133093.json", func(w http.ResponseWriter, _ *http.Request) { write(w, movieStreams) })
	mux.HandleFunc(profilePath+"/stream/series/tt0903747:1:1.json", func(w http.ResponseWriter, _ *http.Request) { write(w, episodeStreams) })
	mux.HandleFunc(profilePath+"/subtitles/movie/tt0133093.json", func(w http.ResponseWriter, _ *http.Request) { write(w, subtitles) })
	// The episode path is registered only in its composed form, so a request
	// built from the wrong coordinates 404s rather than quietly answering with
	// the film's tracks. That is the same arrangement the stream fixture uses.
	mux.HandleFunc(profilePath+"/subtitles/series/tt0903747:1:1.json", func(w http.ResponseWriter, _ *http.Request) { write(w, subtitles) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// settingsFor is the module's settings document naming an instance.
func settingsFor(t *testing.T, instanceURL string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]string{"instanceUrl": instanceURL})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	return b
}

func TestManifestDeclaresOnlyTheSourceRolesItFills(t *testing.T) {
	m := aiostreams.New(nil).Manifest()
	if m.ID != aiostreams.CapabilityID {
		t.Errorf("manifest id = %q, want %q", m.ID, aiostreams.CapabilityID)
	}
	declared := make(map[v1.Role]bool, len(m.Provides))
	for _, r := range m.Provides {
		declared[r] = true
	}
	for _, want := range []v1.Role{v1.RoleStream, v1.RoleSubtitles, v1.RoleSettingsUI} {
		if !declared[want] {
			t.Errorf("manifest does not declare %q", want)
		}
	}
	// The absences are the module's shape, not an oversight: a stream provider
	// that acquired a metadata or catalog role would start putting titles into
	// the library, which is another tier's job (platform#46).
	for _, unwanted := range []v1.Role{v1.RoleMetadata, v1.RoleSearch, v1.RoleCatalog, v1.RolePlayback} {
		if declared[unwanted] {
			t.Errorf("manifest declares %q; this module is a stream provider only", unwanted)
		}
	}
}

func TestImportIsRefused(t *testing.T) {
	_, err := aiostreams.New(nil).Import(context.Background(), nil, v1.ImportRequest{})
	if err == nil {
		t.Fatal("expected import to be refused")
	}
	if !strings.Contains(err.Error(), "stream provider") {
		t.Errorf("error should say why it was refused, got %q", err)
	}
}

func TestStreamsResolvesAFilmByIMDBIdentity(t *testing.T) {
	srv := fakeInstance(t)
	cap := aiostreams.New(srv.Client())

	resp, err := cap.Streams(context.Background(), v1.StreamRequest{
		Settings: settingsFor(t, srv.URL+profilePath+"/manifest.json"),
		Ref: v1.ContentRef{
			MediaType: v1.MediaMovie, ExternalScheme: "imdb", ExternalID: "tt0133093",
		},
	})
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	// Two of the three results are addressable; the third had neither a URL nor
	// an info hash and is dropped rather than stored as an empty location.
	if len(resp.Streams) != 2 {
		t.Fatalf("got %d streams, want 2: %+v", len(resp.Streams), resp.Streams)
	}

	first := resp.Streams[0]
	if first.Location.Scheme != v1.RemoteLocation || first.Location.Provider != "aiostreams" {
		t.Errorf("location = %+v, want a remote aiostreams location", first.Location)
	}
	if first.Location.Ref != "https://cdn.invalid/one.mkv" {
		t.Errorf("ref = %q, want the direct URL", first.Location.Ref)
	}
	if first.Quality != "2160p" {
		t.Errorf("quality = %q, want 2160p", first.Quality)
	}
	if first.SizeBytes != 24_100_000_000 {
		t.Errorf("size = %d, want the declared 24.1 GB", first.SizeBytes)
	}
	if first.Seeders != 88 {
		t.Errorf("seeders = %d, want 88", first.Seeders)
	}
	if first.Title != "The.Matrix.1999.2160p.BluRay.x265.EAC3.mkv" {
		t.Errorf("title = %q, want the release filename", first.Title)
	}
	if first.Label != "[RD+] AIOStreams 2160p" {
		t.Errorf("label = %q, want the instance's short label", first.Label)
	}
	// The container and the two codecs are what platform#27's playability
	// decision reads, and a StreamLink is the only route this module has to them:
	// it reaches the library through the enrichment pass and attaches no Parts of
	// its own.
	if first.Container != "mkv" {
		t.Errorf("container = %q, want mkv", first.Container)
	}
	if first.VideoCodec != "hevc" {
		t.Errorf("videoCodec = %q, want hevc — ffprobe's spelling, not the release's x265", first.VideoCodec)
	}
	if first.AudioCodec != "eac3" {
		t.Errorf("audioCodec = %q, want eac3", first.AudioCodec)
	}

	// The instance's own order is the user's configured sort, so it is carried
	// through rather than re-ranked here.
	if resp.Streams[1].Quality != "1080p" {
		t.Errorf("second stream quality = %q; the instance's order was not preserved", resp.Streams[1].Quality)
	}
	if got := resp.Streams[1].Location.Ref; got != "magnet:?xt=urn:btih:cafebabe" {
		t.Errorf("second ref = %q, want a magnet built from the info hash", got)
	}
}

func TestStreamsComposesTheEpisodeAddress(t *testing.T) {
	// The colon-separated episode id is the upstream's convention, so the
	// Platform hands over season and episode as numbers and this composes it. The
	// fake only answers the composed path, so a wrong composition is a 404 rather
	// than a silently different result.
	srv := fakeInstance(t)
	resp, err := aiostreams.New(srv.Client()).Streams(context.Background(), v1.StreamRequest{
		Settings: settingsFor(t, srv.URL+profilePath),
		Ref: v1.ContentRef{
			MediaType: v1.MediaTVSeries, ExternalScheme: "imdb", ExternalID: "tt0903747",
		},
		Season: 1, Episode: 1,
	})
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(resp.Streams) != 1 {
		t.Fatalf("got %d streams, want 1", len(resp.Streams))
	}
}

func TestStreamsDeclinesWhatItCannotAddress(t *testing.T) {
	srv := fakeInstance(t)
	settings := settingsFor(t, srv.URL+profilePath)

	cases := []struct {
		name string
		req  v1.StreamRequest
	}{
		{
			// A TMDB-sourced work whose IMDB id was unknown. Declining is the
			// correct answer, and it must not be an error: the enrichment pass asks
			// every stream provider about every title, and an error here would fail
			// a user's import over a title that was never this module's to know.
			name: "a scheme it does not speak",
			req: v1.StreamRequest{Settings: settings, Ref: v1.ContentRef{
				MediaType: v1.MediaMovie, ExternalScheme: "tmdb", ExternalID: "603"}},
		},
		{
			name: "a series with no episode",
			req: v1.StreamRequest{Settings: settings, Ref: v1.ContentRef{
				MediaType: v1.MediaTVSeries, ExternalScheme: "imdb", ExternalID: "tt0903747"}},
		},
		{
			name: "a media type with no stream address",
			req: v1.StreamRequest{Settings: settings, Ref: v1.ContentRef{
				MediaType: v1.MediaType("book"), ExternalScheme: "imdb", ExternalID: "tt1"}},
		},
		{
			name: "no identity at all",
			req:  v1.StreamRequest{Settings: settings, Ref: v1.ContentRef{MediaType: v1.MediaMovie}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := aiostreams.New(srv.Client()).Streams(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("declining must not be an error, got %v", err)
			}
			if len(resp.Streams) != 0 {
				t.Errorf("got %d streams, want none", len(resp.Streams))
			}
		})
	}
}

func TestStreamsFromAnUnconfiguredInstanceAreEmptyRatherThanAnError(t *testing.T) {
	// The state a fresh install is in: the default instance is reachable and has
	// no profile, so it declares no stream resource. That has to read as "nothing
	// here yet", because it is one settings screen away from working.
	srv := fakeInstance(t)
	resp, err := aiostreams.New(srv.Client()).Streams(context.Background(), v1.StreamRequest{
		Settings: settingsFor(t, srv.URL+unconfiguredPath),
		Ref: v1.ContentRef{
			MediaType: v1.MediaMovie, ExternalScheme: "imdb", ExternalID: "tt0133093"},
	})
	if err != nil {
		t.Fatalf("an unconfigured instance must not error, got %v", err)
	}
	if len(resp.Streams) != 0 {
		t.Errorf("got %d streams from an unconfigured instance", len(resp.Streams))
	}
}

func TestSubtitlesResolveAndDeduplicate(t *testing.T) {
	srv := fakeInstance(t)
	resp, err := aiostreams.New(srv.Client()).Subtitles(context.Background(), v1.SubtitlesRequest{
		Settings: settingsFor(t, srv.URL+profilePath),
		Ref: v1.ContentRef{
			MediaType: v1.MediaMovie, ExternalScheme: "imdb", ExternalID: "tt0133093"},
	})
	if err != nil {
		t.Fatalf("Subtitles: %v", err)
	}
	// The instance merges several subtitle addons, so the same file arriving
	// twice is routine.
	if len(resp.Subtitles) != 1 {
		t.Fatalf("got %d tracks, want 1 after dedup: %+v", len(resp.Subtitles), resp.Subtitles)
	}
	if resp.Subtitles[0].Language != "eng" {
		t.Errorf("language = %q, want eng", resp.Subtitles[0].Language)
	}
}

// TestSubtitlesComposesTheEpisodeAddress is the subtitles half of what
// TestStreamsComposesTheEpisodeAddress proves for streams.
//
// The fake answers only the composed path, so a request built from the wrong
// coordinates — or from none — is a 404 rather than a silently different result.
// That matters more here than it would elsewhere: subtitles for the wrong
// episode are not an error anyone sees, they are a track that does not match the
// dialogue.
func TestSubtitlesComposesTheEpisodeAddress(t *testing.T) {
	srv := fakeInstance(t)
	resp, err := aiostreams.New(srv.Client()).Subtitles(context.Background(), v1.SubtitlesRequest{
		Settings: settingsFor(t, srv.URL+profilePath),
		Ref: v1.ContentRef{
			MediaType: v1.MediaTVSeries, ExternalScheme: "imdb", ExternalID: "tt0903747",
		},
		Season: 1, Episode: 1,
	})
	if err != nil {
		t.Fatalf("Subtitles: %v", err)
	}
	if len(resp.Subtitles) != 1 {
		t.Fatalf("got %d tracks, want 1 — an empty result here means the episode address was not composed", len(resp.Subtitles))
	}
	if resp.Subtitles[0].Language != "eng" {
		t.Errorf("language = %q, want eng", resp.Subtitles[0].Language)
	}
}

func TestABadSettingsDocumentIsAnError(t *testing.T) {
	// Unparseable settings are the one thing that is an error: the module was
	// handed something it cannot act on at all, which is different from being
	// asked about a title it does not have.
	_, err := aiostreams.New(nil).Streams(context.Background(), v1.StreamRequest{
		Settings: []byte("not json"),
		Ref:      v1.ContentRef{MediaType: v1.MediaMovie, ExternalScheme: "imdb", ExternalID: "tt1"},
	})
	if err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestEmptySettingsUseThePublicDefault(t *testing.T) {
	// No network: the point is only that an empty document resolves to the
	// default instance rather than to nothing, which is what makes the module
	// installable with no configuration at all.
	if !strings.HasPrefix(aiostreams.DefaultInstance, "https://") {
		t.Fatalf("default instance %q is not an https URL", aiostreams.DefaultInstance)
	}
	resp, err := aiostreams.New(nil).Streams(context.Background(), v1.StreamRequest{
		Settings: nil,
		// A ref it cannot address, so the default is resolved and then the request
		// is declined before any call is made.
		Ref: v1.ContentRef{MediaType: v1.MediaMovie, ExternalScheme: "tmdb", ExternalID: "603"},
	})
	if err != nil {
		t.Fatalf("empty settings must be usable, got %v", err)
	}
	if len(resp.Streams) != 0 {
		t.Errorf("got %d streams, want none", len(resp.Streams))
	}
}

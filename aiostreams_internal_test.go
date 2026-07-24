package aiostreams

import (
	"testing"
	"time"
)

// The upstream-shaped unit tests. Everything here is about text a user pasted or
// a formatter emitted, which is exactly where this module's bugs live.

func TestNormaliseInstanceURL(t *testing.T) {
	// Every input is a form AIOStreams itself hands out, plus the ones a person
	// types. The property that matters in all of them: the profile segments
	// survive. Dropping them turns a configured URL into the unconfigured
	// instance, which fails as "no streams" rather than as an error.
	const profile = "https://aiostreams.elfhosted.com/stremio/6f1b2c/AAAAencrypted"
	cases := []struct{ name, in, want string }{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"base url", profile, profile},
		{"manifest url", profile + "/manifest.json", profile},
		{"configure page", profile + "/configure", profile},
		{"install scheme", "stremio://aiostreams.elfhosted.com/stremio/6f1b2c/AAAAencrypted/manifest.json", profile},
		{"trailing slash", profile + "/", profile},
		{"query and fragment", profile + "/manifest.json?x=1#y", profile},
		{"surrounding space", "  " + profile + "/manifest.json  ", profile},
		{"self hosted root", "https://aio.example.org/stremio/manifest.json", "https://aio.example.org/stremio"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normaliseInstanceURL(tc.in); got != tc.want {
				t.Errorf("normaliseInstanceURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormaliseInstanceURLKeepsAProfileSegmentNamedLikeARoute(t *testing.T) {
	// The suffix trim is a suffix trim, not a substring strip: an instance whose
	// path happens to contain "configure" earlier on must keep it.
	const in = "https://aio.example.org/configure/stremio/abc/manifest.json"
	const want = "https://aio.example.org/configure/stremio/abc"
	if got := normaliseInstanceURL(in); got != want {
		t.Errorf("normaliseInstanceURL(%q) = %q, want %q", in, got, want)
	}
}

func TestParseReleaseReadsTheDefaultFormatter(t *testing.T) {
	// A result shaped the way AIOStreams' default formatter emits one.
	s := Stream{
		Name:  "[RD+] AIOStreams 1080p",
		Title: "The Matrix 1999 1080p BluRay x264 DTS-HD MA 5.1\n💾 12.3 GB 👤 41\n🔗 Torrentio",
		BehaviorHints: behaviorHints{
			Filename: "The.Matrix.1999.1080p.BluRay.x264.DTS-HD.MA.5.1.mkv",
		},
	}
	d := parseRelease(s)
	if d.quality != "1080p" {
		t.Errorf("quality = %q, want 1080p", d.quality)
	}
	if d.videoCodec != "h264" {
		t.Errorf("videoCodec = %q, want h264", d.videoCodec)
	}
	if d.audioCodec != "dts" {
		t.Errorf("audioCodec = %q, want dts", d.audioCodec)
	}
	if d.container != "mkv" {
		t.Errorf("container = %q, want mkv", d.container)
	}
	if d.sizeBytes != 12_300_000_000 {
		t.Errorf("sizeBytes = %d, want 12300000000", d.sizeBytes)
	}
	if d.seeders != 41 {
		t.Errorf("seeders = %d, want 41", d.seeders)
	}
	if d.width != 1920 || d.height != 1080 {
		t.Errorf("dimensions = %dx%d, want 1920x1080", d.width, d.height)
	}
}

func TestParseReleasePrefersTheDeclaredSize(t *testing.T) {
	// A number the instance computed beats a string somebody wrote, even when
	// both are present and disagree.
	s := Stream{
		Title:         "Some Film 2160p 💾 60 GB",
		BehaviorHints: behaviorHints{VideoSize: 42},
	}
	if got := parseRelease(s).sizeBytes; got != 42 {
		t.Errorf("sizeBytes = %d, want the declared 42", got)
	}
}

func TestParseReleaseHandlesACustomFormatter(t *testing.T) {
	// AIOStreams lets a user write their own formatter, so the parse has to
	// degrade rather than mis-fire on an unfamiliar layout. Here the facts are
	// present but arranged differently, with no glyphs at all.
	s := Stream{
		Name:        "AIO",
		Description: "Quality: 2160p | Codec: HEVC / EAC3 | Size: 24.5GB | 128 seeders | file: movie.2160p.mkv",
	}
	d := parseRelease(s)
	if d.quality != "2160p" {
		t.Errorf("quality = %q, want 2160p", d.quality)
	}
	if d.videoCodec != "hevc" || d.audioCodec != "eac3" {
		t.Errorf("codecs = %q/%q, want hevc/eac3", d.videoCodec, d.audioCodec)
	}
	if d.sizeBytes != 24_500_000_000 {
		t.Errorf("sizeBytes = %d, want 24500000000", d.sizeBytes)
	}
	if d.seeders != 128 {
		t.Errorf("seeders = %d, want 128", d.seeders)
	}
	if d.container != "mkv" {
		t.Errorf("container = %q, want mkv", d.container)
	}
}

func TestParseReleaseLeavesUnknownFieldsEmptyRatherThanGuessing(t *testing.T) {
	s := Stream{Name: "AIO", Title: "Some Release Nobody Described"}
	d := parseRelease(s)
	if d.quality != "" || d.videoCodec != "" || d.audioCodec != "" || d.container != "" {
		t.Errorf("expected every parsed field empty, got %+v", d)
	}
	if d.sizeBytes != 0 || d.seeders != 0 {
		t.Errorf("expected no size or seeders, got %d bytes / %d seeders", d.sizeBytes, d.seeders)
	}
}

func TestFindQualityCollapsesSpellings(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Film 4K HDR", "2160p"},
		{"Film UHD", "2160p"},
		{"Film 2160p", "2160p"},
		{"Film FHD", "1080p"},
		{"Film 720p", "720p"},
		{"Film with no resolution", ""},
	} {
		if got := findQuality(tc.in); got != tc.want {
			t.Errorf("findQuality(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestReleaseLabelPrefersTheFilenameThenTheFirstLine(t *testing.T) {
	withFilename := Stream{
		Title:         "Something Else",
		BehaviorHints: behaviorHints{Filename: "The.Film.2020.1080p.mkv"},
	}
	if got := releaseLabel(withFilename); got != "The.Film.2020.1080p.mkv" {
		t.Errorf("releaseLabel = %q, want the filename", got)
	}

	// Without one, the first line only — the rest of the block is size, seeders
	// and source, and a label carrying all of it renders as a four-line row.
	multiline := Stream{Title: "The Film 2020 1080p\n💾 8 GB 👤 12\n🔗 Source"}
	if got := releaseLabel(multiline); got != "The Film 2020 1080p" {
		t.Errorf("releaseLabel = %q, want the first line only", got)
	}

	if got := releaseLabel(Stream{}); got != "" {
		t.Errorf("releaseLabel of an unlabelled stream = %q, want empty", got)
	}
}

func TestStreamRefPrefersTheURLAndFallsBackToAMagnet(t *testing.T) {
	if got := (Stream{URL: "https://cdn.example/file.mkv", InfoHash: "abc"}).Ref(); got != "https://cdn.example/file.mkv" {
		t.Errorf("Ref = %q, want the direct URL", got)
	}
	if got := (Stream{InfoHash: "abc123"}).Ref(); got != "magnet:?xt=urn:btih:abc123" {
		t.Errorf("Ref = %q, want a magnet", got)
	}
	if got := (Stream{}).Ref(); got != "" {
		t.Errorf("Ref of an addressless stream = %q, want empty", got)
	}
}

func TestSupportsHonoursTypesAndIDPrefixes(t *testing.T) {
	m := Manifest{
		Types: []string{"movie", "series"},
		Resources: []ResourceDecl{
			{Name: "stream", Types: []string{"movie", "series"}, IDPrefixes: []string{"tt"}},
			{Name: "subtitles"},
		},
	}
	if !supports(m, "stream", "movie", "tt0133093") {
		t.Error("expected an IMDB movie id to be supported")
	}
	if supports(m, "stream", "movie", "kitsu:123") {
		t.Error("expected a non-tt id to be refused by the prefix constraint")
	}
	if supports(m, "stream", "channel", "tt0133093") {
		t.Error("expected an undeclared type to be refused")
	}
	// A bare-string resource inherits the manifest's top-level types.
	if !supports(m, "subtitles", "series", "tt0903747:1:1") {
		t.Error("expected a bare-string resource to inherit the manifest types")
	}
}

func TestUnconfiguredManifestSupportsNothing(t *testing.T) {
	// The real shape the public instance serves with no profile: configurable,
	// configuration required, and empty everything. It has to read as "ask me
	// nothing" rather than as an error.
	m := Manifest{
		ID:            "com.aiostreams.viren070",
		Name:          "AIOStreams | ElfHosted",
		BehaviorHints: addonBehaviorHints{Configurable: true, ConfigurationRequired: true},
	}
	if supports(m, "stream", "movie", "tt0133093") {
		t.Error("an unconfigured instance must support no stream request")
	}
	if declaresResource(m, "stream") {
		t.Error("an unconfigured instance declares no stream resource")
	}
}

func TestDedupeSubtitlesCollapsesTheSameFileInTheSameLanguage(t *testing.T) {
	in := []Subtitle{
		{Lang: "eng", URL: "https://x/1.srt"},
		{Lang: "ENG", URL: "https://x/1.srt"},
		{Lang: "eng", URL: "https://x/2.srt"},
		{Lang: "fre", URL: ""},
	}
	got := dedupeSubtitles(in)
	if len(got) != 2 {
		t.Fatalf("got %d tracks, want 2: %+v", len(got), got)
	}
	if got[0].URL != "https://x/1.srt" || got[1].URL != "https://x/2.srt" {
		t.Errorf("unexpected tracks: %+v", got)
	}
}

func TestMaskInstanceURLHidesTheProfileAndKeepsTheHost(t *testing.T) {
	got := maskInstanceURL("https://aiostreams.elfhosted.com/stremio/6f1b2c9d4e5f/AAAAsecretvalue")
	const want = "aiostreams.elfhosted.com/stremio/••••4e5f/••••alue"
	if got != want {
		t.Errorf("maskInstanceURL = %q, want %q", got, want)
	}
	// The default has nothing to hide, and hiding nothing must not mangle it.
	if got := maskInstanceURL(DefaultInstance); got != "aiostreams.elfhosted.com/stremio" {
		t.Errorf("maskInstanceURL(default) = %q", got)
	}
}

func TestInstanceHostDropsTheProfilePath(t *testing.T) {
	if got := instanceHost("https://aiostreams.elfhosted.com/stremio/abc/def"); got != "aiostreams.elfhosted.com" {
		t.Errorf("instanceHost = %q", got)
	}
}

func TestManifestCacheExpires(t *testing.T) {
	now := time.Unix(0, 0)
	c := newManifestCache()
	c.now = func() time.Time { return now }

	c.put("base", Manifest{Name: "first"})
	if m, ok := c.get("base"); !ok || m.Name != "first" {
		t.Fatalf("expected a fresh entry, got %+v ok=%v", m, ok)
	}

	now = now.Add(manifestTTL - time.Second)
	if _, ok := c.get("base"); !ok {
		t.Error("entry expired before its TTL")
	}

	now = now.Add(2 * time.Second)
	if _, ok := c.get("base"); ok {
		t.Error("entry survived its TTL; reconfiguring an instance would not take effect")
	}
}

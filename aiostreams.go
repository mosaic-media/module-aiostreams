package aiostreams

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The AIOStreams boundary. Everything upstream-shaped lives in this file: the
// instance URL forms a user can paste, the subset of the Stremio addon protocol
// AIOStreams speaks, and the release text its formatters emit. Nothing above
// this file sees an AIOStreams shape, and the Platform sees none at all
// (module-stremio-addons#2).

// DefaultInstance is the public AIOStreams instance run by ElfHosted, and the
// module's default.
//
// It is a default host, not a working configuration. AIOStreams resolves nothing
// until a profile exists: the manifest at this base declares
// configurationRequired, an empty resources array and no types, so a stream
// request against it correctly yields nothing. A user creates a profile on the
// instance and pastes the resulting manifest URL — which carries their profile
// id — into this module's settings.
//
// Defaulting to it anyway lets the settings screen offer a working "Configure"
// link on a fresh install rather than an empty box and a URL to go and find, and
// names the instance the module is pointed at instead of leaving that implicit.
const DefaultInstance = "https://aiostreams.elfhosted.com/stremio"

// manifestTTL is how long a fetched manifest is trusted.
//
// A stream provider is called per item: enriching a series asks once per
// episode, and without a cache that is one manifest fetch per episode on top of
// the stream fetch that is the actual work. Short enough that reconfiguring an
// instance takes effect while a user is still looking at the screen, long enough
// that a season import pays for one.
const manifestTTL = 5 * time.Minute

// Manifest is the subset of an addon manifest this module reads. AIOStreams is
// a Stremio addon by protocol, so this is the Stremio manifest shape — narrowed
// to what a stream and subtitle provider needs, which is why there are no
// catalog fields here.
type Manifest struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Description   string             `json:"description"`
	Logo          string             `json:"logo"`
	Resources     []ResourceDecl     `json:"resources"`
	Types         []string           `json:"types"`
	BehaviorHints addonBehaviorHints `json:"behaviorHints"`
}

// addonBehaviorHints is the manifest's self-description of its configuration
// state. ConfigurationRequired is the flag an unconfigured AIOStreams instance
// sets, and it is what lets this module tell "pointed at an instance that has no
// profile yet" apart from "pointed at nothing" — two states that otherwise both
// look like an empty stream list.
type addonBehaviorHints struct {
	Configurable          bool `json:"configurable"`
	ConfigurationRequired bool `json:"configurationRequired"`
}

// ResourceDecl is one entry of a manifest's resources array. The protocol allows
// each entry to be either a bare string ("stream") or an object carrying its own
// types and id prefixes; both shapes are unmarshalled.
type ResourceDecl struct {
	Name       string
	Types      []string
	IDPrefixes []string
}

// UnmarshalJSON accepts either a bare string or the object form.
func (r *ResourceDecl) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		r.Name = s
		return nil
	}
	var obj struct {
		Name       string   `json:"name"`
		Types      []string `json:"types"`
		IDPrefixes []string `json:"idPrefixes"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	r.Name, r.Types, r.IDPrefixes = obj.Name, obj.Types, obj.IDPrefixes
	return nil
}

// Stream is the subset of a stream object this module reads. A stream is either
// a direct URL — which is what AIOStreams returns for a debrid or proxied
// result, and the common case — or a torrent identified by InfoHash.
type Stream struct {
	Name          string        `json:"name"`
	Title         string        `json:"title"`
	Description   string        `json:"description"`
	URL           string        `json:"url"`
	InfoHash      string        `json:"infoHash"`
	FileIdx       int           `json:"fileIdx"`
	BehaviorHints behaviorHints `json:"behaviorHints"`
}

// behaviorHints is the subset of a stream's behaviorHints this module reads.
type behaviorHints struct {
	VideoSize int64  `json:"videoSize"`
	Filename  string `json:"filename"`
}

// Ref is the location reference to record for this stream: the direct URL when
// there is one, otherwise a magnet built from the info hash. Empty when the
// stream carries neither, which the caller drops.
func (s Stream) Ref() string {
	if s.URL != "" {
		return s.URL
	}
	if s.InfoHash != "" {
		return "magnet:?xt=urn:btih:" + s.InfoHash
	}
	return ""
}

// text is the stream's descriptive text, where AIOStreams' formatters pack the
// release name, resolution, size, seeders and service.
func (s Stream) text() string {
	return strings.Join([]string{s.Title, s.Name, s.Description, s.BehaviorHints.Filename}, "\n")
}

// Subtitle is the subset of a subtitles response entry this module reads
// (module-stremio-addons#1): a track's language and file URL.
type Subtitle struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Lang string `json:"lang"`
}

// InstanceInfo is the display detail for the configured instance, for the
// settings screen: what the manifest says about itself, and whether it is
// reachable and configured at all.
type InstanceInfo struct {
	// Base is the normalised instance URL requests are built from.
	Base string
	// Reachable is false when the manifest could not be fetched — a wrong URL, an
	// instance that is down, a profile that has been deleted.
	Reachable bool
	// Err is why it was not reachable, for a screen that has to say something
	// more useful than "no".
	Err error
	// Configured is true when the instance serves a stream resource. An
	// AIOStreams base with no profile is reachable and not configured, which is
	// the state a fresh install is in.
	Configured  bool
	Name        string
	Version     string
	Logo        string
	Description string
}

// Client talks to one AIOStreams instance. One instance, not a list: this module
// is a provider for a named upstream, and "which of several sources answered" is
// the question module-stremio-addons exists to ask.
type Client struct {
	http  *http.Client
	base  string
	cache *manifestCache
}

// newClient builds a client over one normalised instance base URL, sharing the
// capability's manifest cache.
func newClient(httpClient *http.Client, base string, cache *manifestCache) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{http: httpClient, base: base, cache: cache}
}

// Base is the instance URL this client addresses.
func (c *Client) Base() string { return c.base }

// Streams fetches every stream the instance offers for a content id.
//
// All of them rather than the best one (platform#28): AIOStreams' whole purpose is
// to return a filtered, sorted set, and which member of it a particular client
// can play is not knowable here. The order is the instance's own, which is the
// user's configured sort, so it is carried through rather than re-ranked.
//
// It returns nothing, and no error, when the instance declares no stream
// resource for the type. That is the unconfigured-instance case and it is
// normal, not a failure.
func (c *Client) Streams(ctx context.Context, typ, id string) ([]Stream, error) {
	manifest, err := c.manifest(ctx)
	if err != nil {
		return nil, err
	}
	if !supports(manifest, "stream", typ, id) {
		return nil, nil
	}
	var resp struct {
		Streams []Stream `json:"streams"`
	}
	if err := c.getJSON(ctx, c.base+"/stream/"+typ+"/"+id+".json", &resp); err != nil {
		return nil, err
	}
	out := make([]Stream, 0, len(resp.Streams))
	for _, s := range resp.Streams {
		if s.Ref() != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// Subtitles fetches subtitle tracks for a content id, de-duplicated by language
// and file. It returns nothing, and no error, when the instance declares no
// subtitles resource — AIOStreams serves them only when the user's profile
// enables a subtitle source.
func (c *Client) Subtitles(ctx context.Context, typ, id string) ([]Subtitle, error) {
	manifest, err := c.manifest(ctx)
	if err != nil {
		return nil, err
	}
	if !supports(manifest, "subtitles", typ, id) {
		return nil, nil
	}
	var resp struct {
		Subtitles []Subtitle `json:"subtitles"`
	}
	if err := c.getJSON(ctx, c.base+"/subtitles/"+typ+"/"+id+".json", &resp); err != nil {
		return nil, err
	}
	return dedupeSubtitles(resp.Subtitles), nil
}

// Instance reports what the configured instance says about itself, for the
// settings screen. It never returns an error: every failure is a state the
// screen has to render, so it is carried in the value.
func (c *Client) Instance(ctx context.Context) InstanceInfo {
	info := InstanceInfo{Base: c.base}
	manifest, err := c.manifest(ctx)
	if err != nil {
		info.Err = err
		return info
	}
	info.Reachable = true
	info.Name, info.Version, info.Logo, info.Description = manifest.Name, manifest.Version, manifest.Logo, manifest.Description
	// Configured is asked of the manifest rather than of the URL's shape. A
	// profile id in the path is not evidence — a deleted profile still leaves the
	// URL — and an instance deployed with a baked-in configuration has no id in
	// its path at all. What the instance declares it serves is the fact.
	info.Configured = declaresResource(manifest, "stream") && !manifest.BehaviorHints.ConfigurationRequired
	return info
}

// manifest fetches the instance manifest, through the shared cache.
func (c *Client) manifest(ctx context.Context) (Manifest, error) {
	if m, ok := c.cache.get(c.base); ok {
		return m, nil
	}
	var m Manifest
	if err := c.getJSON(ctx, c.base+"/manifest.json", &m); err != nil {
		return Manifest{}, fmt.Errorf("fetch manifest from %s: %w", c.base, err)
	}
	c.cache.put(c.base, m)
	return m, nil
}

// manifestCache holds fetched manifests for manifestTTL, keyed by instance base
// URL. It lives on the Capability, so it spans the per-invocation clients the
// provider roles build.
type manifestCache struct {
	mu      sync.Mutex
	entries map[string]cachedManifest
	// now is injected so the expiry is testable without sleeping. Production
	// leaves it nil and reads the wall clock.
	now func() time.Time
}

type cachedManifest struct {
	manifest Manifest
	at       time.Time
}

func newManifestCache() *manifestCache {
	return &manifestCache{entries: make(map[string]cachedManifest)}
}

func (c *manifestCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *manifestCache) get(base string) (Manifest, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[base]
	if !ok || c.clock().Sub(e.at) >= manifestTTL {
		return Manifest{}, false
	}
	return e.manifest, true
}

func (c *manifestCache) put(base string, m Manifest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[base] = cachedManifest{manifest: m, at: c.clock()}
}

// normaliseInstanceURL turns whatever a user pastes into the base URL requests
// are built from — the prefix "/manifest.json", "/stream/…" and "/subtitles/…"
// hang off.
//
// The forms are all things AIOStreams itself hands out. Its "Copy URL" gives the
// manifest URL; its install button gives the same under the stremio:// scheme;
// and the page a user is looking at when they decide to copy anything is
// "…/configure", which is the one most often pasted by hand.
//
// It trims a suffix and never a path: the profile segments before those suffixes
// are the configuration and are preserved. A normalisation that dropped the whole
// path would silently turn a working URL into the unconfigured instance, which
// fails as "no streams" rather than as an error.
//
// Empty input yields "", which the caller reads as "use the default".
func normaliseInstanceURL(u string) string {
	s := strings.TrimSpace(u)
	if s == "" {
		return ""
	}
	if rest, ok := strings.CutPrefix(s, "stremio://"); ok {
		s = "https://" + rest
	}
	// A query or fragment is never part of the base.
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimRight(s, "/")
	for _, suffix := range []string{"/manifest.json", "/configure"} {
		s = strings.TrimSuffix(s, suffix)
	}
	return strings.TrimRight(s, "/")
}

// configureURL is the instance's own configuration page for the configured
// profile — where the settings screen sends a user to create or change one.
func configureURL(base string) string { return base + "/configure" }

// supports reports whether the manifest declares the resource for the type, and
// that the id satisfies any id-prefix constraint on it. A bare-string resource
// inherits the manifest's top-level types.
func supports(m Manifest, resource, typ, id string) bool {
	for _, r := range m.Resources {
		if r.Name != resource {
			continue
		}
		types := r.Types
		if len(types) == 0 {
			types = m.Types
		}
		if !contains(types, typ) {
			continue
		}
		if len(r.IDPrefixes) > 0 && !hasAnyPrefix(id, r.IDPrefixes) {
			continue
		}
		return true
	}
	return false
}

// declaresResource reports whether the manifest names the resource at all,
// ignoring types and prefixes. It answers "is this instance configured", which
// is a coarser question than "can it answer this request".
func declaresResource(m Manifest, resource string) bool {
	for _, r := range m.Resources {
		if r.Name == resource {
			return true
		}
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// dedupeSubtitles collapses tracks that are the same file in the same language.
// AIOStreams merges several subtitle addons, so the same OpenSubtitles file
// arriving twice is routine, and a language listed four times is a worse track
// picker than a language listed once.
func dedupeSubtitles(in []Subtitle) []Subtitle {
	seen := make(map[string]bool, len(in))
	out := make([]Subtitle, 0, len(in))
	for _, s := range in {
		k := strings.ToLower(s.Lang) + "|" + s.URL
		if s.URL == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// userAgent identifies this module to the instance. It is load-bearing for
// reachability rather than courtesy: the public instance is Cloudflare-fronted,
// and Go's default "Go-http-client/1.1" draws a 403 where any honest custom
// identifier is served. getJSON sets it on every request.
var userAgent = "mosaic-module-aiostreams/" + moduleVersion

func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The URL carries the user's profile id, so it is not repeated in an error
		// that will be logged. The status is the actionable part.
		return fmt.Errorf("AIOStreams request failed: status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// releaseDetail is what a consumer ranks a candidate on, parsed out of the
// stream's free text and behaviorHints (module-stremio-addons#1, platform#27).
//
// Every field is best-effort and is a guess about a file rather than a fact
// about it: the release text is written by whoever made the release, and it
// lies. What a candidate actually contains is settled by probing the bytes
// (platform#29). Filling them anyway is what makes a candidate list rankable
// before anything is fetched, and an empty field is not neutral — it leaves a
// playability decision with nothing to go on, which is how ten gigabytes of
// Matroska reach a browser that cannot decode it.
type releaseDetail struct {
	quality    string
	sizeBytes  int64
	seeders    int
	container  string
	videoCodec string
	audioCodec string
	width      int
	height     int
}

// parseRelease teases the release detail out of a stream.
//
// It scans whole tokens rather than matching regular expressions, deliberately:
// AIOStreams lets a user write their own result formatter, so the text this
// reads is not one upstream's convention but whatever that user configured. A
// token scan degrades into "found nothing" on an unfamiliar layout, where a
// pattern anchored on one formatter's punctuation would match the wrong span of
// a different one and report a confident, incorrect answer.
func parseRelease(s Stream) releaseDetail {
	text := s.text()
	d := releaseDetail{}
	d.quality = findQuality(text)
	d.videoCodec = findVideoCodec(text)
	d.audioCodec = findAudioCodec(text)

	// The filename is the most trustworthy place a container appears; the free
	// text is the fallback for a formatter that emits no filename at all.
	d.container = findContainer(s.BehaviorHints.Filename)
	if d.container == "" {
		d.container = findContainer(text)
	}

	// The declared size is a number the instance computed; the parsed one is a
	// string somebody wrote. Prefer the number.
	if s.BehaviorHints.VideoSize > 0 {
		d.sizeBytes = s.BehaviorHints.VideoSize
	} else {
		d.sizeBytes = findSize(text)
	}
	d.seeders = findSeeders(text)
	d.width, d.height = dimensionsFor(d.quality)
	return d
}

// tokenise splits descriptive text into lowercase alphanumeric-ish tokens.
// Release text separates facts with everything from dots to emoji, so the
// separator set is "not a letter, digit, or the few characters that appear
// inside a token" — the plus in "DD+", the dot in "5.1" and in "2.3 GB".
func tokenise(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return false
		case r == '+', r == '.':
			return false
		}
		return true
	})
}

// findQuality reads the resolution label. "4k" and "uhd" collapse onto 2160p so
// that two candidates of the same resolution compare equal whatever the
// formatter called them.
func findQuality(text string) string {
	for _, tok := range tokenise(text) {
		switch tok {
		case "2160p", "4k", "uhd":
			return "2160p"
		case "1080p", "fhd":
			return "1080p"
		case "720p", "hd":
			return "720p"
		case "480p", "sd":
			return "480p"
		case "360p":
			return "360p"
		}
	}
	return ""
}

// findVideoCodec collapses the spellings of a codec onto the name ffprobe uses,
// so a parsed guess and a probed fact are comparable rather than merely both
// present.
func findVideoCodec(text string) string {
	for _, tok := range tokenise(text) {
		switch tok {
		case "x265", "h265", "h.265", "hevc":
			return "hevc"
		case "x264", "h264", "h.264", "avc":
			return "h264"
		case "av1":
			return "av1"
		case "vp9":
			return "vp9"
		case "xvid", "divx":
			return "mpeg4"
		}
	}
	return ""
}

// findAudioCodec does the same for audio. The distinctions that matter are the
// ones a browser cannot decode — E-AC3, AC3, DTS, TrueHD — because that is the
// decision a consumer makes with this field.
func findAudioCodec(text string) string {
	for _, tok := range tokenise(text) {
		switch tok {
		case "eac3", "e-ac3", "ddp", "ddp+", "dd+", "atmos":
			return "eac3"
		case "truehd":
			return "truehd"
		case "dts", "dtshd", "dts-hd":
			return "dts"
		case "ac3", "dd5.1":
			return "ac3"
		case "aac", "opus", "flac", "mp3":
			return tok
		}
	}
	return ""
}

// knownContainers is the extension set worth recognising — the ones a consumer's
// playability decision turns on.
var knownContainers = []string{"mkv", "mp4", "m4v", "avi", "ts", "m2ts", "webm", "mov", "wmv", "flv"}

// findContainer reads a container from a filename extension. It looks for a
// token rather than a trailing extension, because release text embeds the
// filename mid-sentence as often as it ends with it.
func findContainer(text string) string {
	for _, tok := range tokenise(text) {
		// A token like "show.s01e01.1080p.mkv" carries the extension after the last
		// dot; a bare "mkv" is the whole token.
		if i := strings.LastIndex(tok, "."); i >= 0 {
			tok = tok[i+1:]
		}
		for _, c := range knownContainers {
			if tok == c {
				return c
			}
		}
	}
	return ""
}

// findSize reads a human size ("2.3 GB", "700MB") into bytes, decimal units as
// sources report them. It takes the first size in the text: where a formatter
// emits more than one number, the release size leads and anything after it is
// cache or library statistics.
func findSize(text string) int64 {
	toks := tokenise(text)
	for i, tok := range toks {
		// "2.3gb" — number and unit fused into one token.
		for _, unit := range []string{"tb", "gb", "mb"} {
			if num, ok := strings.CutSuffix(tok, unit); ok && num != "" {
				if b := sizeToBytes(num, unit); b > 0 {
					return b
				}
			}
		}
		// "2.3 gb" — number and unit as neighbours.
		if i+1 < len(toks) {
			switch toks[i+1] {
			case "tb", "gb", "mb":
				if b := sizeToBytes(tok, toks[i+1]); b > 0 {
					return b
				}
			}
		}
	}
	return 0
}

// sizeToBytes converts a parsed number and unit to bytes.
func sizeToBytes(num, unit string) int64 {
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f <= 0 {
		return 0
	}
	switch unit {
	case "tb":
		return int64(f * 1e12)
	case "gb":
		return int64(f * 1e9)
	case "mb":
		return int64(f * 1e6)
	}
	return 0
}

// findSeeders reads swarm health. Aggregators mark it with a person glyph before
// the count, and AIOStreams' default formatter follows that convention; a custom
// formatter may spell it out instead, so both are read.
func findSeeders(text string) int {
	if n, ok := digitsAfter(text, "👤"); ok {
		return n
	}
	toks := tokenise(text)
	for i, tok := range toks {
		if tok != "seeders" && tok != "seeds" && tok != "seed" {
			continue
		}
		// The count sits on either side depending on the formatter: "👤 42",
		// "42 seeders", "seeders: 42".
		if i+1 < len(toks) {
			if n, err := strconv.Atoi(toks[i+1]); err == nil {
				return n
			}
		}
		if i > 0 {
			if n, err := strconv.Atoi(toks[i-1]); err == nil {
				return n
			}
		}
	}
	return 0
}

// digitsAfter reads the first run of digits following marker.
func digitsAfter(text, marker string) (int, bool) {
	i := strings.Index(text, marker)
	if i < 0 {
		return 0, false
	}
	rest := strings.TrimLeft(text[i+len(marker):], " \t")
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}

// dimensionsFor turns a resolution label into nominal dimensions. It is a
// ranking and display aid, not a measurement — the real numbers come from a
// probe (platform#29).
func dimensionsFor(quality string) (int, int) {
	switch quality {
	case "2160p":
		return 3840, 2160
	case "1080p":
		return 1920, 1080
	case "720p":
		return 1280, 720
	case "480p":
		return 854, 480
	case "360p":
		return 640, 360
	}
	return 0, 0
}

// releaseLabel is the human name for a candidate — the filename where the
// instance gives one, otherwise the first line of its descriptive text.
//
// The first line specifically: AIOStreams' formatters stack several facts into
// one field separated by newlines, and only the first is the release name. A
// label carrying the whole block is what a source picker would render as a
// four-line row.
func releaseLabel(s Stream) string {
	if s.BehaviorHints.Filename != "" {
		return s.BehaviorHints.Filename
	}
	for _, candidate := range []string{s.Title, s.Description, s.Name} {
		if line := strings.TrimSpace(firstLine(candidate)); line != "" {
			return line
		}
	}
	return ""
}

// firstLine takes the text up to the first line break.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// instanceHost is the instance's host, for a screen or a log line that needs to
// name the instance without reproducing the profile id in its path.
func instanceHost(base string) string {
	s := base
	for _, scheme := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, scheme)
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return s
}

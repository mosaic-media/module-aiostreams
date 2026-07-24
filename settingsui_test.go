package aiostreams_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	aiostreams "github.com/mosaic-media/module-aiostreams"
	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// renderSettings renders the settings screen and returns it as JSON text.
func renderSettings(t *testing.T, client *http.Client, settings []byte) string {
	t.Helper()
	resp, err := aiostreams.New(client).SettingsUI(context.Background(), v1.SettingsUIRequest{
		Caller: v1.CallerFromSession("s-1"), Settings: settings,
	})
	if err != nil {
		t.Fatalf("SettingsUI: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(resp.UI, &root); err != nil {
		t.Fatalf("settings UI is not valid JSON: %v", err)
	}
	if root["type"] != "Screen" {
		t.Fatalf("root type = %v, want Screen", root["type"])
	}
	return string(resp.UI)
}

func requireContains(t *testing.T, ui string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(ui, want) {
			t.Errorf("settings UI missing %q", want)
		}
	}
}

// TestSettingsUIRendersAConfiguredInstance is the screen in its finished state:
// it names the instance, says it is serving, and offers the way to change it.
func TestSettingsUIRendersAConfiguredInstance(t *testing.T) {
	srv := fakeInstance(t)
	ui := renderSettings(t, srv.Client(), settingsFor(t, srv.URL+profilePath+"/manifest.json"))

	requireContains(t, ui,
		"Instance", "Serving streams", "Your instance", "v2.31.1",
		// The change-instance form, and the reset that only exists once there is
		// something to revert to.
		"SubmitField", "$value", "configureModule", "Use a different instance", "Reset to default",
		// The upstream is named, along with who runs the default, because a user
		// deciding whether to keep the default needs both.
		"AIOStreams", "ElfHosted")
}

// TestSettingsUISaysWhenTheInstanceNeedsConfiguring covers the state a fresh
// install is in, which from the outside looks identical to a broken one — no
// streams — while being one click from working. Getting these two confused is
// how a user concludes the module does not work.
func TestSettingsUISaysWhenTheInstanceNeedsConfiguring(t *testing.T) {
	srv := fakeInstance(t)
	ui := renderSettings(t, srv.Client(), settingsFor(t, srv.URL+unconfiguredPath))

	requireContains(t, ui, "Needs configuring", "Open configuration", "/configure")
	if strings.Contains(ui, "Serving streams") {
		t.Error("an unconfigured instance must not report that it is serving")
	}
	if strings.Contains(ui, "Unreachable") {
		t.Error("a reachable but unconfigured instance must not be reported as unreachable")
	}
}

// TestSettingsUIReportsAnUnreachableInstance covers the third state: the URL is
// wrong, or the instance is down.
func TestSettingsUIReportsAnUnreachableInstance(t *testing.T) {
	srv := fakeInstance(t)
	ui := renderSettings(t, srv.Client(), settingsFor(t, srv.URL+"/stremio/no-such-profile"))

	requireContains(t, ui, "Unreachable", "Could not reach")
	if strings.Contains(ui, "Serving streams") || strings.Contains(ui, "Needs configuring") {
		t.Error("an unreachable instance must not be reported as any other state")
	}
}

// TestSettingsUIMasksTheProfileInTheURL is the security property of this screen.
// The manifest URL's path segments are a credential — they name the profile and
// carry its encrypted password, so anyone holding the URL holds that user's whole
// configuration, including whatever debrid key built it. A settings screen is a
// page people screenshot when asking for help.
//
// The assertion is over *rendered text* rather than over the whole payload, and
// the difference is the honest limit: the Configure button's action necessarily
// carries the full URL, because a link to somebody else's configuration page is
// not a link. Nothing a screenshot captures may contain it.
func TestSettingsUIMasksTheProfileInTheURL(t *testing.T) {
	srv := fakeInstance(t)
	resp, err := aiostreams.New(srv.Client()).SettingsUI(context.Background(), v1.SettingsUIRequest{
		Caller: v1.CallerFromSession("s-1"), Settings: settingsFor(t, srv.URL+profilePath),
	})
	if err != nil {
		t.Fatalf("SettingsUI: %v", err)
	}
	var root node
	if err := json.Unmarshal(resp.UI, &root); err != nil {
		t.Fatalf("settings UI is not valid JSON: %v", err)
	}
	for _, text := range displayText(root) {
		if strings.Contains(text, "AAAAencrypted") {
			t.Errorf("the settings screen renders the profile secret in visible text: %q", text)
		}
	}

	ui := string(resp.UI)
	if !strings.Contains(ui, "••••") {
		t.Error("the settings screen does not mask the profile segments at all")
	}
	// The host stays readable: it is what tells a user which instance they are
	// pointed at, and it is not a secret.
	requireContains(t, ui, "127.0.0.1")
}

// node is the shape of a serialised UINode, for walking a rendered screen.
type node struct {
	Props    map[string]any    `json:"props"`
	Children []node            `json:"children"`
	Slots    map[string][]node `json:"slots"`
}

// displayTextProps are the props a client renders as words on the screen. An
// action payload is deliberately not among them.
var displayTextProps = []string{"text", "label", "message", "title", "subtitle", "placeholder", "submitLabel"}

// displayText collects every string a viewer of this screen would read.
func displayText(n node) []string {
	var out []string
	for _, key := range displayTextProps {
		if s, ok := n.Props[key].(string); ok {
			out = append(out, s)
		}
	}
	for _, child := range n.Children {
		out = append(out, displayText(child)...)
	}
	for _, slot := range n.Slots {
		for _, child := range slot {
			out = append(out, displayText(child)...)
		}
	}
	return out
}

// TestSettingsUIOnAFreshInstall renders with no settings document at all, which
// is what the Platform hands a module nobody has configured. It must describe the
// default rather than fail, and it must not offer a reset to the default it is
// already using.
func TestSettingsUIOnAFreshInstall(t *testing.T) {
	// A client that fails every request stands in for "the public instance, from
	// a machine with no egress". The screen still has to render.
	ui := renderSettings(t, &http.Client{Transport: failingTransport{}}, nil)

	requireContains(t, ui, "Default instance", "Use a different instance", "SubmitField")
	if strings.Contains(ui, "Reset to default") {
		t.Error("an install already on the default must not be offered a reset to it")
	}
	if !strings.Contains(ui, "aiostreams.elfhosted.com") {
		t.Error("the screen must name the instance it defaults to")
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, http.ErrServerClosed
}

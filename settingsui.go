package aiostreams

import (
	"context"
	"strings"

	"github.com/mosaic-media/contracts/sdui"
	"github.com/mosaic-media/contracts/ui"
	v1 "github.com/mosaic-media/sdk/contracts/platform/v1"
)

// SettingsUI renders the module's own settings screen as SDUI (RoleSettingsUI,
// sdk#4): which instance is in use and whether it is actually serving,
// a way to open that instance's configuration page, a field to point the module
// at a different instance, and a way back to the default.
//
// This role is not decoration for this module. The public default is a *host*,
// not a working configuration — AIOStreams serves nothing until a profile exists
// — so this screen is the whole path from "installed" to "resolving streams". A
// capability with no client path is not done, it is owed.
//
// Every mutating control is an Invoke of the Platform's configureModule command
// carrying the complete new settings document, so the Platform stays the one
// that persists it and the module holds no state between invocations. The screen
// is returned as serialised UINode JSON, which is what keeps the SDK
// SDUI-agnostic.
func (c *Capability) SettingsUI(ctx context.Context, req v1.SettingsUIRequest) (v1.SettingsUIResponse, error) {
	s, err := settingsFrom(req.Settings)
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}
	client, err := c.clientFrom(req.Settings)
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}
	info := client.Instance(ctx)
	custom := normaliseInstanceURL(s.InstanceURL) != ""

	screen := ui.Screen(ui.Title("AIOStreams"), ui.Group(
		instanceSection(info, custom),
		changeInstanceSection(custom),
		aboutSection(),
	))

	data, err := screen.BuildJSON()
	if err != nil {
		return v1.SettingsUIResponse{}, err
	}
	return v1.SettingsUIResponse{UI: data}, nil
}

// configureInput builds the configureModule invoke input for an instance URL —
// the complete settings document the Platform persists.
//
// One field, so this module escapes the echo-the-secret problem `module-tmdb`
// documents: configureModule replaces the whole document with no merge, which
// forces a module with several settings to carry every one of them through the
// client on every control. Here the document *is* the one value the control is
// changing. That is luck rather than a solution — the SDK gap is still open —
// and it is a reason to think hard before this module grows a second setting.
func configureInput(instanceURL string) map[string]any {
	return map[string]any{
		"moduleId": CapabilityID,
		"settings": map[string]any{"instanceUrl": instanceURL},
	}
}

// instanceSection is the status block: which instance, in what state, and the
// one control that changes the state it is in.
//
// The three states are genuinely different and a screen that collapsed them
// would be lying about two of them. "Unreachable" is a broken URL or a downed
// instance. "Needs configuring" is the state a fresh install is in and looks
// identical from the outside — no streams — while being one click from working.
// "Serving" is done.
func instanceSection(info InstanceInfo, custom bool) *ui.Element {
	source := ui.Badge("Default instance", ui.ToneNeutral)
	if custom {
		source = ui.Badge("Your instance", ui.ToneInfo)
	}

	configure := ui.Button("Open configuration", "primary", ui.OnTap(ui.OpenURL(configureURL(info.Base))))

	switch {
	case !info.Reachable:
		return ui.Section("Instance",
			ui.Banner("Could not reach this AIOStreams instance. Check the URL below, or that the instance is up — until it answers, Mosaic has no streams from it.", ui.ToneDanger),
			statusRow(
				ui.Badge(maskInstanceURL(info.Base), ui.ToneNeutral),
				source,
				ui.Badge("Unreachable", ui.ToneDanger),
				configure))

	case !info.Configured:
		return ui.Section("Instance",
			ui.Banner("This instance is up but has no configuration yet, so it serves no streams. Open its configuration page, set up your sources there, then copy the manifest URL it gives you and paste it below.", ui.ToneWarning),
			statusRow(
				ui.Badge(maskInstanceURL(info.Base), ui.ToneNeutral),
				source,
				ui.Badge("Needs configuring", ui.ToneWarning),
				configure))

	default:
		badges := []ui.El{
			ui.Badge(maskInstanceURL(info.Base), ui.ToneNeutral),
			source,
			ui.Badge("Serving streams", ui.ToneSuccess),
		}
		if info.Version != "" {
			badges = append(badges, ui.Badge("v"+info.Version, ui.ToneNeutral))
		}
		badges = append(badges, configure)
		return ui.Section("Instance",
			ui.Banner("Mosaic resolves streams through this instance. Everything about which sources it searches, which debrid service it uses and how it filters results is configured on the instance itself, not here.", ui.ToneSuccess),
			statusRow(badges...))
	}
}

// changeInstanceSection points the module somewhere else: a self-hosted
// AIOStreams, or a configured profile on the public one.
//
// The same field does both, because from here they are the same thing — a URL
// the instance handed the user. Splitting "self-hosted" from "profile" into two
// controls would be a distinction this module cannot verify and does not need.
func changeInstanceSection(custom bool) *ui.Element {
	// A form: the field writes `instanceUrl` into the form's scope, and submit
	// merges the scope into the settings document the invoke carries. This
	// replaces the "$value" substitution, which reached one field by rewriting a
	// literal string anywhere in the action (contracts#20).
	field := ui.Form(
		ui.Vars(sdui.Vars(sdui.Var("instanceUrl", sdui.VarString, ""))),
		ui.SubmitLabel("Use this instance"),
		ui.SubmitAction(ui.Submit(
			ui.Invoke("configureModule", map[string]any{"moduleId": CapabilityID}), "settings")),
		ui.TextInput(
			ui.Prop("name", "instanceUrl"),
			ui.Prop("placeholder", "Paste an AIOStreams manifest URL…"),
			ui.Prop("validators", map[string]any{"required": true})))

	els := []ui.El{
		ui.Banner("Paste the manifest URL your AIOStreams instance gave you — from the public instance after you configure a profile, or from your own self-hosted one. The '/configure' page URL and a stremio:// install link work too.", ui.ToneInfo),
		field,
	}
	if custom {
		// Only offered when there is something to revert *to*. A reset control on
		// an install already using the default is a button that does nothing, which
		// reads as broken the first time somebody presses it.
		els = append(els, statusRow(
			ui.Component("Text", ui.Prop("text", "Back to the public instance at "+instanceHost(DefaultInstance)),
				ui.Prop("style", map[string]any{"variant": "sm", "color": "text-muted"})),
			ui.Button("Reset to default", "danger", ui.OnTap(ui.Invoke("configureModule", configureInput(""))))))
	}
	return ui.Section("Use a different instance", els...)
}

// aboutSection says what the upstream is and who runs the default, because a
// user deciding whether to trust a default they did not choose needs to know
// both.
func aboutSection() *ui.Element {
	return ui.Section("About",
		ui.Banner("AIOStreams is an independent, community-built aggregator: it searches many sources at once and returns one filtered, sorted list. Mosaic is not affiliated with it, nor with ElfHosted, who run the public instance this module defaults to.", ui.ToneInfo),
		statusRow(
			ui.Button("AIOStreams on GitHub", "ghost", ui.OnTap(ui.OpenURL("https://github.com/Viren070/AIOStreams"))),
			ui.Button("Setup guide", "ghost", ui.OnTap(ui.OpenURL("https://guides.viren070.me/stremio/addons/aiostreams/setup")))))
}

// statusRow lays controls out in a wrapping row, which is what a run of badges
// plus a button wants on a narrow screen.
func statusRow(els ...ui.El) *ui.Element {
	return ui.Component("Box",
		ui.Prop("style", map[string]any{"direction": "row", "align": "center", "gap": 2, "wrap": true}),
		ui.Group(els...))
}

// maskInstanceURL renders the instance as evidence of which one is configured
// without reproducing the profile it names.
//
// **The path segments are a credential.** An AIOStreams manifest URL carries the
// profile id and its encrypted password, and anyone holding the URL holds that
// user's whole configuration — including whatever debrid key it was built with.
// A settings screen is a page people screenshot when asking for help, so the
// host and the readable route stay, and any segment long enough to be an opaque
// id is reduced to its last four characters: enough to tell two profiles apart,
// useless to anyone else.
//
// **What this does not cover, and cannot:** the Configure button's OpenURL
// action carries the whole URL, because a link to somebody else's configuration
// page is not a link. So the credential is absent from everything *rendered* and
// present in an action payload — reaching only the admin who already passed
// `module.read` to open this screen, but out of reach of the Platform's
// redaction classes, which cannot see inside a module's settings (platform#34).
// That is the same gap `module-tmdb` records against `configureModule`, arriving
// by a different route, and it is written down rather than papered over.
func maskInstanceURL(base string) string {
	rest := base
	for _, scheme := range []string{"https://", "http://"} {
		rest = strings.TrimPrefix(rest, scheme)
	}
	segments := strings.Split(rest, "/")
	for i, seg := range segments {
		// The first segment is the host, and the short ones are route names like
		// "stremio" — both are meant to be read.
		if i == 0 || len(seg) <= 8 {
			continue
		}
		segments[i] = "••••" + seg[len(seg)-4:]
	}
	return strings.Join(segments, "/")
}

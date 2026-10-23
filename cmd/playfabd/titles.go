package main

// The PlayFab titles this service answers. Core Keeper is always on; DOOM +
// DOOM II joins once its PlayFab TitleId is captured from the Switch client
// (docs/doom-doom2.md) and configured via the environment. One process serves
// every title on the same listener, dispatching on the console's Host like
// platforms/switch/games/klei — no new port block.
//
// Credentials, mappings and log lines stay scoped per title: the store keys
// players by title:account and the token verifier pins the title, so a Core
// Keeper ticket is never valid as a DOOM ticket.

import (
	"os"
	"strings"
)

const (
	coreKeeperTitle       = "94845"
	coreKeeperApplication = "010070201d1ba000"
	coreKeeperHost        = "94845.playfabapi.com"

	// DOOM + DOOM II (2024, KEX) on Switch. Switch application from
	// Blawar/titledb (US.en.json, nsuId 70010000078053). The PlayFab TitleId
	// is not published anywhere; set PLAYFAB_DOOM_TITLE once captured.
	doomApplication = "01008cb01e52e000"
)

type titleConfig struct {
	Title       string // PlayFab TitleId the SDK sends and the host carries
	Application string // Switch application id the Nintendo token must carry
	Host        string // lowercase <title>.playfabapi.com
}

func defaultTitles() map[string]titleConfig {
	titles := map[string]titleConfig{
		coreKeeperHost: {Title: coreKeeperTitle, Application: coreKeeperApplication, Host: coreKeeperHost},
	}
	for _, extra := range titlesFromEnv() {
		titles[strings.ToLower(extra.Host)] = extra
	}
	return titles
}

// titlesFromEnv reads optional titles. Today only DOOM: PLAYFAB_DOOM_TITLE is
// the captured PlayFab TitleId, PLAYFAB_DOOM_APP overrides the Switch
// application when a regional build differs. Unset title means DOOM stays
// off and its host keeps the 503 ServiceUnavailable answer.
func titlesFromEnv() []titleConfig {
	title := strings.TrimSpace(os.Getenv("PLAYFAB_DOOM_TITLE"))
	if title == "" {
		return nil
	}
	app := strings.TrimSpace(os.Getenv("PLAYFAB_DOOM_APP"))
	if app == "" {
		app = doomApplication
	}
	host := strings.ToLower(strings.TrimSpace(title)) + ".playfabapi.com"
	return []titleConfig{{Title: title, Application: app, Host: host}}
}

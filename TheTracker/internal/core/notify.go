package core

import (
	"fmt"
	"strings"
	"time"
)

// A desktop notification when a match has been saved. Off unless the player
// switches it on: a toast over a game that has just ended is not something
// to do to someone who did not ask for it.

// Notification is one toast. View is the page that opens when it is clicked.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	View  string `json:"view"`
}

func (a *App) notify(n Notification) {
	if a.Store.LoadPrefs().General.Notify {
		a.Shell.Notify(n)
	}
}

// TestNotification shows a toast whatever the setting, so the player can see
// what switching it on will look like.
func (a *App) TestNotification() {
	a.Shell.Notify(Notification{Title: "TheTracker", Body: "This is what you'll see when a match is saved.", View: ""})
}

func (a *App) SetNotify(on bool) BackgroundSettings {
	a.Store.UpdatePrefs(func(p *Prefs) { p.General.Notify = on })
	return a.Background()
}

// heroWords turns "npc_dota_hero_shadow_fiend" into "Shadow Fiend".
func heroWords(raw *string) string {
	if raw == nil {
		return ""
	}
	words := strings.Fields(strings.ReplaceAll(strings.TrimPrefix(*raw, "npc_dota_hero_"), "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func dotaNotification(m MatchSummary) Notification {
	result := "Match saved"
	if m.Won != nil {
		result = map[bool]string{true: "Won", false: "Lost"}[*m.Won]
	}
	title := "Dota 2: " + result
	if hero := heroWords(m.HeroName); hero != "" {
		title += " as " + hero
	}
	parts := []string{fmt.Sprintf("%d kills, %d deaths", m.Kills, m.TotalDeaths)}
	if m.GPM != nil {
		parts = append(parts, fmt.Sprintf("%d GPM", *m.GPM))
	}
	if m.Duration != "" {
		parts = append(parts, m.Duration)
	}
	return Notification{Title: title, Body: strings.Join(parts, ", "), View: "sessions"}
}

func cs2Notification(m Cs2Match) Notification {
	result := map[string]string{"win": "Won", "loss": "Lost", "draw": "Drew"}[m.Result]
	if result == "" {
		result = "Left"
	}
	mapName := strings.TrimPrefix(strings.TrimPrefix(m.Map, "de_"), "cs_")
	if mapName != "" {
		mapName = strings.ToUpper(mapName[:1]) + mapName[1:]
	}
	body := fmt.Sprintf("%d / %d / %d", m.Kills, m.Deaths, m.Assists)
	if n := len(m.Rounds); n > 0 {
		body += fmt.Sprintf(", %d damage per round", m.Damage/n)
	}
	return Notification{Title: fmt.Sprintf("CS2: %s %d : %d on %s", result, m.MyScore, m.TheirScore, mapName), Body: body, View: "cs-matches"}
}

// owWatch keeps the Overwatch progress log fed while the app runs, whether
// or not its pages are open.
func (a *App) owWatch() {
	wait := time.Minute
	for {
		time.Sleep(wait)
		wait = 30 * time.Minute
		if a.Store.LoadPrefs().Games.Overwatch {
			a.Ow.Watch()
		}
	}
}

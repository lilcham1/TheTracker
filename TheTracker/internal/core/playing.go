package core

// Which game the player is in right now, for the connection check and Game
// mode, which work the same for every game: a game counts from the moment
// its program is open, so the check and the priority mark are on before the
// first match starts. Dota also counts while its live feed says a match
// runs, which covers the moment between a restart and the process scan.

import "strings"

// gameProcesses are each game's program, without ".exe".
var gameProcesses = []struct{ id, name, process string }{
	{"dota", "Dota 2", "dota2"},
	{"deadlock", "Deadlock", "project8"},
	{"cs2", "Counter-Strike 2", "cs2"},
	{"overwatch", "Overwatch", "Overwatch"},
	{"pubg", "PUBG", "TslGame"},
}

// The programs Game mode marks as priority: every game TheTracker knows.
var gamePriorityApps = []string{"dota2.exe", "project8.exe", "cs2.exe", "Overwatch.exe", "TslGame.exe"}

// runningPrograms is overridable in tests.
var runningPrograms = func() map[uint32]string { return processNames() }

// activeGame names the game being played, or "" for none. Only games the
// player switched on count.
func (a *App) activeGame() string {
	games := a.Store.LoadPrefs().Games
	if games.Dota && a.Tracker.IsLive() {
		return "Dota 2"
	}
	on := map[string]bool{"dota": games.Dota, "deadlock": games.Deadlock, "cs2": games.CS2, "overwatch": games.Overwatch, "pubg": games.PUBG}
	var running map[uint32]string
	for _, g := range gameProcesses {
		if !on[g.id] {
			continue
		}
		if running == nil {
			running = runningPrograms()
		}
		for _, p := range running {
			if strings.EqualFold(p, g.process) {
				return g.name
			}
		}
	}
	return ""
}

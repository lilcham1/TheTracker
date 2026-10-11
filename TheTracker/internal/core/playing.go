package core

// Which game the player is in right now, for the connection check and Game
// mode, which work the same for every game. Dota counts from the moment a
// match runs (its live feed says so); the others have no feed TheTracker can
// read for that, so they count while the game is open.

import "strings"

// gameProcesses are each game's program, without ".exe".
var gameProcesses = []struct{ id, name, process string }{
	{"deadlock", "Deadlock", "project8"},
	{"cs2", "Counter-Strike 2", "cs2"},
	{"overwatch", "Overwatch", "Overwatch"},
}

// The programs Game mode marks as priority: every game TheTracker knows.
var gamePriorityApps = []string{"dota2.exe", "project8.exe", "cs2.exe", "Overwatch.exe"}

// runningPrograms is overridable in tests.
var runningPrograms = func() map[uint32]string { return processNames() }

// activeGame names the game being played, or "" for none. Only games the
// player switched on count.
func (a *App) activeGame() string {
	games := a.Store.LoadPrefs().Games
	if games.Dota && a.Tracker.IsLive() {
		return "Dota 2"
	}
	on := map[string]bool{"deadlock": games.Deadlock, "cs2": games.CS2, "overwatch": games.Overwatch}
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

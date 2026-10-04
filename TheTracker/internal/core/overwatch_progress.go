package core

import (
	"sort"
	"sync"
	"time"
)

// Overwatch progress. Blizzard publishes no list of matches, only running
// totals, so the one way to see what a player did lately is to remember the
// totals each time they are read and compare. Every real fetch of the
// overview leaves a snapshot here when something changed; the difference
// between two snapshots is what was played in between.
//
// It can only see changes from the day tracking began, and only as finely as
// the profile was looked at: games played between two looks arrive as one
// lump, which is why they are grouped into sessions rather than listed.

type owHeroSnap struct {
	Games int   `json:"g"`
	Won   int   `json:"w"`
	Time  int64 `json:"t"`
}

type owSnapshot struct {
	At     int64                 `json:"at"` // unix seconds
	Mode   string                `json:"mode"`
	Games  int                   `json:"games"`
	Won    int                   `json:"won"`
	Lost   int                   `json:"lost"`
	Time   int64                 `json:"time"`
	Ranks  []OwRank              `json:"ranks"`
	Heroes map[string]owHeroSnap `json:"heroes"`
}

// Enough for years of play at a few snapshots a day.
const owMaxSnapshots = 1500

// Two changes this close together are one sitting.
const owSessionGap = 3 * time.Hour

var owProgressMu sync.Mutex

func (o *Overwatch) progressFile(playerID string) string {
	return "ow_progress_" + safeKey(playerID) + ".json"
}

func (o *Overwatch) snapshots(playerID string) []owSnapshot {
	out := []owSnapshot{}
	o.store.readJSON(o.progressFile(playerID), &out)
	return out
}

func owRanksEqual(a, b []OwRank) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Division != b[i].Division || a[i].Tier != b[i].Tier {
			return false
		}
	}
	return true
}

// remember keeps a snapshot of a freshly fetched overview, unless nothing
// moved since the last one for that mode.
func (o *Overwatch) remember(playerID string, ov OwOverview) {
	snap := owSnapshot{
		At: o.now().Unix(), Mode: ov.Mode,
		Games: ov.General.GamesPlayed, Won: ov.General.GamesWon, Lost: ov.General.GamesLost, Time: ov.General.TimePlayed,
		Ranks: ov.Ranks, Heroes: map[string]owHeroSnap{},
	}
	for _, h := range ov.Heroes {
		snap.Heroes[h.Key] = owHeroSnap{Games: h.GamesPlayed, Won: h.GamesWon, Time: h.TimePlayed}
	}

	owProgressMu.Lock()
	defer owProgressMu.Unlock()
	all := o.snapshots(playerID)
	for i := len(all) - 1; i >= 0; i-- {
		if last := all[i]; last.Mode == snap.Mode {
			if last.Games == snap.Games && last.Won == snap.Won && last.Time == snap.Time && owRanksEqual(last.Ranks, snap.Ranks) {
				return
			}
			break
		}
	}
	all = append(all, snap)
	if len(all) > owMaxSnapshots {
		all = all[len(all)-owMaxSnapshots:]
	}
	_ = o.store.writeJSON(o.progressFile(playerID), all)
}

type OwHeroDelta struct {
	Key      string  `json:"key"`
	Name     string  `json:"name"`
	Role     string  `json:"role"`
	Portrait *string `json:"portrait"`
	Games    int     `json:"games"`
	Won      int     `json:"won"`
	Time     int64   `json:"time"`
}

// OwSession is what changed on the profile over one sitting.
type OwSession struct {
	// When the change was first and last seen, unix seconds. The games were
	// played some time before each.
	From   int64         `json:"from"`
	To     int64         `json:"to"`
	Games  int           `json:"games"`
	Won    int           `json:"won"`
	Lost   int           `json:"lost"`
	Time   int64         `json:"time"`
	Heroes []OwHeroDelta `json:"heroes"`
}

type OwRankChange struct {
	At    int64    `json:"at"`
	Ranks []OwRank `json:"ranks"`
}

type OwProgress struct {
	Mode string `json:"mode"`
	// When tracking began for this mode; nil if it has not yet.
	Since    *int64      `json:"since"`
	Sessions []OwSession `json:"sessions"` // newest first
	// Each time the competitive ranks were seen to change, newest first.
	Ranks []OwRankChange `json:"ranks"`
	// Everything since tracking began.
	Total OwSession `json:"total"`
}

// Progress is what has changed on the linked profile since the app began
// watching it, for one mode.
func (o *Overwatch) Progress(mode string) OwProgress {
	if mode != "competitive" && mode != "quickplay" {
		mode = "all"
	}
	out := OwProgress{Mode: mode, Sessions: []OwSession{}, Ranks: []OwRankChange{}, Total: OwSession{Heroes: []OwHeroDelta{}}}
	link := o.Link()
	if link.PlayerID == "" {
		return out
	}
	owProgressMu.Lock()
	all := o.snapshots(link.PlayerID)
	owProgressMu.Unlock()

	info := o.heroes()
	var prev *owSnapshot
	var lastRanks []OwRank
	seenRanks := false
	totalHeroes := map[string]*OwHeroDelta{}
	for i := range all {
		s := &all[i]
		// Ranks are the same whichever mode was being looked at.
		if len(s.Ranks) > 0 && (!seenRanks || !owRanksEqual(lastRanks, s.Ranks)) {
			out.Ranks = append(out.Ranks, OwRankChange{At: s.At, Ranks: s.Ranks})
			lastRanks, seenRanks = s.Ranks, true
		}
		if s.Mode != mode {
			continue
		}
		if prev == nil {
			out.Since = ptr(s.At)
			out.Total.From = s.At
			prev = s
			continue
		}
		games, secs := s.Games-prev.Games, s.Time-prev.Time
		// Totals that went down mean the profile was reset (a new
		// competitive season): start counting again from here.
		if games < 0 || secs < 0 {
			prev = s
			continue
		}
		if games == 0 && secs == 0 {
			prev = s
			continue
		}
		d := OwSession{From: prev.At, To: s.At, Games: games, Won: max(s.Won-prev.Won, 0), Lost: max(s.Lost-prev.Lost, 0), Time: secs, Heroes: []OwHeroDelta{}}
		for key, h := range s.Heroes {
			was := prev.Heroes[key]
			hg, ht := h.Games-was.Games, h.Time-was.Time
			if hg <= 0 && ht <= 0 {
				continue
			}
			hd := OwHeroDelta{Key: key, Name: info[key].Name, Role: info[key].Role, Portrait: info[key].Portrait, Games: max(hg, 0), Won: max(h.Won-was.Won, 0), Time: max(ht, 0)}
			if hd.Name == "" {
				hd.Name = key
			}
			d.Heroes = append(d.Heroes, hd)
			if t := totalHeroes[key]; t != nil {
				t.Games, t.Won, t.Time = t.Games+hd.Games, t.Won+hd.Won, t.Time+hd.Time
			} else {
				c := hd
				totalHeroes[key] = &c
			}
		}
		out.Total.Games, out.Total.Won, out.Total.Lost, out.Total.Time = out.Total.Games+d.Games, out.Total.Won+d.Won, out.Total.Lost+d.Lost, out.Total.Time+d.Time
		out.Total.To = s.At

		// Changes seen close together are one sitting.
		if n := len(out.Sessions); n > 0 && time.Duration(d.To-out.Sessions[n-1].To)*time.Second <= owSessionGap {
			m := &out.Sessions[n-1]
			m.To, m.Games, m.Won, m.Lost, m.Time = d.To, m.Games+d.Games, m.Won+d.Won, m.Lost+d.Lost, m.Time+d.Time
			for _, hd := range d.Heroes {
				merged := false
				for j := range m.Heroes {
					if m.Heroes[j].Key == hd.Key {
						m.Heroes[j].Games, m.Heroes[j].Won, m.Heroes[j].Time = m.Heroes[j].Games+hd.Games, m.Heroes[j].Won+hd.Won, m.Heroes[j].Time+hd.Time
						merged = true
					}
				}
				if !merged {
					m.Heroes = append(m.Heroes, hd)
				}
			}
		} else {
			out.Sessions = append(out.Sessions, d)
		}
		prev = s
	}

	byTime := func(hs []OwHeroDelta) {
		sort.Slice(hs, func(i, j int) bool {
			if hs[i].Time != hs[j].Time {
				return hs[i].Time > hs[j].Time
			}
			return hs[i].Name < hs[j].Name
		})
	}
	for i := range out.Sessions {
		byTime(out.Sessions[i].Heroes)
	}
	for _, h := range totalHeroes {
		out.Total.Heroes = append(out.Total.Heroes, *h)
	}
	byTime(out.Total.Heroes)
	// Newest first.
	for i, j := 0, len(out.Sessions)-1; i < j; i, j = i+1, j-1 {
		out.Sessions[i], out.Sessions[j] = out.Sessions[j], out.Sessions[i]
	}
	for i, j := 0, len(out.Ranks)-1; i < j; i, j = i+1, j-1 {
		out.Ranks[i], out.Ranks[j] = out.Ranks[j], out.Ranks[i]
	}
	return out
}

// Watch reads the linked profile for every mode, so that a snapshot exists
// even on days the Overwatch pages are never opened.
func (o *Overwatch) Watch() {
	if o.Link().PlayerID == "" {
		return
	}
	for _, mode := range []string{"all", "competitive", "quickplay"} {
		_, _ = o.Overview(mode, false)
	}
}

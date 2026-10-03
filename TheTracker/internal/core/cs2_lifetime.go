package core

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Lifetime CS2 statistics: the totals Steam keeps for every player — kills,
// deaths, accuracy, each weapon, each map — whether or not this app was
// running. They come from Steam's Web API through the cloud function in
// convex/steam.ts, which holds the API key Steam requires so that it never
// ships inside the app.
//
// It is optional. Until the key is set on the deployment this reports "not
// set up" and the CS2 pages show only what the app recorded live.

type Cs2Weapon struct {
	Key      string   `json:"key"`
	Kills    int64    `json:"kills"`
	Shots    int64    `json:"shots"`
	Hits     int64    `json:"hits"`
	Accuracy *float64 `json:"accuracy"`
}

type Cs2MapStat struct {
	Key    string  `json:"key"`
	Rounds int64   `json:"rounds"`
	Wins   int64   `json:"wins"`
	Rate   float64 `json:"rate"`
}

type Cs2Lifetime struct {
	Available bool `json:"available"`
	// Why not, when not: not_set_up | private | no_stats | signed_out |
	// unreachable.
	Reason string `json:"reason,omitempty"`

	Kills         int64   `json:"kills"`
	Deaths        int64   `json:"deaths"`
	KD            float64 `json:"kd"`
	Headshots     int64   `json:"headshots"`
	HeadshotRate  float64 `json:"headshotRate"`
	Accuracy      float64 `json:"accuracy"`
	Damage        int64   `json:"damage"`
	ADR           float64 `json:"adr"`
	Rounds        int64   `json:"rounds"`
	RoundsWon     int64   `json:"roundsWon"`
	MatchesPlayed int64   `json:"matchesPlayed"`
	MatchesWon    int64   `json:"matchesWon"`
	MatchWinRate  float64 `json:"matchWinRate"`
	MVPs          int64   `json:"mvps"`
	HoursPlayed   float64 `json:"hoursPlayed"`
	BombsPlanted  int64   `json:"bombsPlanted"`
	BombsDefused  int64   `json:"bombsDefused"`
	KnifeKills    int64   `json:"knifeKills"`

	Weapons []Cs2Weapon  `json:"weapons"`
	Maps    []Cs2MapStat `json:"maps"`
	Freshness
}

// Things Steam counts under total_kills_* that are not weapons.
var cs2NotWeapons = map[string]bool{
	"headshot": true, "enemy_weapon": true, "enemy_blinded": true, "knife_fight": true,
	"against_zoomed_sniper": true,
}

// ParseCs2Lifetime turns Steam's flat list of named counters into the
// lifetime picture. Counters Steam does not send are simply zero.
func ParseCs2Lifetime(stats []jsonMap) Cs2Lifetime {
	v := map[string]int64{}
	for _, s := range stats {
		if name, ok := getStr(s, "name"); ok {
			v[name] = jI64(s, "value")
		}
	}
	out := Cs2Lifetime{
		Available: true,
		Kills:     v["total_kills"], Deaths: v["total_deaths"], Headshots: v["total_kills_headshot"],
		Damage: v["total_damage_done"], Rounds: v["total_rounds_played"], RoundsWon: v["total_wins"],
		MatchesPlayed: v["total_matches_played"], MatchesWon: v["total_matches_won"], MVPs: v["total_mvps"],
		BombsPlanted: v["total_planted_bombs"], BombsDefused: v["total_defused_bombs"], KnifeKills: v["total_kills_knife"],
		HoursPlayed: float64(v["total_time_played"]) / 3600,
		Weapons:     []Cs2Weapon{}, Maps: []Cs2MapStat{},
	}
	out.KD = float64(out.Kills) / float64(max(out.Deaths, 1))
	out.HeadshotRate = pct(float64(out.Headshots), float64(out.Kills))
	out.Accuracy = pct(float64(v["total_shots_hit"]), float64(v["total_shots_fired"]))
	out.MatchWinRate = pct(float64(out.MatchesWon), float64(out.MatchesPlayed))
	if out.Rounds > 0 {
		out.ADR = float64(out.Damage) / float64(out.Rounds)
	}

	for name, kills := range v {
		if key, ok := strings.CutPrefix(name, "total_kills_"); ok && kills > 0 && !cs2NotWeapons[key] {
			// A weapon is something Steam also counts shots for; the rest
			// (knife, grenades) are kept too, without accuracy.
			w := Cs2Weapon{Key: key, Kills: kills, Shots: v["total_shots_"+key], Hits: v["total_hits_"+key]}
			if w.Shots > 0 {
				w.Accuracy = ptr(pct(float64(w.Hits), float64(w.Shots)))
			}
			out.Weapons = append(out.Weapons, w)
		}
		if key, ok := strings.CutPrefix(name, "total_rounds_map_"); ok && kills > 0 {
			wins := v["total_wins_map_"+key]
			out.Maps = append(out.Maps, Cs2MapStat{Key: key, Rounds: kills, Wins: wins, Rate: pct(float64(wins), float64(kills))})
		}
	}
	sort.Slice(out.Weapons, func(i, j int) bool {
		if out.Weapons[i].Kills != out.Weapons[j].Kills {
			return out.Weapons[i].Kills > out.Weapons[j].Kills
		}
		return out.Weapons[i].Key < out.Weapons[j].Key
	})
	sort.Slice(out.Maps, func(i, j int) bool {
		if out.Maps[i].Rounds != out.Maps[j].Rounds {
			return out.Maps[i].Rounds > out.Maps[j].Rounds
		}
		return out.Maps[i].Key < out.Maps[j].Key
	})
	return out
}

// steamID64 is the 64-bit id of the account the player uses: the one they
// signed in with, or the one linked before sign-in existed.
func (a *App) steamID64() string {
	if id := a.Cloud.Auth().Steam; id != nil {
		return id.SteamID
	}
	if acc := a.Store.LoadLink("dota").AccountID; acc != nil {
		return strconv.FormatUint(*acc+steamID64Base, 10)
	}
	return ""
}

// Cs2Lifetime fetches the lifetime totals. A reason is returned in place of
// an error for every expected "no": the page explains each one differently.
func (a *App) Cs2Lifetime(force bool) Cs2Lifetime {
	id := a.steamID64()
	if id == "" {
		return Cs2Lifetime{Reason: "signed_out"}
	}
	out, fresh, err := cachedFetch(a.Store, "cs2_lifetime_"+id, 30*time.Minute, force, func() (Cs2Lifetime, error) {
		v, err := a.Cloud.call("action", "steam:cs2Stats", map[string]any{"steamId": id}, "")
		if err != nil {
			return Cs2Lifetime{}, err
		}
		m, _ := v.(map[string]any)
		if !jBool(m, "ok") {
			// Not cached as a success: returned as an error so the next
			// visit asks again (the key may have been set meanwhile).
			return Cs2Lifetime{}, &apiError{"reason:" + jStr(m, "reason", "no_stats"), false}
		}
		return ParseCs2Lifetime(jList(m["stats"])), nil
	})
	if err != nil {
		reason := "unreachable"
		if r, ok := strings.CutPrefix(err.Error(), "reason:"); ok {
			reason = r
		} else if !strings.Contains(err.Error(), "Couldn't reach") {
			// The deployment has no such function yet, or hides its error:
			// either way the feature has not been switched on.
			reason = "not_set_up"
		}
		return Cs2Lifetime{Reason: reason, Weapons: []Cs2Weapon{}, Maps: []Cs2MapStat{}}
	}
	out.Freshness = fresh
	return out
}

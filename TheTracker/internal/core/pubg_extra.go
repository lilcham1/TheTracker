package core

// More PUBG, all from the official API through the cloud service: weapon
// mastery (the account's lifetime record with each gun), survival mastery,
// the ranked leaderboard per region, and goals drawn from the player's own
// recent matches.

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

type PubgWeapon struct {
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Level     int     `json:"level"`
	Tier      int     `json:"tier"`
	Kills     int     `json:"kills"`
	Knocks    int     `json:"knocks"`
	Damage    float64 `json:"damage"`
	Headshots int     `json:"headshots"`
	Longest   float64 `json:"longest"`
	MostKills int     `json:"mostKills"`
}

type PubgWeapons struct {
	Weapons []PubgWeapon `json:"weapons"`
	// Survival mastery.
	SurvivalLevel   int       `json:"survivalLevel"`
	SurvivalTier    int       `json:"survivalTier"`
	SurvivalMatches int       `json:"survivalMatches"`
	Freshness       Freshness `json:"freshness"`
}

// Weapons is the linked account's weapon and survival mastery.
func (p *Pubg) Weapons(force bool) (PubgWeapons, error) {
	link := p.Link()
	if link.AccountID == "" {
		return PubgWeapons{}, errors.New("No PUBG account connected yet.")
	}
	out, fresh, err := cachedFetch(p.store, "pubg_weapons_"+safeKey(link.AccountID), 30*time.Minute, force, func() (PubgWeapons, error) {
		out := PubgWeapons{Weapons: []PubgWeapon{}}
		m, err := p.get(fmt.Sprintf("/shards/%s/players/%s/weapon_mastery", link.Shard, link.AccountID))
		if err != nil {
			return out, err
		}
		for key, raw := range sub(sub(sub(m, "data"), "attributes"), "weaponSummaries") {
			w, _ := raw.(map[string]any)
			s := sub(w, "StatsTotal")
			pw := PubgWeapon{Key: key, Name: pgWeaponName(pgWeaponKey(key)), Level: int(jI64(w, "LevelCurrent")), Tier: int(jI64(w, "TierCurrent")),
				Kills: int(jI64(s, "Kills")), Knocks: int(jI64(s, "Groggies")), Damage: jF64(s, "DamagePlayer"), Headshots: int(jI64(s, "HeadShots")),
				Longest: jF64(s, "LongestDefeat"), MostKills: int(jI64(s, "MostDefeatsInAGame"))}
			if pw.Kills == 0 && pw.Damage == 0 {
				continue
			}
			out.Weapons = append(out.Weapons, pw)
		}
		sort.Slice(out.Weapons, func(i, j int) bool { return out.Weapons[i].Damage > out.Weapons[j].Damage })
		// Best effort: the weapons are worth showing without it.
		if s, err := p.get(fmt.Sprintf("/shards/%s/players/%s/survival_mastery", link.Shard, link.AccountID)); err == nil {
			a := sub(sub(s, "data"), "attributes")
			out.SurvivalLevel, out.SurvivalTier, out.SurvivalMatches = int(jI64(a, "level")), int(jI64(a, "tier")), int(jI64(a, "totalMatchesPlayed"))
		}
		return out, nil
	})
	out.Freshness = fresh
	return out, err
}

// pgWeaponKey turns weapon mastery's names ("Item_Weapon_HK416_C") into
// telemetry's ("WeapHK416_C"), which pgWeaponName knows.
func pgWeaponKey(key string) string {
	const prefix = "Item_Weapon_"
	if len(key) > len(prefix) && key[:len(prefix)] == prefix {
		return "Weap" + key[len(prefix):]
	}
	return key
}

// ---------- Leaderboard ----------

type PubgBoardRow struct {
	Rank    int     `json:"rank"`
	Name    string  `json:"name"`
	Me      bool    `json:"me,omitempty"`
	Points  int     `json:"points"`
	Tier    string  `json:"tier"`
	SubTier string  `json:"subTier"`
	Games   int     `json:"games"`
	Wins    int     `json:"wins"`
	KDA     float64 `json:"kda"`
	Damage  float64 `json:"damage"`
	AvgRank float64 `json:"avgRank"`
}

type PubgBoard struct {
	Region    string         `json:"region"`
	Mode      string         `json:"mode"`
	Season    string         `json:"season"`
	Rows      []PubgBoardRow `json:"rows"`
	Freshness Freshness      `json:"freshness"`
}

var pubgRegions = map[string]bool{"as": true, "eu": true, "jp": true, "kakao": true, "krjp": true, "na": true, "oc": true, "ru": true, "sa": true, "sea": true}

// Leaderboard is the ranked top players of a region and mode this season.
func (p *Pubg) Leaderboard(region, mode string) (PubgBoard, error) {
	if !pubgRegions[region] {
		region = "eu"
	}
	if mode != "squad" && mode != "squad-fpp" {
		mode = "squad-fpp"
	}
	season, err := p.currentSeason("steam")
	if err != nil {
		return PubgBoard{}, err
	}
	me := p.Link().AccountID
	out, fresh, err := cachedFetch(p.store, "pubg_board_"+region+"_"+mode, 30*time.Minute, false, func() (PubgBoard, error) {
		out := PubgBoard{Region: region, Mode: mode, Season: season, Rows: []PubgBoardRow{}}
		m, err := p.get(fmt.Sprintf("/shards/pc-%s/leaderboards/%s/%s", region, season, mode))
		if err != nil {
			return out, err
		}
		for _, inc := range jList(m["included"]) {
			if jStr(inc, "type", "") != "player" {
				continue
			}
			a := sub(inc, "attributes")
			s := sub(a, "stats")
			out.Rows = append(out.Rows, PubgBoardRow{Rank: int(jI64(a, "rank")), Name: jStr(a, "name", ""), Points: int(jI64(s, "rankPoints")),
				Tier: jStr(s, "tier", ""), SubTier: jStr(s, "subTier", ""), Games: int(jI64(s, "games")), Wins: int(jI64(s, "wins")),
				KDA: jF64(s, "kda"), Damage: jF64(s, "averageDamage"), AvgRank: jF64(s, "averageRank")})
			if jStr(inc, "id", "") == me {
				out.Rows[len(out.Rows)-1].Me = true
			}
		}
		sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].Rank < out.Rows[j].Rank })
		return out, nil
	})
	out.Freshness = fresh
	return out, err
}

// ---------- Goals ----------

// PubgGoals sets the next match's goals from the player's own last 14 days:
// the level chosen in Settings (top 50%, 30% or 20% of their matches) for
// damage, kills, placement and time alive.
func (a *App) PubgGoals() (GoalSet, error) {
	ov, err := a.Pubg.Overview(false)
	if err != nil {
		return GoalSet{}, err
	}
	pct, label := goalLevel(a.Store.LoadPrefs().Coach.PubgLevel)
	var dmg, kills, place, alive []float64
	for _, m := range ov.Matches {
		if m.Custom {
			continue
		}
		dmg, kills = append(dmg, m.Damage), append(kills, float64(m.Kills))
		place, alive = append(place, float64(m.Place)), append(alive, m.Survived)
	}
	set := GoalSet{Game: "pubg", Level: label, Goals: []Goal{}}
	if len(dmg) < 5 {
		set.Note = "Goals start after five matches in the last 14 days."
		return set, nil
	}
	set.Goals = append(set.Goals,
		Goal{Key: "damage", Label: "Damage", Format: "count", Target: roundStep(quantile(dmg, pct), 10), Why: "Damage your top matches reach.", State: "pending"},
		Goal{Key: "kills", Label: "Kills", Format: "count", Target: float64(int(quantile(kills, pct) + 0.5)), Why: "Kills your top matches reach.", State: "pending"},
		Goal{Key: "place", Label: "Place", Format: "count", Target: float64(int(quantile(place, 1-pct) + 0.5)), AtMost: true, Why: "Where your top matches finish.", State: "pending"},
		Goal{Key: "alive", Label: "Alive", Format: "time", Target: roundStep(quantile(alive, pct), 30), Why: "How long your top matches last.", State: "pending"},
	)
	set.Note = fmt.Sprintf("From your last %d matches.", len(dmg))
	return set, nil
}

// checkPubgGoals marks a goal set against one match.
func checkPubgGoals(set GoalSet, m PubgMatch) GoalSet {
	out := set
	out.Goals = make([]Goal, len(set.Goals))
	for i, g := range set.Goals {
		var v float64
		switch g.Key {
		case "damage":
			v = m.Damage
		case "kills":
			v = float64(m.Kills)
		case "place":
			v = float64(m.Place)
		case "alive":
			v = m.Survived
		}
		g.Value = &v
		met := v >= g.Target
		if g.AtMost {
			met = v <= g.Target
		}
		g.State = "missed"
		if met {
			g.State = "met"
		}
		out.Goals[i] = g
	}
	return out
}

func roundStep(v, step float64) float64 { return float64(int(v/step+0.5)) * step }

// PubgStreams is PUBG's live Twitch streams, most watched first; reason is
// why there are none ("not_set_up", "unreachable"), or "".
func (a *App) PubgStreams() map[string]any {
	streams, reason := a.liveStreams("pubg", false)
	if streams == nil {
		streams = []LiveStream{}
	}
	return map[string]any{"streams": streams, "reason": reason}
}

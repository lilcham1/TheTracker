package core

// The AI coach: a short write-up of a reviewed match, written by Claude on
// the cloud service from the facts the review established (never from
// anything it has to guess). The cloud service holds the API key; the app
// only sends the review. One call per match, kept locally and on the
// service, so a match is never paid for twice.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type CoachFocus struct {
	Title string `json:"title"`
	Why   string `json:"why"`
	Drill string `json:"drill"`
}

type CoachDeath struct {
	Clock string `json:"clock"`
	Note  string `json:"note"`
}

type CoachNote struct {
	Verdict string       `json:"verdict"`
	Deaths  []CoachDeath `json:"deaths"`
	Focus   []CoachFocus `json:"focus"`
	Praise  string       `json:"praise"`
	Model   string       `json:"model,omitempty"`
}

type CoachResult struct {
	// ready | off | not_set_up | error
	Status string     `json:"status"`
	Note   *CoachNote `json:"note,omitempty"`
	Error  string     `json:"error,omitempty"`
}

// Coach returns the write-up for a match, asking the cloud service once.
func (a *App) Coach(game string, matchID uint64, force bool) (CoachResult, error) {
	if a.Store.LoadPrefs().Coach.AIOff {
		return CoachResult{Status: "off"}, nil
	}
	var review MatchReview
	var err error
	if game == "deadlock" {
		review, err = a.DeadlockReview(matchID)
	} else {
		game = "dota"
		review, err = a.DotaReview(matchID)
	}
	if err != nil {
		return CoachResult{}, err
	}
	key := fmt.Sprintf("coach_%s_%d", game, review.MatchID)
	var note CoachNote
	if !force {
		if _, ok := a.Store.ReadCacheStale(key, &note); ok && note.Verdict != "" {
			return CoachResult{Status: "ready", Note: &note}, nil
		}
	}
	v, err := a.Cloud.call("action", "coach:review", map[string]any{
		"game": game, "matchId": strconv.FormatUint(review.MatchID, 10), "facts": coachFacts(review), "force": force,
	}, "")
	if err != nil {
		// A deployment without the coach function answers either way.
		if strings.Contains(err.Error(), "Could not find public function") || strings.Contains(err.Error(), "Server Error") {
			return CoachResult{Status: "not_set_up"}, nil
		}
		return CoachResult{Status: "error", Error: err.Error()}, nil
	}
	out, _ := v.(map[string]any)
	if ok, _ := out["ok"].(bool); !ok {
		reason, _ := out["reason"].(string)
		if reason == "not_set_up" {
			return CoachResult{Status: "not_set_up"}, nil
		}
		return CoachResult{Status: "error", Error: coachReason(reason)}, nil
	}
	raw, _ := json.Marshal(out["note"])
	if json.Unmarshal(raw, &note) != nil || note.Verdict == "" {
		return CoachResult{Status: "error", Error: "The coach's answer couldn't be read."}, nil
	}
	a.Store.WriteCache(key, note)
	return CoachResult{Status: "ready", Note: &note}, nil
}

func coachReason(reason string) string {
	switch reason {
	case "bad_key":
		return "The cloud service's Anthropic key was refused."
	case "rate_limited":
		return "The coach is busy right now; try again in a minute."
	case "refused":
		return "The model declined to write this one."
	}
	return "The coach couldn't answer: " + reason
}

// coachFacts is the review as the model sees it: the points, the deaths
// with their context, the goals, the plain facts. No names of other
// players, no account ids.
func coachFacts(r MatchReview) map[string]any {
	result := "unknown"
	if r.Won != nil {
		if *r.Won {
			result = "won"
		} else {
			result = "lost"
		}
	}
	deaths := []map[string]any{}
	for _, d := range r.DeathLog {
		m := map[string]any{"clock": d.Clock}
		if d.Killer != "" {
			m["killer"] = d.Killer
		}
		if d.Region != "" {
			m["where"] = d.Region
		}
		if d.Fight != "" {
			m["fight"] = d.Fight
		}
		if d.Note != "" {
			m["note"] = d.Note
		}
		if len(d.Reasons) > 0 {
			m["playerState"] = d.Reasons
		}
		if d.GoldLost != nil {
			m["goldLost"] = *d.GoldLost
		}
		deaths = append(deaths, m)
	}
	points := func(ps []ReviewPoint) []map[string]any {
		out := []map[string]any{}
		for _, p := range ps {
			out = append(out, map[string]any{"title": p.Title, "detail": p.Detail})
		}
		return out
	}
	facts := map[string]any{
		"game": r.Game, "hero": r.Hero, "result": result, "durationSeconds": r.Duration,
		"kills": r.Kills, "deaths": r.Deaths, "assists": r.Assists,
		"wentWrong": points(r.Wrong), "wentWell": points(r.Good), "deathLog": deaths, "notes": r.Notes,
	}
	if len(r.Facts) > 0 {
		facts["facts"] = r.Facts
	}
	if r.Goals != nil {
		goals := []map[string]any{}
		for _, g := range r.Goals.Goals {
			m := map[string]any{"goal": g.Label, "target": g.Target, "state": g.State}
			if g.Value != nil {
				m["actual"] = *g.Value
			}
			goals = append(goals, m)
		}
		facts["goals"] = goals
		facts["role"] = r.Goals.Role
	}
	return facts
}

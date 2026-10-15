package core

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// dotaReviewRaw is the part of an OpenDota match a review needs, for the
// player only. Kept small in the cache: a full match is hundreds of KB.
type dotaReviewRaw struct {
	MatchID    uint64 `json:"matchId"`
	Duration   int    `json:"duration"`
	StartTime  int64  `json:"startTime"`
	RadiantWin bool   `json:"radiantWin"`
	GameMode   int    `json:"gameMode"`
	Parsed     bool   `json:"parsed"`
	HeroID     int    `json:"heroId"`
	Radiant    bool   `json:"radiant"`
	Kills      int    `json:"kills"`
	Deaths     int    `json:"deaths"`
	Assists    int    `json:"assists"`
	LastHits   int    `json:"lastHits"`
	GPM        int    `json:"gpm"`
	// Percentile against everyone on the same hero (OpenDota's benchmarks),
	// 0–1, and the figure it was computed from.
	Bench    map[string]float64 `json:"bench"`
	BenchRaw map[string]float64 `json:"benchRaw"`
	// Parsed matches only.
	LH10    *int     `json:"lh10,omitempty"`
	LaneEff *float64 `json:"laneEff,omitempty"`
	// Parsed matches: when each item was first bought, game clock.
	Purchases map[string]float64 `json:"purchases,omitempty"`
	// Parsed matches: fights, deaths, curves (review_fights.go).
	Facts *dotaMatchFacts `json:"facts,omitempty"`
}

// reviewRaw fetches a match for review. A match OpenDota hasn't parsed yet
// is asked to be parsed, and fetched again after half an hour.
func (d *Dota) reviewRaw(matchID uint64) (dotaReviewRaw, error) {
	key := fmt.Sprintf("od_review3_%d", matchID)
	var cached dotaReviewRaw
	if at, ok := d.store.ReadCacheStale(key, &cached); ok && cached.MatchID == matchID {
		if cached.Parsed || time.Since(at) < 30*time.Minute {
			return cached, nil
		}
	}
	me := d.store.LoadLink("dota").AccountID
	if me == nil {
		return dotaReviewRaw{}, fmt.Errorf("Connect your Steam account to review Dota matches.")
	}
	var m jsonMap
	if err := d.api.get(fmt.Sprintf("/matches/%d", matchID), &m); err != nil {
		if cached.MatchID == matchID {
			return cached, nil
		}
		return dotaReviewRaw{}, err
	}
	out := dotaReviewRaw{MatchID: matchID, Duration: int(jI64(m, "duration")), StartTime: jI64(m, "start_time"),
		RadiantWin: jBool(m, "radiant_win"), Bench: map[string]float64{}, BenchRaw: map[string]float64{}}
	_, out.Parsed = m["version"].(float64)
	out.GameMode = int(jI64(m, "game_mode"))
	found := false
	players := jList(m["players"])
	for i, p := range players {
		if jU64(p, "account_id") != *me {
			continue
		}
		found = true
		if out.Parsed {
			facts := dotaFactsFrom(m, players, i, d.Heroes())
			out.Facts = &facts
		}
		out.HeroID = int(jI64(p, "hero_id"))
		out.Radiant = jI64(p, "player_slot") < 128
		out.Kills, out.Deaths, out.Assists = int(jI64(p, "kills")), int(jI64(p, "deaths")), int(jI64(p, "assists"))
		out.LastHits, out.GPM = int(jI64(p, "last_hits")), int(jI64(p, "gold_per_min"))
		for k, raw := range sub(p, "benchmarks") {
			b, _ := raw.(map[string]any)
			if pct, ok := b["pct"].(float64); ok {
				out.Bench[k] = pct
			}
			if v, ok := b["raw"].(float64); ok {
				out.BenchRaw[k] = v
			}
		}
		if lh, ok := p["lh_t"].([]any); ok && len(lh) > 10 {
			if v, ok := lh[10].(float64); ok {
				n := int(v)
				out.LH10 = &n
			}
		}
		for _, buy := range jList(p["purchase_log"]) {
			k, t := jStr(buy, "key", ""), jF64(buy, "time")
			if out.Purchases == nil {
				out.Purchases = map[string]float64{}
			}
			if _, seen := out.Purchases[k]; k != "" && !seen {
				out.Purchases[k] = t
			}
		}
		if v, ok := p["lane_efficiency_pct"].(float64); ok {
			out.LaneEff = &v
		}
	}
	if !found {
		return dotaReviewRaw{}, fmt.Errorf("Your account isn't in that match's public data (private match data, or a different account).")
	}
	d.store.WriteCache(key, out)
	if !out.Parsed && time.Since(time.Unix(out.StartTime, 0)) < 7*24*time.Hour {
		// Best effort: OpenDota parses on request; the review gets the
		// minute-by-minute figures next time.
		_ = d.api.do(http.MethodPost, fmt.Sprintf("/request/%d", matchID), nil, nil)
	}
	return out, nil
}

// What each benchmark means for the player, and what to practise when low.
var dotaBenchmarks = []struct {
	key, title, low, high, habit string
	farm                         bool
}{
	{"gold_per_min", "Gold per minute", "Low gold per minute", "Strong gold per minute", "Between fights, always have a lane or a camp to farm; never walk around empty-handed.", true},
	{"last_hits_per_min", "Last hits", "Few last hits", "Good last hits", "Practise last hits in a demo with your hero: 10 minutes, aim for 70.", true},
	{"xp_per_min", "Experience", "Low experience", "Strong experience", "Stay near creeps and fights: time dead or walking idle costs levels.", false},
	{"hero_damage_per_min", "Damage to heroes", "Low damage to heroes", "High damage to heroes", "Be in fights from the start and keep hitting the closest target you can reach safely.", false},
	{"deaths_per_min", "Deaths", "Too many deaths", "Few deaths", "Before you walk forward, count the enemies on the minimap: the missing ones are coming for you.", false},
	{"assists_per_min", "Fight participation", "Missed fights", "In every fight", "When your team groups, go with them: join fights before they start, not after.", false},
	{"tower_damage", "Tower damage", "Little tower damage", "Pushed towers", "After a won fight, hit a tower before you go back to farming.", false},
}

// DotaReview reviews one match; 0 means the latest.
func (a *App) DotaReview(matchID uint64) (MatchReview, error) {
	if matchID == 0 {
		h, err := a.Dota.History(20, false)
		if err != nil {
			return MatchReview{}, err
		}
		for _, m := range h.Matches {
			if !m.Abandoned {
				matchID = m.MatchID
				break
			}
		}
		if matchID == 0 {
			return MatchReview{}, errNoReviewMatch
		}
	}
	raw, err := a.Dota.reviewRaw(matchID)
	if err != nil {
		return MatchReview{}, err
	}
	hero := a.Dota.Heroes()[raw.HeroID]
	won := raw.RadiantWin == raw.Radiant
	r := MatchReview{Game: "dota", MatchID: matchID, Hero: hero.Name, HeroSlug: hero.Slug, Won: &won, Duration: raw.Duration,
		StartTime: raw.StartTime, Kills: raw.Kills, Deaths: raw.Deaths, Assists: raw.Assists,
		Wrong: []ReviewPoint{}, Good: []ReviewPoint{}, DeathLog: []ReviewDeath{}, Notes: []string{}}
	if r.Hero == "" {
		r.Hero = fmt.Sprintf("Hero %d", raw.HeroID)
	}
	// A support's gold and last hits are low by design: judged on the rest.
	support := raw.BenchRaw["last_hits_per_min"] > 0 && raw.BenchRaw["last_hits_per_min"] < 2.5
	// OpenDota's hero comparisons come from normal games; Turbo's faster
	// gold and XP make them meaningless there.
	turbo := raw.GameMode == 23
	if turbo {
		r.Notes = append(r.Notes, "Turbo match: not compared with other players, whose numbers come from normal games.")
	}
	var wrong, good []ReviewPoint
	for _, b := range dotaBenchmarks {
		pct, ok := raw.Bench[b.key]
		if !ok || (support && b.farm) || turbo {
			continue
		}
		v := raw.BenchRaw[b.key]
		val := v
		figure := dotaBenchFigure(b.key, v, raw.Duration)
		switch {
		case pct < 0.3:
			detail := fmt.Sprintf("%s: lower than %d%% of %s players", figure, int(math.Round((1-pct)*100)), r.Hero)
			if b.key == "deaths_per_min" {
				detail = fmt.Sprintf("%s: more than %d%% of %s players", figure, int(math.Round((1-pct)*100)), r.Hero)
			}
			if b.key == "last_hits_per_min" && raw.LH10 != nil {
				detail += fmt.Sprintf(" (%d by 10:00)", *raw.LH10)
			}
			wrong = append(wrong, ReviewPoint{Kind: b.key, Title: b.low, Detail: detail, Habit: b.habit, Weight: 0.4 - pct, Value: &val})
		case pct >= 0.75:
			detail := fmt.Sprintf("%s: better than %d%% of %s players", figure, int(math.Round(pct*100)), r.Hero)
			good = append(good, ReviewPoint{Kind: b.key, Title: b.high, Detail: detail, Weight: pct, Value: &val})
		}
	}
	if raw.LaneEff != nil && !support && !turbo {
		if *raw.LaneEff < 55 {
			wrong = append(wrong, ReviewPoint{Kind: "lane", Title: "Weak laning", Detail: fmt.Sprintf("%.0f%% of the lane gold you could have had by 10:00", *raw.LaneEff),
				Habit: "In lane, last-hit first and trade second: a missed creep is gold you never get back.", Weight: (55 - *raw.LaneEff) / 100, Value: pctOf(*raw.LaneEff)})
		} else if *raw.LaneEff >= 80 {
			good = append(good, ReviewPoint{Kind: "lane", Title: "Strong laning", Detail: fmt.Sprintf("%.0f%% of the possible lane gold", *raw.LaneEff), Weight: *raw.LaneEff / 100})
		}
	}

	// What TheTracker saw live: each death and what went wrong in it.
	idStr := strconv.FormatUint(matchID, 10)
	var local *MatchSummary
	for _, s := range a.Store.LoadHistory() {
		if s.MatchID == idStr {
			s := s
			local = &s
		}
	}
	if raw.Facts != nil {
		fw, fg, facts := dotaFactPoints(*raw.Facts, raw.Duration, support)
		wrong, good = append(wrong, fw...), append(good, fg...)
		r.Facts = facts
		if len(raw.Facts.Deaths) > 0 {
			r.DeathLog = dotaDeathLog(raw.Facts.Deaths, local)
		}
	}
	if local != nil {
		for _, d := range local.Deaths {
			if raw.Facts == nil || len(raw.Facts.Deaths) == 0 {
				r.DeathLog = append(r.DeathLog, ReviewDeath{Clock: d.Clock, GoldLost: d.GoldLost, Region: d.Region})
			}
		}
		if local.TotalGoldLost >= 1500 {
			v := float64(local.TotalGoldLost)
			wrong = append(wrong, ReviewPoint{Kind: "gold_lost", Title: "Gold lost to deaths", Detail: fmt.Sprintf("%s gold lost across %d deaths", fmtThousands(local.TotalGoldLost), len(local.Deaths)),
				Habit: "Spend gold before fights and back off when you're carrying a lot.", Weight: math.Min(0.5, float64(local.TotalGoldLost)/8000), Value: &v})
		}
	} else {
		r.Notes = append(r.Notes, "TheTracker didn't record this match live, so what went wrong in each death isn't known.")
	}
	if !raw.Parsed {
		r.Notes = append(r.Notes, "OpenDota hasn't fully analysed this match yet (TheTracker asked it to); laning figures appear in a while.")
	}
	// The goals for the role and hero, checked against what the match gave.
	if !turbo {
		role := "core"
		if support {
			role = "support"
		}
		if set, err := a.DotaGoals(raw.HeroID, role, a.Store.LoadPrefs().Coach.DotaLevel); err == nil {
			f := dotaGoalFacts{Clock: float64(raw.Duration), Ended: true, Deaths: raw.Deaths,
				LastHits: map[int]float64{}, NetWorth: map[int]float64{}, Assists: map[int]float64{}, ItemTimes: raw.Purchases}
			if raw.LH10 != nil {
				f.LastHits[10] = float64(*raw.LH10)
			}
			if local != nil {
				for minute, cp := range local.Checkpoints {
					if cp == nil {
						continue
					}
					f.LastHits[minute] = float64(cp.LastHits)
					if cp.NetWorth > 0 {
						f.NetWorth[minute] = float64(cp.NetWorth)
					}
					if cp.NetWorth > 0 || cp.Assists > 0 {
						f.Assists[minute] = float64(cp.Assists)
					}
				}
				if len(local.ItemTimes) > 0 {
					f.ItemTimes = local.ItemTimes
				}
			}
			checked := checkDotaGoals(set, f)
			r.Goals = &checked
			// A missed goal that repeats a point already made stays in the
			// Goals section only.
			same := map[string]string{"goal_lh10": "last_hits_per_min", "goal_nw10": "gold_per_min", "goal_nw20": "gold_per_min",
				"goal_deaths": "deaths_per_min", "goal_assists20": "assists_per_min"}
			have := map[string]bool{}
			for _, p := range wrong {
				have[p.Kind] = true
			}
			for _, p := range goalPoints(checked) {
				if !have[same[p.Kind]] {
					wrong = append(wrong, p)
				}
			}
		}
	}
	r.Wrong, r.Good = sortPoints(wrong, 4), sortPoints(good, 3)
	r.Verdict = reviewVerdict(won, r.Wrong, r.Good)
	return r, nil
}

// dotaDeathLog merges the parsed match's deaths with the ones the app saw
// live (matched by clock, within ten seconds), so each has the fight, the
// place, and what the player held.
func dotaDeathLog(deaths []dotaDeathCtx, local *MatchSummary) []ReviewDeath {
	out := []ReviewDeath{}
	used := map[int]bool{}
	for _, d := range deaths {
		rd := ReviewDeath{Clock: FmtClock(d.Time), Killer: d.Killer, Region: d.Region, Fight: fightText(d)}
		if d.BuyBack {
			rd.Note = "bought back"
		}
		if d.GoldAdv <= -4000 {
			rd.Note = strings.TrimPrefix(rd.Note+", team "+fmtThousands(int64(-d.GoldAdv))+" gold down", ", ")
		}
		if local != nil {
			for i, l := range local.Deaths {
				if !used[i] && l.At > 0 && math.Abs(l.At-d.Time) <= 10 {
					used[i] = true
					rd.GoldLost = l.GoldLost
					if rd.Region == "" {
						rd.Region = l.Region
					}
					break
				}
			}
		}
		out = append(out, rd)
	}
	return out
}

func dotaBenchFigure(key string, v float64, duration int) string {
	switch key {
	case "gold_per_min":
		return fmt.Sprintf("%.0f gold a minute", v)
	case "xp_per_min":
		return fmt.Sprintf("%.0f XP a minute", v)
	case "last_hits_per_min":
		return fmt.Sprintf("%.1f last hits a minute", v)
	case "hero_damage_per_min":
		return fmt.Sprintf("%.0f damage a minute", v)
	case "deaths_per_min":
		return fmt.Sprintf("%.0f deaths", math.Round(v*float64(duration)/60))
	case "assists_per_min":
		return fmt.Sprintf("%.0f assists", math.Round(v*float64(duration)/60))
	case "tower_damage":
		return fmt.Sprintf("%.0f tower damage", v)
	}
	return fmt.Sprintf("%.1f", v)
}

// reviewVerdict says the match in one line.
func reviewVerdict(won bool, wrong, good []ReviewPoint) string {
	res := "Lost"
	if won {
		res = "Won"
	}
	lower := func(s string) string {
		if s == "" {
			return s
		}
		return strings.ToLower(s[:1]) + s[1:]
	}
	switch {
	case len(wrong) > 0 && len(good) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s. Best part: %s.", res, lower(wrong[0].Title), lower(good[0].Title))
	case len(wrong) > 0:
		return fmt.Sprintf("%s. Biggest issue: %s.", res, lower(wrong[0].Title))
	case len(good) > 0:
		return fmt.Sprintf("%s. Nothing stood out as a mistake. Best part: %s.", res, lower(good[0].Title))
	}
	return fmt.Sprintf("%s. Nothing stood out either way: an average game for you on this hero.", res)
}

// DotaWorkOn tallies the issues of the last ten matches.
func (a *App) DotaWorkOn() (WorkOn, error) {
	h, err := a.Dota.History(20, false)
	if err != nil {
		return WorkOn{}, err
	}
	var ids []uint64
	for _, m := range h.Matches {
		if !m.Abandoned && len(ids) < 10 {
			ids = append(ids, m.MatchID)
		}
	}
	return tallyWorkOn("dota", reviewMany(ids, a.DotaReview)), nil
}

package core

// Reviews of logged Call of Duty matches. With no telemetry, every point
// compares the match to the player's own log: K/D and deaths against their
// usual for that game, losing streaks and long sessions from the play
// sessions the app records, and the mode they do worst in.

import (
	"fmt"
	"sort"
	"strings"
)

// CodReview reviews one logged match, the newest when id is empty.
func (a *App) CodReview(id string) (MatchReview, error) {
	matches := a.Cod.Matches()
	var m *CodMatch
	for i := range matches {
		if id == "" || matches[i].ID == id {
			m = &matches[i]
			break
		}
	}
	if m == nil {
		return MatchReview{}, errNoReviewMatch
	}
	r := MatchReview{Game: "cod", MatchKey: m.ID, Hero: codGames[m.Game] + " · " + m.Mode, StartTime: m.Date,
		Kills: m.Kills, Deaths: m.Deaths, Assists: m.Assists, Wrong: []ReviewPoint{}, Good: []ReviewPoint{}, DeathLog: []ReviewDeath{}, Notes: []string{}}
	placed := codPlaced(m.Game)
	if placed {
		// Placed matches have no loss: only a win is marked.
		if m.Place == 1 {
			won := true
			r.Won = &won
		}
	} else if m.Result != "draw" {
		won := m.Result == "win"
		r.Won = &won
	}
	var wrong, good []ReviewPoint
	add := func(kind, title, detail, habit string, w, v float64) {
		wrong = append(wrong, ReviewPoint{Kind: kind, Title: title, Detail: detail, Habit: habit, Weight: w, Value: &v})
	}
	kd := codKD(m.Kills, m.Deaths)

	// The same game's other matches, last 90 days.
	cut := m.Date - 90*86400
	var others []CodMatch
	for _, o := range matches {
		if o.ID != m.ID && o.Game == m.Game && o.Date >= cut {
			others = append(others, o)
		}
	}
	fact := fmt.Sprintf("%d kill%s, %d death%s, %d assist%s (K/D %.2f)", m.Kills, plural(m.Kills), m.Deaths, plural(m.Deaths), m.Assists, plural(m.Assists), kd)
	if placed {
		fact = fmt.Sprintf("Placed #%d, ", m.Place) + fact
	}
	if m.Map != "" {
		fact += " on " + m.Map
	}
	r.Facts = append(r.Facts, fact)
	if m.Score > 0 {
		r.Facts = append(r.Facts, fmt.Sprintf("Score %d", m.Score))
	}
	if m.Damage > 0 {
		r.Facts = append(r.Facts, fmt.Sprintf("Damage %d", m.Damage))
	}
	if m.Note != "" {
		r.Facts = append(r.Facts, "Your note: "+m.Note)
	}

	if len(others) >= 5 {
		var kds, deaths, places, dmg []float64
		for _, o := range others {
			kds, deaths = append(kds, codKD(o.Kills, o.Deaths)), append(deaths, float64(o.Deaths))
			if o.Place > 0 {
				places = append(places, float64(o.Place))
			}
			if o.Damage > 0 {
				dmg = append(dmg, float64(o.Damage))
			}
		}
		for _, l := range [][]float64{kds, deaths, places, dmg} {
			sort.Float64s(l)
		}
		usual := quantile(kds, 0.5)
		r.Facts = append(r.Facts, fmt.Sprintf("Your usual %s K/D is %.2f, from %d logged matches", codGames[m.Game], usual, len(others)))
		lowKD := kd < quantile(kds, 0.25) && m.Deaths >= 5
		if lowKD {
			add("low_kd", "Traded badly", fmt.Sprintf("K/D %.2f; your usual is %.2f", kd, usual),
				"Fight next to a teammate so each duel is two guns on one, and reload or move after a kill instead of pushing on half health.", 0.5, kd)
		} else if kd >= quantile(kds, 0.75) && m.Kills > 0 {
			good = append(good, ReviewPoint{Kind: "kd", Title: "Strong K/D", Detail: fmt.Sprintf("K/D %.2f, better than three quarters of your matches", kd), Weight: 0.6})
		}
		if !placed && !lowKD && float64(m.Deaths) > quantile(deaths, 0.75) && m.Deaths >= 8 {
			add("many_deaths", "Died more than usual", fmt.Sprintf("%d deaths; you usually have %.0f", m.Deaths, quantile(deaths, 0.5)),
				"Hold one lane with cover and let them come to you: most extra deaths come from crossing open lanes and re-peeking the same angle.", 0.4, float64(m.Deaths))
		}
		if placed && len(places) >= 5 && float64(m.Place) > quantile(places, 0.75) {
			add("early_out", "Out early", fmt.Sprintf("#%d; you usually reach #%.0f", m.Place, quantile(places, 0.5)),
				"Land a little away from the hottest spot and get full plates and your loadout before taking fights.", 0.5, float64(m.Place))
		}
		if placed && len(dmg) >= 5 && m.Damage > 0 {
			if float64(m.Damage) < quantile(dmg, 0.25) {
				add("low_damage", "Little damage", fmt.Sprintf("%d damage; you usually deal %.0f", m.Damage, quantile(dmg, 0.5)),
					"Take the fights at range you can win from cover: damage is what wins the loadout race.", 0.3, float64(m.Damage))
			} else if float64(m.Damage) >= quantile(dmg, 0.75) {
				good = append(good, ReviewPoint{Kind: "damage", Title: "Big damage", Detail: fmt.Sprintf("%d damage, more than three quarters of your matches", m.Damage), Weight: 0.6})
			}
		}
		// The mode this player wins least, in multiplayer.
		if !placed && m.Result == "loss" {
			type wl struct{ w, n int }
			byMode := map[string]*wl{}
			all := wl{}
			for _, o := range append(others, *m) {
				if o.Result == "" {
					continue
				}
				x := byMode[o.Mode]
				if x == nil {
					x = &wl{}
					byMode[o.Mode] = x
				}
				x.n++
				all.n++
				if o.Result == "win" {
					x.w++
					all.w++
				}
			}
			worst, worstRate := "", 2.0
			for mode, x := range byMode {
				if x.n < 4 {
					continue
				}
				if rate := float64(x.w) / float64(x.n); rate < worstRate {
					worst, worstRate = mode, rate
				}
			}
			overall := float64(all.w) / float64(max(all.n, 1))
			if worst == m.Mode && len(byMode) > 1 && worstRate < overall-0.1 {
				add("weak_mode", "Your weakest mode", fmt.Sprintf("You win %.0f%% of %s, %.0f%% overall", worstRate*100, m.Mode, overall*100),
					"Learn this mode's spawns and objectives: watch one pro match of it, then play three in a row playing only the objective.", 0.3, worstRate)
			}
		}
	} else {
		r.Notes = append(r.Notes, fmt.Sprintf("Comparisons with your usual start after five logged %s matches.", codGames[m.Game]))
	}

	// The session around it: streaks and fatigue.
	if m.Session != "" {
		var same []CodMatch
		for _, o := range matches {
			if o.Session == m.Session && o.Date <= m.Date {
				same = append(same, o)
			}
		}
		sort.SliceStable(same, func(i, j int) bool { return same[i].Date < same[j].Date })
		lost := func(o CodMatch) bool {
			if codPlaced(o.Game) {
				return o.Place > 10
			}
			return o.Result == "loss"
		}
		streak := 0
		for i := len(same) - 1; i >= 0 && lost(same[i]); i-- {
			streak++
		}
		if streak >= 3 {
			add("tilt", "Losing streak", fmt.Sprintf("Loss %d in a row this session", streak),
				"After two losses in a row, stop for five minutes: tilt shows up as rushed fights and worse aim.", 0.45, float64(streak))
		}
		for _, s := range a.Cod.Sessions() {
			if s.ID != m.Session {
				continue
			}
			in := m.Date - s.Start
			if in > 150*60 && len(others) >= 5 && kd < medianKD(others) {
				add("long_session", "Late in a long session", fmt.Sprintf("Logged %s into the session, with a K/D under your usual", FmtClock(float64(in))),
					"Aim and decisions drop after about two hours: end on a planned break, not on a loss.", 0.35, float64(in)/60)
			}
			r.Facts = append(r.Facts, fmt.Sprintf("Match %d of a session that started %s before it was logged", len(same), FmtClock(float64(in))))
		}
	}

	if placed {
		if m.Place == 1 {
			good = append(good, ReviewPoint{Kind: "win", Title: "Won the match", Detail: fmt.Sprintf("%d kills", m.Kills), Weight: 1})
		} else if m.Place <= 10 {
			good = append(good, ReviewPoint{Kind: "top10", Title: "Top 10", Detail: fmt.Sprintf("#%d", m.Place), Weight: 0.5})
		}
	} else if m.Result == "win" {
		good = append(good, ReviewPoint{Kind: "win", Title: "Won", Detail: m.Mode, Weight: 0.5})
	}
	set := a.CodGoals(m.Game)
	if len(set.Goals) > 0 {
		checked := checkCodGoals(set, *m)
		r.Goals = &checked
	}
	r.Wrong, r.Good = sortPoints(wrong, 4), sortPoints(good, 3)
	r.Verdict = codVerdict(*m, r.Wrong, r.Good)
	return r, nil
}

func medianKD(ms []CodMatch) float64 {
	var kds []float64
	for _, o := range ms {
		kds = append(kds, codKD(o.Kills, o.Deaths))
	}
	sort.Float64s(kds)
	return quantile(kds, 0.5)
}

func codVerdict(m CodMatch, wrong, good []ReviewPoint) string {
	res := map[string]string{"win": "Won", "loss": "Lost", "draw": "Draw"}[m.Result]
	if codPlaced(m.Game) {
		res = fmt.Sprintf("#%d", m.Place)
		if m.Place == 1 {
			res = "Won"
		}
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
	return res + ". Nothing stood out either way."
}

// CodWorkOn tallies the issues of the last ten logged matches.
func (a *App) CodWorkOn() (WorkOn, error) {
	var reviews []MatchReview
	for i, m := range a.Cod.Matches() {
		if i >= 10 {
			break
		}
		if r, err := a.CodReview(m.ID); err == nil {
			reviews = append(reviews, r)
		}
	}
	return tallyWorkOn("cod", reviews), nil
}

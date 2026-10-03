package core

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Insights: plain-language observations drawn from the player's own recorded
// sessions. Every figure is arithmetic on what the live feed recorded about
// their own play — last hits at each mark, when they died, what they bought
// and when. Nothing is inferred about anyone else.

type Insight struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// good | bad | neutral — how the UI tints it.
	Tone string `json:"tone"`
}

// GoalResult is how one session measured up against the player's goals.
type GoalResult struct {
	Key    string `json:"key"` // lastHits10 | maxDeaths | minGpm
	Label  string `json:"label"`
	Target int    `json:"target"`
	// Nil when the session has no value for this goal (a match that ended
	// before 10:00 has no last hits at 10).
	Value *int `json:"value"`
	Met   bool `json:"met"`
}

// EvaluateGoals scores one session. Goals set to zero are skipped.
func EvaluateGoals(g Goals, m *MatchSummary) []GoalResult {
	out := []GoalResult{}
	if g.LastHits10 > 0 {
		r := GoalResult{Key: "lastHits10", Label: "Last hits at 10:00", Target: g.LastHits10}
		if cp := m.Checkpoints[10]; cp != nil {
			r.Value = ptr(int(cp.LastHits))
			r.Met = int(cp.LastHits) >= g.LastHits10
		}
		out = append(out, r)
	}
	if g.MaxDeaths > 0 {
		out = append(out, GoalResult{Key: "maxDeaths", Label: "Deaths at most", Target: g.MaxDeaths, Value: ptr(m.TotalDeaths), Met: m.TotalDeaths <= g.MaxDeaths})
	}
	if g.MinGPM > 0 {
		r := GoalResult{Key: "minGpm", Label: "GPM at least", Target: g.MinGPM}
		if m.GPM != nil {
			r.Value = ptr(int(*m.GPM))
			r.Met = int(*m.GPM) >= g.MinGPM
		}
		out = append(out, r)
	}
	return out
}

type GoalProgress struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Target int    `json:"target"`
	// Sessions that had a value for this goal, and how many met it.
	Counted int `json:"counted"`
	Met     int `json:"met"`
}

// GoalsProgress totals each goal over the most recent sessions.
func GoalsProgress(g Goals, history []MatchSummary, last int) []GoalProgress {
	if last > 0 && len(history) > last {
		history = history[len(history)-last:]
	}
	byKey := map[string]*GoalProgress{}
	var order []string
	for i := range history {
		for _, r := range EvaluateGoals(g, &history[i]) {
			p := byKey[r.Key]
			if p == nil {
				p = &GoalProgress{Key: r.Key, Label: r.Label, Target: r.Target}
				byKey[r.Key] = p
				order = append(order, r.Key)
			}
			if r.Value != nil {
				p.Counted++
				if r.Met {
					p.Met++
				}
			}
		}
	}
	out := []GoalProgress{}
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	// With no sessions yet, still list the goals that are set.
	if len(history) == 0 {
		for _, r := range EvaluateGoals(g, &MatchSummary{}) {
			out = append(out, GoalProgress{Key: r.Key, Label: r.Label, Target: r.Target})
		}
	}
	return out
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

// minSessionsForInsights: below this, averages are noise.
const minSessionsForInsights = 3

var itemLabels = map[string]string{
	"black_king_bar": "Black King Bar", "blink": "Blink Dagger", "ultimate_scepter": "Aghanim's Scepter",
	"power_treads": "Power Treads", "phase_boots": "Phase Boots", "manta": "Manta Style",
	"greater_crit": "Daedalus", "sphere": "Linken's Sphere", "sheepstick": "Scythe of Vyse",
	"aghanims_shard": "Aghanim's Shard", "arcane_boots": "Arcane Boots", "travel_boots": "Boots of Travel",
}

func itemLabel(key string) string {
	if l, ok := itemLabels[key]; ok {
		return l
	}
	words := strings.Split(key, "_")
	for i, w := range words {
		if w != "" && w != "of" && w != "and" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// BuildInsights reads the session history (oldest first) and returns what is
// worth saying about it.
func BuildInsights(history []MatchSummary) []Insight {
	out := []Insight{}
	n := len(history)
	if n < minSessionsForInsights {
		return out
	}

	// --- Last hits at 10 minutes: level and direction.
	var lh10 []float64
	for _, m := range history {
		if cp := m.Checkpoints[10]; cp != nil {
			lh10 = append(lh10, float64(cp.LastHits))
		}
	}
	if len(lh10) >= minSessionsForInsights {
		avg := mean(lh10)
		detail := fmt.Sprintf("You average %.0f last hits at 10:00 across %d recorded games.", avg, len(lh10))
		tone := "neutral"
		if len(lh10) >= 6 {
			half := len(lh10) / 2
			before, after := mean(lh10[:half]), mean(lh10[half:])
			switch diff := after - before; {
			case diff >= 3:
				detail += fmt.Sprintf(" Your recent games are up %.0f on the earlier ones.", diff)
				tone = "good"
			case diff <= -3:
				detail += fmt.Sprintf(" Your recent games are down %.0f on the earlier ones.", -diff)
				tone = "bad"
			default:
				detail += " That has been steady."
			}
		}
		out = append(out, Insight{Title: "Farming by 10 minutes", Detail: detail, Tone: tone})
	}

	// --- When deaths happen.
	phases := []struct {
		label    string
		from, to int
	}{{"the first 10 minutes", 0, 600}, {"10 to 20 minutes", 600, 1200}, {"20 to 30 minutes", 1200, 1800}, {"after 30 minutes", 1800, math.MaxInt}}
	counts := make([]int, len(phases))
	totalDeaths, goldLost, goldDeaths := 0, int64(0), 0
	for _, m := range history {
		for _, d := range m.Deaths {
			if d.GoldLost != nil {
				goldLost += *d.GoldLost
				goldDeaths++
			}
			secs, ok := ParseClock(d.Clock)
			if !ok {
				continue
			}
			totalDeaths++
			for i, p := range phases {
				if secs >= p.from && secs < p.to {
					counts[i]++
				}
			}
		}
	}
	if totalDeaths >= 8 {
		worst := 0
		for i := range counts {
			if counts[i] > counts[worst] {
				worst = i
			}
		}
		share := pct(float64(counts[worst]), float64(totalDeaths))
		if share >= 35 {
			out = append(out, Insight{
				Title:  "When you die",
				Detail: fmt.Sprintf("%.0f%% of your deaths come in %s (%d of %d). That is the stretch to play more carefully.", share, phases[worst].label, counts[worst], totalDeaths),
				Tone:   "bad",
			})
		}
	}
	if goldDeaths >= 5 {
		out = append(out, Insight{
			Title:  "What a death costs",
			Detail: fmt.Sprintf("Each death costs you %d gold on average, and you die %.1f times a game. Spending before a risky fight keeps that gold on your hero.", goldLost/int64(goldDeaths), float64(totalDeaths)/float64(n)),
			Tone:   "neutral",
		})
	}

	// --- Deaths against results.
	var winDeaths, lossDeaths []float64
	for _, m := range history {
		if m.Won == nil {
			continue
		}
		if *m.Won {
			winDeaths = append(winDeaths, float64(m.TotalDeaths))
		} else {
			lossDeaths = append(lossDeaths, float64(m.TotalDeaths))
		}
	}
	if len(winDeaths) >= 3 && len(lossDeaths) >= 3 {
		w, l := mean(winDeaths), mean(lossDeaths)
		if l-w >= 1 {
			out = append(out, Insight{
				Title:  "Deaths and results",
				Detail: fmt.Sprintf("You die %.1f times in games you win and %.1f in games you lose.", w, l),
				Tone:   "neutral",
			})
		}
	}

	// --- Timing of the item bought most often.
	type timing struct {
		secs []float64
	}
	firsts := map[string]*timing{}
	for _, m := range history {
		seen := map[string]bool{}
		for _, k := range m.KeyItems {
			if seen[k.Item] || k.Item == "boots" {
				continue
			}
			seen[k.Item] = true
			if secs, ok := ParseClock(k.Clock); ok {
				if firsts[k.Item] == nil {
					firsts[k.Item] = &timing{}
				}
				firsts[k.Item].secs = append(firsts[k.Item].secs, float64(secs))
			}
		}
	}
	var items []string
	for k, t := range firsts {
		if len(t.secs) >= 3 {
			items = append(items, k)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := firsts[items[i]], firsts[items[j]]
		if len(a.secs) != len(b.secs) {
			return len(a.secs) > len(b.secs)
		}
		return items[i] < items[j]
	})
	if len(items) > 2 {
		items = items[:2]
	}
	for _, k := range items {
		t := firsts[k]
		best := t.secs[0]
		for _, s := range t.secs {
			best = math.Min(best, s)
		}
		out = append(out, Insight{
			Title:  itemLabel(k) + " timing",
			Detail: fmt.Sprintf("You finish %s at %s on average over %d games. Your fastest was %s.", itemLabel(k), FmtClock(mean(t.secs)), len(t.secs), FmtClock(best)),
			Tone:   "neutral",
		})
	}

	// --- Recent form, where results are known.
	var results []bool
	for _, m := range history {
		if m.Won != nil {
			results = append(results, *m.Won)
		}
	}
	if len(results) >= 5 {
		recent := results[max(0, len(results)-10):]
		wins := 0
		for _, w := range recent {
			if w {
				wins++
			}
		}
		tone := "neutral"
		switch rate := pct(float64(wins), float64(len(recent))); {
		case rate >= 60:
			tone = "good"
		case rate <= 40:
			tone = "bad"
		}
		out = append(out, Insight{
			Title:  "Recent form",
			Detail: fmt.Sprintf("You won %d of your last %d recorded games.", wins, len(recent)),
			Tone:   tone,
		})
	}
	return out
}

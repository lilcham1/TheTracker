package core

// The AI coach: a short write-up of a reviewed match, written by Claude
// Sonnet 5.5 from the facts the review established (never from anything it
// has to guess). It runs on this PC with the owner's own Anthropic sign-in
// (`ant auth login`, or ANTHROPIC_API_KEY), and only for the owner's Steam
// account: everyone else never sees it. One call per match, kept locally,
// so a match is never paid for twice.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const coachModel = "claude-sonnet-5-5"

// The owner's Steam account, as sha256("thetracker-coach-owner:" + id), so
// the account id itself isn't in the source.
const coachOwnerHash = "a071e257df63306d0f09c308dd96ea3f692670b698305c2f7c3b0cbc8eda70f1"

func coachOwnerOf(accountID uint64) bool {
	sum := sha256.Sum256([]byte("thetracker-coach-owner:" + strconv.FormatUint(accountID, 10)))
	return hex.EncodeToString(sum[:]) == coachOwnerHash
}

// CoachUnlocked says whether this install belongs to the owner: the linked
// Dota or Deadlock account, or the Steam sign-in, is theirs.
func (a *App) CoachUnlocked() bool {
	for _, game := range []string{"dota", "deadlock"} {
		if id := a.Store.LoadLink(game).AccountID; id != nil && coachOwnerOf(*id) {
			return true
		}
	}
	if st := a.Cloud.Auth().Steam; st != nil && coachOwnerOf(st.AccountID) {
		return true
	}
	return false
}

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
	// ready | locked | off | not_set_up | error
	Status string     `json:"status"`
	Note   *CoachNote `json:"note,omitempty"`
	Error  string     `json:"error,omitempty"`
}

const coachSystem = `You are a coach for a solo-queue player of Dota 2 and Deadlock, reading one of their matches between games.
You get the facts an analyzer established from the public match data and from what the player's own client reported: the review points, each death with who killed them, where, the fight it was part of and how the game stood, the role goals, net worth curves, time dead, wards.
Rules:
- Use only those facts. Never invent events, positions, items or numbers that aren't in them. If the facts are thin, say less.
- Talk to the player directly, plainly, like a coach who respects them: specific, no fluff, no jargon they wouldn't know, no praise sandwiches.
- Find the thread: what one or two habits explain most of what went wrong, and what the cheapest fix is. Prefer the pattern across deaths over listing each death.
- Each death note says what to do differently next time in that spot, in at most 25 words, keyed by the death's clock exactly as given. Skip a note when a death was fine (a good trade, a sacrifice that won the fight).
- Focus items: at most three, each a habit (title up to 6 words), why it mattered in this match (up to 30 words), and one concrete drill or rule to practise next game (up to 30 words).
- The verdict is two sentences at most. Praise is one sentence on what to keep doing, grounded in a fact; empty if nothing stood out.
Answer with the JSON only.`

var coachSchema = map[string]any{
	"type": "object", "additionalProperties": false,
	"properties": map[string]any{
		"verdict": map[string]any{"type": "string"},
		"deaths": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"clock": map[string]any{"type": "string"}, "note": map[string]any{"type": "string"}},
			"required":   []string{"clock", "note"},
		}},
		"focus": map[string]any{"type": "array", "items": map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{"title": map[string]any{"type": "string"}, "why": map[string]any{"type": "string"}, "drill": map[string]any{"type": "string"}},
			"required":   []string{"title", "why", "drill"},
		}},
		"praise": map[string]any{"type": "string"},
	},
	"required": []string{"verdict", "deaths", "focus", "praise"},
}

// coachAsk sends one review to the model; swapped in tests.
var coachAsk = func(ctx context.Context, game string, facts map[string]any) (CoachNote, error) {
	raw, _ := json.Marshal(facts)
	name := "Dota 2"
	if game == "deadlock" {
		name = "Deadlock"
	}
	// Credentials come from the SDK's standard chain: ANTHROPIC_API_KEY, or
	// the profile `ant auth login` keeps on this PC.
	client := anthropic.NewClient(option.WithRequestTimeout(2 * time.Minute))
	msg, err := client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     coachModel,
		MaxTokens: 4000,
		// A policy decline is re-served by a fallback model in the same call.
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		System:    []anthropic.BetaTextBlockParam{{Text: coachSystem}},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortMedium,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: coachSchema},
		},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("Game: " + name + "\nFacts:\n" + string(raw))),
		},
	})
	if err != nil {
		return CoachNote{}, err
	}
	if msg.StopReason == anthropic.BetaStopReasonRefusal {
		return CoachNote{}, errCoachRefused
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	var note CoachNote
	if err := json.Unmarshal([]byte(text.String()), &note); err != nil {
		return CoachNote{}, errors.New("The coach's answer couldn't be read.")
	}
	note.Model = string(msg.Model)
	return note, nil
}

var errCoachRefused = errors.New("The model declined to write this one.")

// Coach returns the write-up for a match, asking the model once.
func (a *App) Coach(game string, matchID uint64, force bool) (CoachResult, error) {
	if !a.CoachUnlocked() {
		return CoachResult{Status: "locked"}, nil
	}
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
	note, err = coachAsk(context.Background(), game, coachFacts(review))
	if err != nil {
		return coachError(err), nil
	}
	a.Store.WriteCache(key, note)
	return CoachResult{Status: "ready", Note: &note}, nil
}

// coachError says what went wrong in words the player can act on.
func coachError(err error) CoachResult {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
			return CoachResult{Status: "not_set_up", Error: "Your Anthropic sign-in was refused or has expired."}
		case apiErr.StatusCode == 429:
			return CoachResult{Status: "error", Error: "Anthropic is rate-limiting your account right now; try again in a minute."}
		case apiErr.StatusCode == 400 && strings.Contains(strings.ToLower(apiErr.Error()), "credit"):
			return CoachResult{Status: "error", Error: "Your Anthropic account has no API credits left (platform.claude.com, Billing)."}
		}
		return CoachResult{Status: "error", Error: fmt.Sprintf("Anthropic answered with an error (HTTP %d).", apiErr.StatusCode)}
	}
	// The SDK's no-credentials error type is internal to it; its message is
	// the only handle on it.
	if strings.Contains(err.Error(), "no Anthropic credentials found") {
		return CoachResult{Status: "not_set_up"}
	}
	return CoachResult{Status: "error", Error: err.Error()}
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

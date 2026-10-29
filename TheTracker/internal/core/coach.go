package core

// The AI coach: a short write-up of a reviewed match, written by Claude
// Sonnet 5.5 from the facts the review established (never from anything it
// has to guess). It runs through the owner's own Claude Code on this PC, in
// headless mode, so their Claude plan covers it; and only for the owner's
// Steam account: everyone else never sees it. One call per match, kept
// locally, so a match is never asked for twice.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

const coachSystem = `You are a coach for a player of Dota 2, Deadlock, PUBG: Battlegrounds, Call of Duty and VALORANT, reading one of their matches between games.
You get the facts an analyzer established from the public match data and from what the player's own client reported: the review points, each death with who killed them, where, the fight it was part of and how the game stood, the role goals, net worth curves, time dead, wards.
For Call of Duty there is no match data: the facts are what the player logged themselves (mode, result, kills, deaths, an optional note) compared with their own log and play sessions, so coach their habits and decisions from those figures and their note, and give no death notes.
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
	name := map[string]string{"dota": "Dota 2", "deadlock": "Deadlock", "pubg": "PUBG: Battlegrounds", "cod": "Call of Duty", "valorant": "VALORANT"}[game]
	bin := claudeCodePath()
	if bin == "" {
		return CoachNote{}, errNoClaudeCode
	}
	schema, _ := json.Marshal(coachSchema)
	args := []string{"-p", "--output-format", "json", "--model", coachModel, "--effort", "medium",
		"--system-prompt", coachSystem, "--json-schema", string(schema),
		// A write-up needs no tools, no MCP servers, and no saved session.
		"--tools", "", "--strict-mcp-config", "--no-session-persistence"}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	out, err := coachExec(ctx, bin, args, "Game: "+name+"\nFacts:\n"+string(raw))
	if ctx.Err() != nil {
		return CoachNote{}, errors.New("Claude Code took too long to answer.")
	}
	return parseClaudeCodeNote(out, err)
}

// coachExec runs Claude Code with the prompt on stdin; swapped in tests.
var coachExec = func(ctx context.Context, bin string, args []string, stdin string) ([]byte, error) {
	cmd := hidden(bin, args...)
	// Its own folder, so no project's CLAUDE.md or settings come along;
	// and no API key in its environment, so the Claude plan pays.
	dir := filepath.Join(os.TempDir(), "thetracker-coach")
	_ = os.MkdirAll(dir, 0o755)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(kv, "=", 2)[0])
		if k == "ANTHROPIC_API_KEY" || k == "ANTHROPIC_AUTH_TOKEN" || k == "CLAUDECODE" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Stdin = strings.NewReader(stdin)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return stdout.Bytes(), err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return stdout.Bytes(), ctx.Err()
	}
}

// claudeCodePath finds the claude program: on the PATH, or where its
// installer puts it.
func claudeCodePath() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", "claude.exe"),
		filepath.Join(os.Getenv("APPDATA"), "npm", "claude.cmd"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// parseClaudeCodeNote reads Claude Code's --output-format json answer.
func parseClaudeCodeNote(out []byte, runErr error) (CoachNote, error) {
	var res struct {
		IsError          bool            `json:"is_error"`
		Subtype          string          `json:"subtype"`
		Result           string          `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if json.Unmarshal(out, &res) != nil {
		if runErr != nil {
			return CoachNote{}, fmt.Errorf("Claude Code didn't run: %v", runErr)
		}
		return CoachNote{}, errors.New("Claude Code's answer couldn't be read.")
	}
	if res.IsError {
		return CoachNote{}, claudeCodeError(res.Result)
	}
	body := []byte(res.Result)
	if len(res.StructuredOutput) > 0 && string(res.StructuredOutput) != "null" {
		body = res.StructuredOutput
	}
	text := strings.TrimSpace(string(body))
	text = strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```")
	text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	var note CoachNote
	if err := json.Unmarshal([]byte(text), &note); err != nil || note.Verdict == "" {
		return CoachNote{}, errors.New("The coach's answer couldn't be read.")
	}
	note.Model = coachModel + " via Claude Code"
	return note, nil
}

// claudeCodeError turns Claude Code's own error text into an error the
// coach can sort.
func claudeCodeError(msg string) error {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "/login") || strings.Contains(low, "not logged in") || strings.Contains(low, "invalid api key") || strings.Contains(low, "oauth"):
		return errClaudeCodeSignIn
	case strings.Contains(low, "limit"):
		return fmt.Errorf("Your Claude plan's usage limit is reached for now: %s", strings.TrimSpace(msg))
	}
	if msg == "" {
		msg = "no details"
	}
	return fmt.Errorf("Claude Code couldn't write it: %s", strings.TrimSpace(msg))
}

var (
	errNoClaudeCode     = errors.New("Claude Code isn't installed on this PC.")
	errClaudeCodeSignIn = errors.New("Claude Code isn't signed in to your Claude account.")
)

var errCoachRefused = errors.New("The model declined to write this one.")

// Coach returns the write-up for a match, asking the model once.
func (a *App) Coach(game string, matchID uint64, matchKey string, force bool) (CoachResult, error) {
	if !a.CoachUnlocked() {
		return CoachResult{Status: "locked"}, nil
	}
	if a.Store.LoadPrefs().Coach.AIOff {
		return CoachResult{Status: "off"}, nil
	}
	var review MatchReview
	var err error
	switch game {
	case "pubg":
		review, err = a.PubgReview(matchKey)
	case "cod":
		review, err = a.CodReview(matchKey)
	case "valorant":
		review, err = a.ValorantReview(matchKey)
	case "deadlock":
		review, err = a.DeadlockReview(matchID)
	default:
		game = "dota"
		review, err = a.DotaReview(matchID)
	}
	if err != nil {
		return CoachResult{}, err
	}
	key := fmt.Sprintf("coach_%s_%d", game, review.MatchID)
	if review.MatchKey != "" {
		key = "coach_" + game + "_" + safeKey(review.MatchKey)
	}
	var note CoachNote
	if !force {
		if _, ok := a.Store.ReadCacheStale(key, &note); ok && note.Verdict != "" {
			return CoachResult{Status: "ready", Note: &note}, nil
		}
	}
	note, err = coachAsk(context.Background(), game, coachFacts(review))
	if err != nil {
		res := coachError(err)
		// The last failure, for troubleshooting without the UI.
		a.Store.WriteCache("coach_last_error", res)
		return res, nil
	}
	a.Store.WriteCache(key, note)
	return CoachResult{Status: "ready", Note: &note}, nil
}

// coachError says what went wrong in words the player can act on.
func coachError(err error) CoachResult {
	if errors.Is(err, errNoClaudeCode) || errors.Is(err, errClaudeCodeSignIn) {
		return CoachResult{Status: "not_set_up", Error: err.Error()}
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
		if d.Killer != "" && r.Game != "pubg" && r.Game != "valorant" {
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

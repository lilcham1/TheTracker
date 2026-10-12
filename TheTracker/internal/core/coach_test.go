package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func coachTestApp(t *testing.T, me uint64) *App {
	t.Helper()
	f := newFakeAPI(t, map[string]any{
		"/heroes": odHeroes,
		"/matches/100": map[string]any{
			"match_id": 100, "duration": 2400, "start_time": 1700000000, "radiant_win": false, "game_mode": 22,
			"players": []any{map[string]any{"account_id": float64(me), "hero_id": 1, "player_slot": 0, "kills": 4, "deaths": 9, "assists": 3, "last_hits": 220, "gold_per_min": 410,
				"benchmarks": map[string]any{"deaths_per_min": map[string]any{"raw": 0.225, "pct": 0.05}}}},
		},
	})
	a := newTestApp(t)
	a.Dota.api = &service{Name: "OpenDota", Base: f.srv.URL, Attempts: 1}
	a.Store.SaveLink("dota", Link{AccountID: &me})
	return a
}

func stubCoach(t *testing.T, ask func(context.Context, string, map[string]any) (CoachNote, error)) *int {
	t.Helper()
	calls := 0
	old := coachAsk
	coachAsk = func(ctx context.Context, game string, facts map[string]any) (CoachNote, error) {
		calls++
		return ask(ctx, game, facts)
	}
	t.Cleanup(func() { coachAsk = old })
	return &calls
}

func TestCoachIsLockedForEveryoneButTheOwner(t *testing.T) {
	a := coachTestApp(t, 42)
	calls := stubCoach(t, func(context.Context, string, map[string]any) (CoachNote, error) { return CoachNote{Verdict: "x"}, nil })
	if a.CoachUnlocked() {
		t.Fatal("account 42 isn't the owner")
	}
	if r, err := a.Coach("dota", 100, false); err != nil || r.Status != "locked" || *calls != 0 {
		t.Fatalf("locked: %+v %v calls %d", r, err, *calls)
	}
	if coachOwnerOf(42) || !coachOwnerOf(850402858) {
		t.Fatal("owner hash")
	}
}

func TestCoachAsksOnceAndKeepsTheAnswer(t *testing.T) {
	const owner = 850402858
	a := coachTestApp(t, owner)
	if !a.CoachUnlocked() {
		t.Fatal("owner should be unlocked")
	}
	var seen map[string]any
	calls := stubCoach(t, func(_ context.Context, game string, facts map[string]any) (CoachNote, error) {
		seen = facts
		return CoachNote{Verdict: "Lost to the same mistake three times.", Focus: []CoachFocus{{Title: "BKB before the fight"}}, Model: coachModel}, nil
	})
	r, err := a.Coach("dota", 100, false)
	if err != nil || r.Status != "ready" || r.Note.Focus[0].Title != "BKB before the fight" || r.Note.Model != "claude-sonnet-5-5" {
		t.Fatalf("ready: %+v %v", r, err)
	}
	// Kept locally: the model isn't asked again unless forced.
	if r, _ = a.Coach("dota", 100, false); r.Status != "ready" || *calls != 1 {
		t.Fatalf("cached: %+v calls %d", r, *calls)
	}
	if _, _ = a.Coach("dota", 100, true); *calls != 2 {
		t.Fatalf("force: calls %d", *calls)
	}
	// The facts carry the review and nothing personal.
	if seen["hero"] != "Anti-Mage" || seen["result"] != "lost" || seen["deaths"] != 9 {
		t.Fatalf("facts: %+v", seen)
	}
	for k := range seen {
		if strings.Contains(strings.ToLower(k), "account") || strings.Contains(strings.ToLower(k), "steam") {
			t.Fatalf("facts carry %s", k)
		}
	}
	// Off in Settings.
	a.Store.SaveCoach(CoachPrefs{AIOff: true})
	if r, _ = a.Coach("dota", 100, false); r.Status != "off" {
		t.Fatalf("off: %+v", r)
	}
}

func TestCoachSaysWhenThereIsNoSignIn(t *testing.T) {
	a := coachTestApp(t, 850402858)
	stubCoach(t, func(context.Context, string, map[string]any) (CoachNote, error) {
		return CoachNote{}, errClaudeCodeSignIn
	})
	if r, _ := a.Coach("dota", 100, false); r.Status != "not_set_up" {
		t.Fatalf("no sign-in: %+v", r)
	}
	stubCoach(t, func(context.Context, string, map[string]any) (CoachNote, error) { return CoachNote{}, errCoachRefused })
	if r, _ := a.Coach("dota", 100, false); r.Status != "error" || r.Error == "" {
		t.Fatalf("refused: %+v", r)
	}
	stubCoach(t, func(context.Context, string, map[string]any) (CoachNote, error) { return CoachNote{}, errNoClaudeCode })
	if r, _ := a.Coach("dota", 100, false); r.Status != "not_set_up" || !strings.Contains(r.Error, "isn't installed") {
		t.Fatalf("no claude: %+v", r)
	}
}

// What Claude Code prints with --output-format json, in its three shapes.
func TestParseClaudeCodeNote(t *testing.T) {
	note := `{"verdict":"Two deaths alone in their jungle cost the game.","deaths":[{"clock":"12:34","note":"Wait for a ward."}],"focus":[{"title":"Ward before you farm","why":"x","drill":"y"}],"praise":""}`
	structured, _ := json.Marshal(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "", "structured_output": json.RawMessage(note)})
	n, err := parseClaudeCodeNote(structured, nil)
	if err != nil || n.Focus[0].Title != "Ward before you farm" || n.Model != "claude-sonnet-5-5 via Claude Code" {
		t.Fatalf("structured: %+v %v", n, err)
	}
	fenced, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": "```json\n" + note + "\n```"})
	if n, err = parseClaudeCodeNote(fenced, nil); err != nil || n.Deaths[0].Clock != "12:34" {
		t.Fatalf("text: %+v %v", n, err)
	}
	notIn, _ := json.Marshal(map[string]any{"type": "result", "is_error": true, "result": "Not logged in · Please run /login"})
	if _, err = parseClaudeCodeNote(notIn, errors.New("exit status 1")); !errors.Is(err, errClaudeCodeSignIn) {
		t.Fatalf("not logged in: %v", err)
	}
	limited, _ := json.Marshal(map[string]any{"type": "result", "is_error": true, "result": "Claude usage limit reached. Your limit will reset at 11pm."})
	if _, err = parseClaudeCodeNote(limited, errors.New("exit status 1")); err == nil || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("limit: %v", err)
	}
	if _, err = parseClaudeCodeNote([]byte("garbage"), errors.New("exec: not found")); err == nil || !strings.Contains(err.Error(), "didn't run") {
		t.Fatalf("garbage: %v", err)
	}
}

// coachAsk builds the right Claude Code command line and hands the facts
// over on stdin.
func TestCoachAskRunsClaudeCodeHeadless(t *testing.T) {
	old := coachExec
	t.Cleanup(func() { coachExec = old })
	var gotArgs []string
	var gotIn string
	coachExec = func(_ context.Context, bin string, args []string, stdin string) ([]byte, error) {
		gotArgs, gotIn = args, stdin
		return json.Marshal(map[string]any{"is_error": false, "result": `{"verdict":"ok","deaths":[],"focus":[],"praise":""}`})
	}
	if claudeCodePath() == "" {
		t.Skip("no Claude Code on this machine")
	}
	n, err := coachAsk(context.Background(), "deadlock", map[string]any{"hero": "Haze"})
	if err != nil || n.Verdict != "ok" {
		t.Fatalf("%+v %v", n, err)
	}
	line := strings.Join(gotArgs, " ")
	for _, want := range []string{"-p", "--output-format json", "--model claude-sonnet-5-5", "--json-schema", "--no-session-persistence", "--strict-mcp-config"} {
		if !strings.Contains(line, want) {
			t.Errorf("args miss %q: %s", want, line)
		}
	}
	if !strings.Contains(gotIn, "Game: Deadlock") || !strings.Contains(gotIn, `"hero":"Haze"`) {
		t.Errorf("stdin: %q", gotIn)
	}
}

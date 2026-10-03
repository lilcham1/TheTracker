package core

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSteam builds a Steam folder with Dota installed in a second library,
// which is the layout the first version of the config installer missed.
func fakeSteam(t *testing.T, launchOptions string) (root string, cfgDir string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "Steam")
	library := filepath.Join(base, "SteamLibrary")
	cfgDir = filepath.Join(library, "steamapps", "common", "dota 2 beta", "game", "dota", "cfg")
	for _, d := range []string{filepath.Join(root, "steamapps"), cfgDir, filepath.Join(root, "userdata", "850402858", "config")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	vdf := fmt.Sprintf("\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"%s\"\n\t}\n}\n",
		strings.ReplaceAll(root, `\`, `\\`), strings.ReplaceAll(library, `\`, `\\`))
	os.WriteFile(filepath.Join(root, "steamapps", "libraryfolders.vdf"), []byte(vdf), 0o644)

	local := "\"UserLocalConfigStore\"\n{\n\t\"friends\"\n\t{\n\t\t\"570\"\t\t\"1\"\n\t}\n\t\"Software\"\n\t{\n\t\t\"apps\"\n\t\t{\n\t\t\t\"570\"\n\t\t\t{\n\t\t\t\t\"LastPlayed\"\t\t\"1\"\n\t\t\t\t\"LaunchOptions\"\t\t\"" + launchOptions + "\"\n\t\t\t}\n\t\t}\n\t}\n}\n"
	os.WriteFile(filepath.Join(root, "userdata", "850402858", "config", "localconfig.vdf"), []byte(local), 0o644)
	return root, cfgDir
}

func newTestGsi(t *testing.T, launchOptions string) (*Gsi, *Tracker, string) {
	t.Helper()
	store := NewStore(t.TempDir())
	tr := NewTracker(store)
	g := NewGsi(store, tr)
	root, cfgDir := fakeSteam(t, launchOptions)
	g.steamRoot = func() string { return root }
	return g, tr, cfgDir
}

func TestTheConfigNamesThePortAndAsksForWhatTheTrackerReads(t *testing.T) {
	text := gsiConfigText(3002, "secret")
	if !strings.Contains(text, `"http://localhost:3002/"`) {
		t.Fatal("the config must name the port actually bound, or Dota posts into the void")
	}
	if !strings.Contains(text, `"token"         "secret"`) {
		t.Fatal("the token is missing from the config")
	}
	for _, key := range []string{`"map"`, `"player"`, `"hero"`, `"items"`} {
		if !strings.Contains(text, key) {
			t.Errorf("%s must be requested", key)
		}
	}
	if !strings.Contains(text, `"abilities"     "0"`) {
		t.Error("abilities stays off: nothing reads it and it inflates every payload")
	}
}

func TestInstallFindsDotaInASecondLibraryAndReportsIt(t *testing.T) {
	g, _, cfgDir := newTestGsi(t, "-novid -gamestateintegration")

	if s := g.Status(); s.Installed || len(s.CfgDirs) != 1 || s.InstalledAt != nil {
		t.Fatalf("before install: %+v", s)
	}
	written, err := g.Install()
	if err != nil || len(written) != 1 {
		t.Fatalf("install: %v %v", written, err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "gamestate_integration", gsiCfgName)); err != nil {
		t.Fatal("the config was not written into the second library")
	}
	s := g.Status()
	if !s.Installed || s.Stale || s.InstalledAt == nil {
		t.Fatalf("after install: %+v", s)
	}
	if s.LaunchOption == nil || !*s.LaunchOption {
		t.Fatal("the launch option is set in localconfig.vdf and should be detected, even though \"570\" appears earlier in the file")
	}

	if removed := g.Remove(); len(removed) != 1 {
		t.Fatal("remove should delete the config")
	}
	if g.Status().Installed {
		t.Fatal("still reported installed after removal")
	}
}

func TestAMissingLaunchOptionIsReported(t *testing.T) {
	g, _, _ := newTestGsi(t, "-novid")
	if lo := g.Status().LaunchOption; lo == nil || *lo {
		t.Fatal("launch options without the flag should read as not set")
	}
}

func TestAnOlderConfigIsStaleAndGetsRewritten(t *testing.T) {
	g, _, cfgDir := newTestGsi(t, "")
	folder := filepath.Join(cfgDir, "gamestate_integration")
	os.MkdirAll(folder, 0o755)
	// What the previous version wrote: same port, no token.
	old := "\"Dota 2 Integration Configuration\"\n{\n    \"uri\"           \"http://localhost:3000/\"\n}\n"
	os.WriteFile(filepath.Join(folder, gsiCfgName), []byte(old), 0o644)

	if s := g.Status(); s.Installed || !s.Stale {
		t.Fatalf("an out-of-date config should be stale: %+v", s)
	}
	g.EnsureInstalled()
	if s := g.Status(); !s.Installed || s.Stale {
		t.Fatalf("startup should have rewritten it: %+v", s)
	}
	// The setup date is the old file's, not the rewrite's: it is the point
	// from which matches should have been recorded.
	if g.installedAt() == nil {
		t.Fatal("the original setup time was lost")
	}
}

func TestOnlyTheOldTrackersOwnConfigIsRemoved(t *testing.T) {
	ours := "\"Dota 2 Integration Configuration\"\n{\n    \"uri\"           \"http://localhost:3000/\"\n    \"timeout\"       \"5.0\"\n}"
	if !isLegacyConfig(ours) {
		t.Fatal("the old tracker's config was not recognised")
	}
	for _, other := range []string{
		strings.Replace(ours, "localhost:3000", "127.0.0.1:3002", 1),
		strings.Replace(ours, "localhost:3000", "localhost:30001", 1),
		"not a config at all",
	} {
		if isLegacyConfig(other) {
			t.Errorf("someone else's config would be deleted: %q", other)
		}
	}

	g, _, cfgDir := newTestGsi(t, "")
	folder := filepath.Join(cfgDir, "gamestate_integration")
	os.MkdirAll(folder, 0o755)
	os.WriteFile(filepath.Join(folder, legacyCfgName), []byte(ours), 0o644)
	os.WriteFile(filepath.Join(folder, "gamestate_integration_logitech.cfg"), []byte(ours), 0o644)
	if removed := g.RemoveLegacyConfigs(); len(removed) != 1 {
		t.Fatalf("expected exactly the legacy file removed, got %v", removed)
	}
	if _, err := os.Stat(filepath.Join(folder, "gamestate_integration_logitech.cfg")); err != nil {
		t.Fatal("another tool's config was deleted")
	}
}

func post(g *Gsi, body string, headers map[string]string) error {
	r, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/", bytes.NewBufferString(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return g.Accept(r, []byte(body))
}

func TestTheListenerOnlyAcceptsDota(t *testing.T) {
	g, tr, _ := newTestGsi(t, "")
	match := func(id, auth string) string {
		return fmt.Sprintf(`{%s"map":{"matchid":"%s","game_state":"%s","clock_time":300},"player":{"activity":"playing"},"hero":{"name":"npc_dota_hero_axe"}}`, auth, id, stateInProgress)
	}
	current := func() string {
		if c := tr.Status(false).Current; c != nil {
			return c.MatchID
		}
		return ""
	}

	// A web page can make a browser POST to localhost. Browsers always send
	// these headers on such a request; Dota never does.
	if err := post(g, match("1", ""), map[string]string{"Origin": "https://evil.example"}); err == nil || current() != "" {
		t.Fatal("a request from a web page was accepted as a match")
	}
	if err := post(g, match("1", ""), map[string]string{"Sec-Fetch-Site": "cross-site"}); err == nil || current() != "" {
		t.Fatal("a browser fetch was accepted as a match")
	}
	// The wrong token.
	if err := post(g, match("2", `"auth":{"token":"guess"},`), nil); err == nil || current() != "" {
		t.Fatal("a payload with the wrong token was accepted")
	}
	// The right token.
	if err := post(g, match("3", `"auth":{"token":"`+g.token()+`"},`), nil); err != nil || current() != "3" {
		t.Fatalf("Dota's own payload was refused: %v", err)
	}
	// No token at all: a Dota still running from before the config had one.
	// Refusing these would silently stop tracking until Dota restarts.
	if err := post(g, match("4", ""), nil); err != nil || current() != "4" {
		t.Fatalf("a token-less payload from an already-running Dota was refused: %v", err)
	}
	// Garbage is ignored without an error Dota would retry on.
	if err := post(g, "not json", nil); err == nil {
		t.Fatal("unparseable input should be reported to the caller")
	}
}

func TestTheListenerBindsLoopbackAndMovesOffABusyPort(t *testing.T) {
	a, _, _ := newTestGsi(t, "")
	a.Start()
	defer a.Stop()
	if a.ListenerError() != "" {
		t.Skipf("no free port in the range on this machine: %s", a.ListenerError())
	}

	// A second listener finds the first one's port taken and must move on
	// rather than fail, and say that it did.
	b, _, cfgDir := newTestGsi(t, "")
	b.Start()
	defer b.Stop()
	if b.ListenerError() != "" {
		t.Skip("no second free port available")
	}
	if b.Port() == a.Port() {
		t.Fatal("two listeners claim the same port")
	}
	s := b.Status()
	if s.ListenerNotice == "" {
		t.Fatal("moving port needs saying: Dota has to restart to pick it up")
	}
	b.EnsureInstalled()
	raw, _ := os.ReadFile(filepath.Join(cfgDir, "gamestate_integration", gsiCfgName))
	if !strings.Contains(string(raw), fmt.Sprintf("localhost:%d/", b.Port())) {
		t.Fatal("the config must follow the port actually bound")
	}

	resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/", b.Port()), "application/json", strings.NewReader(`{"player":{"activity":"menu"}}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("the listener did not answer on loopback: %v", err)
	}
	if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/anything", b.Port())); err != nil || resp.StatusCode != 404 {
		t.Fatal("only POST / is served")
	}
}

func TestSteamLoginUsersParsing(t *testing.T) {
	sample := "\"users\"\n{\n\t\"76561198810668586\"\n\t{\n\t\t\"AccountName\"\t\t\"lilcham\"\n\t\t\"PersonaName\"\t\t\"lilcham\"\n\t\t\"RememberPassword\"\t\t\"1\"\n\t\t\"MostRecent\"\t\t\"1\"\n\t\t\"Timestamp\"\t\t\"1788560000\"\n\t}\n\t\"76561198000000001\"\n\t{\n\t\t\"AccountName\"\t\t\"someone\"\n\t\t\"PersonaName\"\t\t\"Someone Else\"\n\t\t\"MostRecent\"\t\t\"0\"\n\t}\n}\n"
	accounts := parseLoginUsers(sample)
	if len(accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accounts))
	}
	if accounts[0].AccountID != 850402858 || *accounts[0].Personaname != "lilcham" || accounts[0].Source != "most recent" {
		t.Fatalf("first account wrong: %+v", accounts[0])
	}
	if accounts[1].AccountID != 39734273 || *accounts[1].Personaname != "Someone Else" || accounts[1].Source != "previously signed in" {
		t.Fatalf("second account wrong: %+v", accounts[1])
	}
	if len(parseLoginUsers("\"users\"\n{\n}\n")) != 0 {
		t.Fatal("an empty file has no accounts")
	}
}

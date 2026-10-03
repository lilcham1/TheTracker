package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"thetracker/internal/core"
)

func newServer(t *testing.T) (*httptest.Server, *core.App) {
	app := core.NewApp(t.TempDir(), &core.NoShell{})
	app.SetAPIBases("http://127.0.0.1:1", "http://127.0.0.1:1", "http://127.0.0.1:1")
	assets := fstest.MapFS{"index.html": {Data: []byte("<html>ok</html>")}}
	srv := httptest.NewServer(New(app, assets))
	t.Cleanup(srv.Close)
	return srv, app
}

func call(t *testing.T, srv *httptest.Server, cmd, body string) (int, map[string]any, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/"+cmd, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	return resp.StatusCode, obj, string(raw)
}

// These reach outside the app — they start Dota, write into the real Dota
// install, or open Explorer — so they are not fired blindly from a test.
var outward = map[string]bool{"launch_dota": true, "gsi_install": true, "gsi_remove": true, "quit": true}

func TestEveryCommandAnswersWithJSON(t *testing.T) {
	srv, app := newServer(t)
	for _, cmd := range Commands(app) {
		if outward[cmd] {
			continue
		}
		status, _, raw := call(t, srv, cmd, "{}")
		if status != 200 && status != 400 {
			t.Errorf("%s: HTTP %d", cmd, status)
		}
		if !json.Valid([]byte(raw)) {
			t.Errorf("%s: reply is not JSON: %q", cmd, raw)
		}
		if status == 400 {
			var e map[string]string
			if json.Unmarshal([]byte(raw), &e) != nil || e["error"] == "" {
				t.Errorf("%s: a failure must carry a message for the person using the app, got %q", cmd, raw)
			}
			if strings.Contains(e["error"], "127.0.0.1") || strings.Contains(e["error"], "dial tcp") {
				t.Errorf("%s: a raw network error reached the UI: %q", cmd, e["error"])
			}
		}
	}
}

func TestBootHasEverythingTheFirstFrameNeeds(t *testing.T) {
	srv, _ := newServer(t)
	status, boot, raw := call(t, srv, "boot", "")
	if status != 200 {
		t.Fatalf("boot failed: %s", raw)
	}
	for _, key := range []string{"version", "prefs", "profile", "dotaLink", "deadlockLink", "auth", "background", "gsi", "overlayVisible", "dataDir", "exportDir"} {
		if _, ok := boot[key]; !ok {
			t.Errorf("boot is missing %s", key)
		}
	}
	prefs := boot["prefs"].(map[string]any)
	if prefs["builds"] == nil || prefs["overlay"].(map[string]any)["dota"] == nil {
		t.Errorf("prefs must never carry null where the UI expects a list or object: %v", prefs)
	}
}

func TestCommandsRoundTrip(t *testing.T) {
	srv, app := newServer(t)

	if status, _, raw := call(t, srv, "save_build", `{"game":"dota","hero":"axe","name":""}`); status != 400 || !strings.Contains(raw, "hero and a name") {
		t.Fatalf("a nameless build should be refused: %d %s", status, raw)
	}
	_, prefs, _ := call(t, srv, "save_build", `{"game":"dota","hero":"axe","name":"Blink","items":["blink"]}`)
	if len(prefs["builds"].([]any)) != 1 {
		t.Fatal("build not saved")
	}

	if status, _, _ := call(t, srv, "dota_link", `{"accountId":0,"personaname":"x"}`); status != 400 {
		t.Fatal("linking account 0 should be refused")
	}
	call(t, srv, "dota_link", `{"accountId":42,"personaname":"me","avatar":null}`)
	if id := app.Store.LoadLink("dota").AccountID; id == nil || *id != 42 {
		t.Fatal("link not stored")
	}
	if app.Store.LoadLink("deadlock").AccountID != nil {
		t.Fatal("linking Dota must not link Deadlock")
	}
	call(t, srv, "dota_unlink", "")
	if app.Store.LoadLink("dota").AccountID != nil {
		t.Fatal("unlink failed")
	}

	if status, _, raw := call(t, srv, "save_profile", `{"username":"`+strings.Repeat("x", 41)+`"}`); status != 400 {
		t.Fatalf("an over-long name should be refused here, not by the server later: %s", raw)
	}
	if status, _, _ := call(t, srv, "restore", `{"path":"C:\Windows\system.ini"}`); status != 400 {
		t.Fatal("restore accepted a path outside the backup folder")
	}
	if status, _, _ := call(t, srv, "reveal", `{"path":"C:\Windows\System32\cmd.exe"}`); status != 400 {
		t.Fatal("reveal accepted a path that is not one of the app's files")
	}
	if status, _, _ := call(t, srv, "open_url", `{"url":"https://evil.example/"}`); status != 400 {
		t.Fatal("open_url opened a site that is not on the list")
	}
	if status, _, _ := call(t, srv, "open_url", `{"url":"file:///C:/Windows/System32/calc.exe"}`); status != 400 {
		t.Fatal("open_url accepted a file URL")
	}
	if status, _, raw := call(t, srv, "save_goals", `not json`); status != 400 || !strings.Contains(raw, "error") {
		t.Fatal("malformed input should be a clean 400")
	}

	if status, _, _ := call(t, srv, "sim_start", `{"seconds":10}`); status != 200 {
		t.Fatal("simulator did not start")
	}
	_, live, _ := call(t, srv, "get_live", `{"log":true}`)
	if live["simulating"] != true || live["current"] == nil {
		t.Fatalf("live view during a simulation: %v", live)
	}
	call(t, srv, "sim_stop", "")
	if _, live, _ := call(t, srv, "get_live", ""); live["simulating"] != false {
		t.Fatal("simulator did not stop")
	}
}

func TestOnlyPostReachesCommandsAndPagesAreLockedDown(t *testing.T) {
	srv, _ := newServer(t)
	resp, _ := http.Get(srv.URL + "/api/get_history")
	if resp.StatusCode != 404 {
		t.Fatalf("GET on a command should not run it, got %d", resp.StatusCode)
	}
	if status, _, _ := call(t, srv, "no_such_command", ""); status != 404 {
		t.Fatal("an unknown command should be a 404")
	}

	page, _ := http.Get(srv.URL + "/")
	body, _ := io.ReadAll(page.Body)
	if page.StatusCode != 200 || !strings.Contains(string(body), "ok") {
		t.Fatal("the UI is not served at /")
	}
	csp := page.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "connect-src 'self'") || strings.Contains(csp, "unsafe-eval") {
		t.Fatalf("the page policy must keep scripts and connections to the app itself: %q", csp)
	}
}

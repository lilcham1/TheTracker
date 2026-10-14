package core

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"unicode/utf16"
)

type fakeTasks struct {
	exists     map[string]bool
	on, off    string
	runs       []string
	installErr error
	sid        string
}

func (f *fakeTasks) Exists(task string) bool { return f.exists[task] }
func (f *fakeTasks) Install(on, off string) error {
	if f.installErr != nil {
		return f.installErr
	}
	for _, t := range gameModeLegacyTasks {
		delete(f.exists, t)
	}
	f.exists[gameModeOnTask], f.exists[gameModeOffTask] = true, true
	f.on, f.off = on, off
	return nil
}
func (f *fakeTasks) Remove() error {
	f.exists = map[string]bool{}
	return nil
}
func (f *fakeTasks) Run(task string) error    { f.runs = append(f.runs, task); return nil }
func (f *fakeTasks) UserSID() (string, error) { return f.sid, nil }

// decodeTask pulls the script back out of a task definition.
func decodeTask(t *testing.T, xml string) string {
	i := strings.Index(xml, "-EncodedCommand ")
	j := strings.Index(xml, "</Arguments>")
	raw, err := base64.StdEncoding.DecodeString(xml[i+len("-EncodedCommand ") : j])
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("encoded command: %v", err)
	}
	u := make([]uint16, len(raw)/2)
	for k := range u {
		u[k] = uint16(raw[2*k]) | uint16(raw[2*k+1])<<8
	}
	return string(utf16.Decode(u))
}

func TestGameModeScripts(t *testing.T) {
	on, off, slowed := gameModeScripts([]string{"sync", "browsers", "made-up"})
	for _, want := range []string{"'OneDrive.exe'", "-ThrottleRateActionBitsPerSecond 1000000", "'chrome.exe'", "-ThrottleRateActionBitsPerSecond 3000000",
		"'dota2.exe' -DSCPAction 46", "-PolicyStore ActiveStore"} {
		if !strings.Contains(on, want) {
			t.Errorf("on script lacks %s", want)
		}
	}
	if slowed != len(gameModeGroups["sync"].Apps)+len(gameModeGroups["browsers"].Apps) {
		t.Errorf("slowed count: %d", slowed)
	}
	for _, unwanted := range []string{"steam.exe", "DoSvc -Force", "Discord", "made-up", "Set-Content", "Out-File"} {
		if strings.Contains(on+off, unwanted) {
			t.Errorf("scripts must not have %s", unwanted)
		}
	}
	// Every policy goes in the active store, which Windows empties at restart.
	if strings.Count(on, "New-NetQosPolicy") != strings.Count(on, "-PolicyStore ActiveStore | Out-Null") {
		t.Error("a policy outside the active store")
	}
	if !strings.Contains(off, "Remove-NetQosPolicy -PolicyStore ActiveStore") || strings.Contains(off, "New-NetQosPolicy") {
		t.Errorf("off only removes: %s", off)
	}
	on, _, _ = gameModeScripts([]string{"windowsUpdate", "launchers"})
	if !strings.Contains(on, "Stop-Service -Name DoSvc -Force") || !strings.Contains(on, "'steamwebhelper.exe'") {
		t.Errorf("update sharing and launchers: %s", on)
	}
	if psQuote("it's") != "'it''s'" {
		t.Error("quotes")
	}
}

func TestGameModeTaskXML(t *testing.T) {
	x := gameModeTaskXML("desc", "Write-Output 1", "S-1-5-21-1-2-3-1001")
	for _, want := range []string{"<UserId>S-1-5-18</UserId>", "(A;;FRFX;;;S-1-5-21-1-2-3-1001)", "<Hidden>true</Hidden>", "powershell.exe"} {
		if !strings.Contains(x, want) {
			t.Errorf("task lacks %s", want)
		}
	}
	if decodeTask(t, x) != "Write-Output 1" {
		t.Error("the script round-trips")
	}
}

func TestGameModeLifecycle(t *testing.T) {
	a := newTestApp(t)
	f := &fakeTasks{sid: "S-1-5-21-9", exists: map[string]bool{}}
	g := a.GameMode
	g.tasks = f

	// Before setup, matches do nothing.
	g.On()
	if len(f.runs) != 0 || g.Status().State != "not_set_up" {
		t.Fatalf("not set up: %+v %+v", f.runs, g.Status())
	}
	if err := g.Setup(nil); err == nil {
		t.Fatal("setup needs a category")
	}
	f.installErr = errors.New("Windows' approval was declined, so nothing was changed.")
	if err := g.Setup([]string{"sync"}); err == nil || a.Store.LoadPrefs().Network.GameMode {
		t.Fatal("a declined prompt changes nothing")
	}
	f.installErr = nil
	if err := g.Setup([]string{"sync", "windowsUpdate"}); err != nil {
		t.Fatal(err)
	}
	p := a.Store.LoadPrefs().Network
	if !p.GameMode || len(p.Categories) != 2 {
		t.Fatalf("prefs: %+v", p)
	}
	if s := decodeTask(t, f.on); !strings.Contains(s, "OneDrive.exe") || !strings.Contains(s, "DoSvc") || strings.Contains(s, "chrome.exe") {
		t.Fatalf("installed on-script: %s", s)
	}
	if g.Status().State != "ready" {
		t.Fatalf("ready: %+v", g.Status())
	}

	g.On()
	if s := g.Status(); s.State != "on" || s.Slowed != len(gameModeGroups["sync"].Apps) || !s.UpdatePaused {
		t.Fatalf("on: %+v", s)
	}
	g.Off()
	if s := g.Status(); s.State != "ready" {
		t.Fatalf("off again: %+v", s)
	}
	if strings.Join(f.runs, ",") != gameModeOnTask+","+gameModeOffTask {
		t.Fatalf("runs: %v", f.runs)
	}

	if err := g.Remove(); err != nil || f.exists[gameModeOnTask] || a.Store.LoadPrefs().Network.GameMode {
		t.Fatalf("removed: %v %+v", err, f)
	}
	if f.runs[len(f.runs)-1] != gameModeOffTask {
		t.Fatal("removing turns it off first")
	}
}

func TestGameModeOlderTasks(t *testing.T) {
	a := newTestApp(t)
	a.Store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode = true })

	// 1.9.0's tasks: never run, the player is asked to update.
	f := &fakeTasks{sid: "S-1-5-21-9", exists: map[string]bool{gameModeUnsafeTasks[0]: true, gameModeUnsafeTasks[1]: true}}
	g := a.GameMode
	g.tasks = f
	if s := g.Status(); s.State != "needs_update" {
		t.Fatalf("1.9.0 tasks need the one-time update: %+v", s)
	}
	g.On()
	if len(f.runs) != 0 {
		t.Fatal("1.9.0's tasks are never run")
	}

	// 1.9.1–1.9.4's tasks keep working (Dota only) until set up again.
	f.exists = map[string]bool{gameModePrevOn: true, gameModePrevOff: true}
	g.forgetTaskState()
	if s := g.Status(); s.State != "ready" || !s.UpdateAvailable {
		t.Fatalf("previous tasks still work, with an update offered: %+v", s)
	}
	g.On()
	g.Off()
	if strings.Join(f.runs, ",") != gameModePrevOn+","+gameModePrevOff {
		t.Fatalf("previous tasks run: %v", f.runs)
	}

	// Setting up again replaces both kinds, and every game is prioritised.
	if err := g.Setup([]string{"sync"}); err != nil {
		t.Fatal(err)
	}
	for _, old := range gameModeLegacyTasks {
		if f.exists[old] {
			t.Fatalf("old task %s left behind", old)
		}
	}
	if s := g.Status(); s.State != "ready" || s.UpdateAvailable {
		t.Fatalf("current tasks: %+v", s)
	}
	script := decodeTask(t, f.on)
	for _, game := range gamePriorityApps {
		if !strings.Contains(script, "'"+game+"' -DSCPAction 46") {
			t.Errorf("%s not prioritised", game)
		}
	}
}

// A setup from before PUBG was added keeps working and offers an update
// naming the game it leaves out.
func TestGameModeNewGameOffersUpdate(t *testing.T) {
	a := newTestApp(t)
	a.Store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode, p.Network.Categories = true, []string{"sync"} })
	f := &fakeTasks{sid: "S-1-5-21-9", exists: map[string]bool{gameModeOnTask: true, gameModeOffTask: true}}
	g := a.GameMode
	g.tasks = f
	s := g.Status()
	if s.State != "ready" || !s.UpdateAvailable || !strings.Contains(s.UpdateReason, "PUBG") || strings.Contains(s.UpdateReason, "Dota") {
		t.Fatalf("older setup: %+v", s)
	}
	if err := g.Setup([]string{"sync"}); err != nil {
		t.Fatal(err)
	}
	if s := g.Status(); s.UpdateAvailable || s.UpdateReason != "" {
		t.Fatalf("after update: %+v", s)
	}
}

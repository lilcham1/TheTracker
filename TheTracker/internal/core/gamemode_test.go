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

func TestGameModeLegacyTasksNeedUpdate(t *testing.T) {
	a := newTestApp(t)
	// 1.9.0's tasks are registered; the current ones aren't yet.
	f := &fakeTasks{sid: "S-1-5-21-9", exists: map[string]bool{gameModeLegacyTasks[0]: true, gameModeLegacyTasks[1]: true}}
	g := a.GameMode
	g.tasks = f
	a.Store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode = true })

	if s := g.Status(); s.State != "needs_update" {
		t.Fatalf("old tasks need the one-time update: %+v", s)
	}
	g.On()
	if len(f.runs) != 0 {
		t.Fatal("the old tasks are never run")
	}
	if err := g.Setup([]string{"sync"}); err != nil {
		t.Fatal(err)
	}
	if f.exists[gameModeLegacyTasks[0]] || g.Status().State != "ready" {
		t.Fatalf("setting up again replaces them: %+v", f.exists)
	}
}

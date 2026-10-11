package core

// Game mode: while a Dota match runs, slow other programs' uploads and mark
// Dota's packets as priority, then undo it when the match ends.
//
// Windows only lets an administrator set network policies, so the player
// approves it once: TheTracker registers two scheduled tasks that run as
// the system account (no window, nothing over the game) and that the
// player's account may start but not change. The tasks' scripts are written
// into the tasks themselves, which only an administrator can change, and
// they only ever name the programs below: they can't be made to run
// anything else. The policies go in Windows' active store, which is emptied
// at every restart, so nothing can stay behind.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf16"
)

// The programs slowed per category, and their upload limit in bits per
// second. Discord and other voice apps are never on this list.
type gameModeGroup struct {
	Label string
	Rate  int
	Apps  []string
}

var gameModeGroups = map[string]gameModeGroup{
	"sync": {"Cloud sync", 1_000_000, []string{"OneDrive.exe", "OneDrive.Sync.Service.exe", "GoogleDriveFS.exe", "Dropbox.exe",
		"iCloudDrive.exe", "iCloudServices.exe", "iCloudPhotos.exe"}},
	"launchers": {"Game launchers", 1_000_000, []string{"steam.exe", "steamwebhelper.exe", "EpicGamesLauncher.exe", "EpicWebHelper.exe",
		"Battle.net.exe", "EADesktop.exe", "EABackgroundService.exe", "UbisoftConnect.exe", "upc.exe"}},
	// Browsers keep enough for a video call.
	"browsers": {"Browsers", 3_000_000, []string{"chrome.exe", "msedge.exe", "firefox.exe", "opera.exe", "brave.exe"}},
	// Windows Update's sharing with other PCs: its service is stopped for the
	// match (Windows starts it again when it next needs it).
	"windowsUpdate": {"Windows Update sharing", 0, nil},
}

var gameModeOrder = []string{"sync", "launchers", "windowsUpdate", "browsers"}

const (
	gameModeOnTask     = `\TheTracker\GameModeOn`
	gameModeOffTask    = `\TheTracker\GameModeOff`
	gameModeStatusFile = "game_mode.json"
)

func validGameModeCategories(cats []string) []string {
	out := []string{}
	for _, c := range gameModeOrder {
		for _, want := range cats {
			if want == c {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// psQuote quotes a string for PowerShell. Only catalogue names and the data
// folder (checked for quotes first) ever reach it.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

const gameModeRemoveAll = `Get-NetQosPolicy -PolicyStore ActiveStore | Where-Object { $_.Name -like 'TheTracker*' } | Remove-NetQosPolicy -PolicyStore ActiveStore -Confirm:$false`

// The task writes what it did where the app can read it: reading network
// policies back needs admin rights the app doesn't have.
func gameModeStatusLine(dir, state string) string {
	return `$c = @(Get-NetQosPolicy -PolicyStore ActiveStore | Where-Object { $_.Name -like 'TheTracker*' }).Count
$d = [string](Get-Service -Name DoSvc).Status
Set-Content -LiteralPath ` + psQuote(dir+`\`+gameModeStatusFile) + ` -Encoding ascii -Value ('{"state":"` + state + `","policies":' + $c + ',"doSvc":"' + $d + '","at":"' + (Get-Date).ToUniversalTime().ToString('o') + '"}')`
}

// gameModeScripts builds the On and Off scripts for the chosen categories.
func gameModeScripts(cats []string, dataDir string) (on, off string) {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'SilentlyContinue'\n")
	b.WriteString(gameModeRemoveAll + "\n")
	n := 0
	stopUpdate := false
	for _, c := range validGameModeCategories(cats) {
		if c == "windowsUpdate" {
			stopUpdate = true
			continue
		}
		g := gameModeGroups[c]
		for _, app := range g.Apps {
			n++
			fmt.Fprintf(&b, "New-NetQosPolicy -Name 'TheTracker %d' -AppPathNameMatchCondition %s -ThrottleRateActionBitsPerSecond %d -PolicyStore ActiveStore | Out-Null\n", n, psQuote(app), g.Rate)
		}
	}
	b.WriteString("New-NetQosPolicy -Name 'TheTracker Dota' -AppPathNameMatchCondition 'dota2.exe' -DSCPAction 46 -PolicyStore ActiveStore | Out-Null\n")
	if stopUpdate {
		b.WriteString("Stop-Service -Name DoSvc -Force\n")
	}
	b.WriteString(gameModeStatusLine(dataDir, "on") + "\n")
	off = "$ErrorActionPreference = 'SilentlyContinue'\n" + gameModeRemoveAll + "\n" + gameModeStatusLine(dataDir, "off") + "\n"
	return b.String(), off
}

// encodePS is PowerShell's -EncodedCommand form: base64 of UTF-16LE.
func encodePS(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// gameModeTaskXML is a task that runs a script as the system account, which
// the player's own account (sid) may read and start but not change.
func gameModeTaskXML(desc, script, sid string) string {
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Author>TheTracker</Author>
    <Description>` + desc + `</Description>
    <SecurityDescriptor>D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FRFX;;;` + sid + `)</SecurityDescriptor>
  </RegistrationInfo>
  <Principals>
    <Principal id="Author">
      <UserId>S-1-5-18</UserId>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>Queue</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <Hidden>true</Hidden>
    <ExecutionTimeLimit>PT2M</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>powershell.exe</Command>
      <Arguments>-NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand ` + encodePS(script) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// gameModeTasks is the Windows side: registering the tasks (with the
// player's approval) and starting them.
type gameModeTasks interface {
	Installed() bool
	Install(onXML, offXML string) error
	Remove() error
	Run(task string) error
	UserSID() (string, error)
}

type GameModeStatus struct {
	// not_set_up | ready | on
	State        string   `json:"state"`
	Categories   []string `json:"categories"`
	Policies     int      `json:"policies"`
	UpdatePaused bool     `json:"updatePaused"`
	At           string   `json:"at,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type GameMode struct {
	tasks gameModeTasks
	store *Store

	mu      sync.Mutex
	lastErr string
}

func NewGameMode(store *Store) *GameMode {
	return &GameMode{tasks: windowsTasks{}, store: store}
}

func (g *GameMode) prefs() NetworkPrefs { return g.store.LoadPrefs().Network }

// Setup registers the tasks for the chosen categories. Windows asks the
// player for approval.
func (g *GameMode) Setup(cats []string) error {
	cats = validGameModeCategories(cats)
	if len(cats) == 0 {
		return errors.New("Pick at least one kind of program to slow down.")
	}
	dir := g.store.Dir
	if strings.ContainsAny(dir, `'"<>&`) {
		return errors.New("TheTracker's data folder has a character Game mode can't use.")
	}
	sid, err := g.tasks.UserSID()
	if err != nil {
		return err
	}
	on, off := gameModeScripts(cats, dir)
	if err := g.tasks.Install(gameModeTaskXML("TheTracker Game mode: slows background uploads during a Dota match.", on, sid),
		gameModeTaskXML("TheTracker Game mode: undoes it after the match.", off, sid)); err != nil {
		return err
	}
	g.store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode, p.Network.Categories = true, cats })
	return nil
}

// Remove undoes everything: the policies, then the tasks.
func (g *GameMode) Remove() error {
	_ = g.Off()
	if g.tasks.Installed() {
		if err := g.tasks.Remove(); err != nil {
			return err
		}
	}
	g.store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode = false })
	return nil
}

func (g *GameMode) run(task string) error {
	if !g.prefs().GameMode || !g.tasks.Installed() {
		return nil
	}
	err := g.tasks.Run(task)
	g.mu.Lock()
	g.lastErr = ""
	if err != nil {
		g.lastErr = err.Error()
	}
	g.mu.Unlock()
	return err
}

func (g *GameMode) On() error  { return g.run(gameModeOnTask) }
func (g *GameMode) Off() error { return g.run(gameModeOffTask) }

func (g *GameMode) Status() GameModeStatus {
	p := g.prefs()
	s := GameModeStatus{State: "not_set_up", Categories: p.Categories}
	if s.Categories == nil {
		s.Categories = []string{}
	}
	if !p.GameMode || !g.tasks.Installed() {
		return s
	}
	s.State = "ready"
	var file struct {
		State    string `json:"state"`
		Policies int    `json:"policies"`
		DoSvc    string `json:"doSvc"`
		At       string `json:"at"`
	}
	if g.store.readJSON(gameModeStatusFile, &file) {
		s.Policies, s.At = file.Policies, file.At
		if file.State == "on" && file.Policies > 0 {
			s.State = "on"
			s.UpdatePaused = strings.EqualFold(file.DoSvc, "Stopped")
		}
	}
	g.mu.Lock()
	s.Error = g.lastErr
	g.mu.Unlock()
	return s
}

// GameModeCategory is what Settings shows: each category and its programs.
type GameModeCategory struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Mbps  float64  `json:"mbps"`
	Apps  []string `json:"apps"`
}

func GameModeCatalogue() []GameModeCategory {
	out := []GameModeCategory{}
	for _, id := range gameModeOrder {
		g := gameModeGroups[id]
		apps := make([]string, 0, len(g.Apps))
		for _, a := range g.Apps {
			apps = append(apps, strings.TrimSuffix(a, ".exe"))
		}
		sort.Strings(apps)
		out = append(out, GameModeCategory{ID: id, Label: g.Label, Mbps: float64(g.Rate) / 1e6, Apps: apps})
	}
	return out
}

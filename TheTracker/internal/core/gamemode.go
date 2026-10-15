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
//
// The tasks write nothing to disk: a system task writing into a folder the
// player's account controls could be pointed somewhere else, so the app
// keeps track of what it switched itself.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
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
	gameModeOnTask  = `\TheTracker\GameMode2\On`
	gameModeOffTask = `\TheTracker\GameMode2\Off`
)

// The tasks 1.9.1–1.9.4 registered: safe, but they mark only Dota's traffic
// as priority. They keep working until Game mode is set up again.
var gameModePrevOn, gameModePrevOff = `\TheTracker\GameMode\On`, `\TheTracker\GameMode\Off`

// The tasks 1.9.0 registered, whose scripts wrote a status file into the
// data folder. Never run; setting Game mode up again deletes them.
var gameModeUnsafeTasks = []string{`\TheTracker\GameModeOn`, `\TheTracker\GameModeOff`}

// gameModeLegacyTasks is every older task, deleted once the current ones
// exist.
var gameModeLegacyTasks = append([]string{gameModePrevOn, gameModePrevOff}, gameModeUnsafeTasks...)

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

// psQuote quotes a string for PowerShell. Only catalogue names reach it.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

const gameModeRemoveAll = `Get-NetQosPolicy -PolicyStore ActiveStore | Where-Object { $_.Name -like 'TheTracker*' } | Remove-NetQosPolicy -PolicyStore ActiveStore -Confirm:$false`

// gameModeScripts builds the On and Off scripts for the chosen categories,
// and says how many programs On slows.
func gameModeScripts(cats []string) (on, off string, slowed int) {
	var b strings.Builder
	b.WriteString("$ErrorActionPreference = 'SilentlyContinue'\n")
	b.WriteString(gameModeRemoveAll + "\n")
	stopUpdate := false
	for _, c := range validGameModeCategories(cats) {
		if c == "windowsUpdate" {
			stopUpdate = true
			continue
		}
		g := gameModeGroups[c]
		for _, app := range g.Apps {
			slowed++
			fmt.Fprintf(&b, "New-NetQosPolicy -Name 'TheTracker %d' -AppPathNameMatchCondition %s -ThrottleRateActionBitsPerSecond %d -PolicyStore ActiveStore | Out-Null\n", slowed, psQuote(app), g.Rate)
		}
	}
	// Every game TheTracker knows is marked as priority traffic.
	for i, game := range gamePriorityApps {
		fmt.Fprintf(&b, "New-NetQosPolicy -Name 'TheTracker game %d' -AppPathNameMatchCondition %s -DSCPAction 46 -PolicyStore ActiveStore | Out-Null\n", i+1, psQuote(game))
	}
	if stopUpdate {
		b.WriteString("Stop-Service -Name DoSvc -Force\n")
	}
	off = "$ErrorActionPreference = 'SilentlyContinue'\n" + gameModeRemoveAll + "\n"
	return b.String(), off, slowed
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
	// Exists says whether a task is registered.
	Exists(task string) bool
	// Install registers both tasks and deletes the legacy ones.
	Install(onXML, offXML string) error
	// Remove deletes every Game mode task, legacy ones included.
	Remove() error
	// Run starts a task and waits for it to finish.
	Run(task string) error
	UserSID() (string, error)
}

type GameModeStatus struct {
	// not_set_up | needs_update | ready | on
	State      string   `json:"state"`
	Categories []string `json:"categories"`
	// Working, but set up by an earlier version that marks only Dota's
	// traffic as priority: setting up again adds the other games.
	UpdateAvailable bool `json:"updateAvailable"`
	// Why, in a sentence, when it isn't the older task format.
	UpdateReason string `json:"updateReason,omitempty"`
	Slowed       int    `json:"slowed"`
	UpdatePaused bool   `json:"updatePaused"`
	Error        string `json:"error,omitempty"`
}

// gameModeTaskSet is which registered tasks there are.
type gameModeTaskSet struct {
	current, previous, unsafe bool
}

// usable is the On and Off tasks to run: the current ones, else the
// previous version's (safe, Dota only). 1.9.0's are never run.
func (t gameModeTaskSet) usable() (on, off string, ok bool) {
	switch {
	case t.current:
		return gameModeOnTask, gameModeOffTask, true
	case t.previous:
		return gameModePrevOn, gameModePrevOff, true
	}
	return "", "", false
}

type GameMode struct {
	tasks gameModeTasks
	store *Store

	// One switch at a time: On and Off are separate tasks, and an Off still
	// running when On starts would remove what On just set.
	runMu sync.Mutex

	mu      sync.Mutex
	on      bool
	lastErr string
	// Which tasks exist, asked of Windows at most once a minute.
	set   gameModeTaskSet
	setAt time.Time
}

func NewGameMode(store *Store) *GameMode {
	return &GameMode{tasks: windowsTasks{}, store: store}
}

func (g *GameMode) prefs() NetworkPrefs { return g.store.LoadPrefs().Network }

func (g *GameMode) taskState() gameModeTaskSet {
	g.mu.Lock()
	if !g.setAt.IsZero() && time.Since(g.setAt) < time.Minute {
		set := g.set
		g.mu.Unlock()
		return set
	}
	g.mu.Unlock()
	var set gameModeTaskSet
	set.current = g.tasks.Exists(gameModeOnTask) && g.tasks.Exists(gameModeOffTask)
	if !set.current {
		set.previous = g.tasks.Exists(gameModePrevOn) && g.tasks.Exists(gameModePrevOff)
	}
	for _, t := range gameModeUnsafeTasks {
		set.unsafe = set.unsafe || g.tasks.Exists(t)
	}
	g.mu.Lock()
	g.set, g.setAt = set, time.Now()
	g.mu.Unlock()
	return set
}

func (g *GameMode) forgetTaskState() {
	g.mu.Lock()
	g.setAt = time.Time{}
	g.mu.Unlock()
}

// Setup registers the tasks for the chosen categories. Windows asks the
// player for approval.
func (g *GameMode) Setup(cats []string) error {
	cats = validGameModeCategories(cats)
	if len(cats) == 0 {
		return errors.New("Pick at least one kind of program to slow down.")
	}
	sid, err := g.tasks.UserSID()
	if err != nil {
		return err
	}
	on, off, _ := gameModeScripts(cats)
	defer g.forgetTaskState()
	if err := g.tasks.Install(gameModeTaskXML("TheTracker Game mode: slows background uploads while you play.", on, sid),
		gameModeTaskXML("TheTracker Game mode: undoes it when you stop.", off, sid)); err != nil {
		return err
	}
	g.store.UpdatePrefs(func(p *Prefs) {
		p.Network.GameMode, p.Network.Categories = true, cats
		p.Network.Apps = append([]string{}, gamePriorityApps...)
	})
	return nil
}

// Remove undoes everything: the policies, then the tasks.
func (g *GameMode) Remove() error {
	_ = g.Off()
	defer g.forgetTaskState()
	if set := g.taskState(); set.current || set.previous || set.unsafe {
		if err := g.tasks.Remove(); err != nil {
			return err
		}
		g.forgetTaskState()
		if set := g.taskState(); set.current || set.previous || set.unsafe {
			return errors.New("Windows didn't remove Game mode's tasks. Try again.")
		}
	}
	g.store.UpdatePrefs(func(p *Prefs) { p.Network.GameMode = false })
	g.mu.Lock()
	g.on = false
	g.mu.Unlock()
	return nil
}

func (g *GameMode) run(on bool) error {
	if !g.prefs().GameMode {
		return nil
	}
	onTask, offTask, ok := g.taskState().usable()
	if !ok {
		return nil
	}
	task := offTask
	if on {
		task = onTask
	}
	g.runMu.Lock()
	err := g.tasks.Run(task)
	g.runMu.Unlock()
	g.mu.Lock()
	g.lastErr = ""
	if err != nil {
		g.lastErr = err.Error()
	} else {
		g.on = on
	}
	g.mu.Unlock()
	return err
}

func (g *GameMode) On() error  { return g.run(true) }
func (g *GameMode) Off() error { return g.run(false) }

func (g *GameMode) Status() GameModeStatus {
	p := g.prefs()
	s := GameModeStatus{State: "not_set_up", Categories: validGameModeCategories(p.Categories)}
	set := g.taskState()
	_, _, usable := set.usable()
	switch {
	case p.GameMode && usable:
		s.State = "ready"
		s.UpdateAvailable = !set.current
	case p.GameMode && set.unsafe:
		s.State = "needs_update"
	}
	// Set up before a game was added: its traffic isn't marked yet.
	if (s.State == "ready") && !s.UpdateAvailable {
		if missing := missingPriorityGames(p.Apps); len(missing) > 0 {
			s.UpdateAvailable = true
			s.UpdateReason = "Press Update to also prioritise " + strings.Join(missing, ", ") + "; Windows asks once."
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	s.Error = g.lastErr
	if s.State == "ready" && g.on {
		s.State = "on"
		_, _, s.Slowed = gameModeScripts(s.Categories)
		for _, c := range s.Categories {
			s.UpdatePaused = s.UpdatePaused || c == "windowsUpdate"
		}
	}
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

var priorityGameNames = map[string]string{"dota2.exe": "Dota 2", "project8.exe": "Deadlock", "cs2.exe": "CS2", "Overwatch.exe": "Overwatch", "TslGame.exe": "PUBG", "cod.exe": "Call of Duty"}

// missingPriorityGames names the games a setup made with apps doesn't
// prioritise. A setup from before the list was saved counts as having the
// four games Game mode first covered.
func missingPriorityGames(apps []string) []string {
	if len(apps) == 0 {
		apps = []string{"dota2.exe", "project8.exe", "cs2.exe", "Overwatch.exe"}
	}
	have := map[string]bool{}
	for _, a := range apps {
		have[strings.ToLower(a)] = true
	}
	var out []string
	for _, a := range gamePriorityApps {
		if !have[strings.ToLower(a)] {
			out = append(out, priorityGameNames[a])
		}
	}
	return out
}

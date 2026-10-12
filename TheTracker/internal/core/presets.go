package core

// Pro settings presets: competitive video, audio, gameplay, visibility and
// hotkey settings for Dota 2, Deadlock and CS2, applied to the games' own
// settings files under Steam's userdata folder. Every write is preceded by
// a backup that one click restores, and nothing is written while the game
// runs (it rewrites its files on exit and reads them on launch).
//
// Each line is tagged "seen" when its name already appears in the player's
// file, "known" when it is a standard setting the file doesn't hold yet.
// The games' own schema files are empty, so there is no list to check
// names against; an unknown name is ignored by the game, never harmful.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PresetLine struct {
	ID    string   `json:"id"`
	Group string   `json:"group"` // Video | Audio | Gameplay | Visibility | Hotkeys
	Label string   `json:"label"`
	File  string   `json:"file"` // video | convars | cloudconvars | keys
	Path  []string `json:"-"`
	Value string   `json:"value"`
	Note  string   `json:"note,omitempty"`
	// Where the value comes from: a pro, a guide, or "my pick".
	Source string `json:"source"`
	// Hotkeys: the in-game action and the section of the layout it sits
	// in, so the page shows one row per action.
	Action  string `json:"action,omitempty"`
	Section string `json:"section,omitempty"`
	// Remove the key instead of setting it (a modifier the layout drops).
	Remove bool `json:"remove,omitempty"`
	// Shown but never written: a setting only the game's own menu takes.
	Info bool `json:"info,omitempty"`
	// Filled by the preview.
	Current string `json:"current"`
	Present bool   `json:"present"`
	Changed bool   `json:"changed"`
}

type PresetFile struct {
	Kind  string `json:"kind"`
	Path  string `json:"path"`
	Cloud bool   `json:"cloud"`
	Found bool   `json:"found"`
}

type PresetPreview struct {
	Game      string       `json:"game"`
	Installed bool         `json:"installed"`
	Running   bool         `json:"running"`
	CanApply  bool         `json:"canApply"`
	Reasons   []string     `json:"reasons"`
	Files     []PresetFile `json:"files"`
	Lines     []PresetLine `json:"lines"`
	// The hotkey layouts on offer and the one these lines use.
	Layouts []PresetLayout `json:"layouts"`
	Layout  string         `json:"layout"`
	// How many lines would change.
	Changes    int    `json:"changes"`
	LastBackup string `json:"lastBackup,omitempty"`
	// The oldest backup that holds the key file: the keys from before the
	// first preset, if any apply has touched them.
	OriginalKeys string `json:"originalKeys,omitempty"`
}

// steamAccount is the signed-in Steam account; a test swaps it.
var steamAccount = activeSteamAccount

var presetGames = map[string]struct {
	appID   string
	dir     string // under steamapps/common
	process string // exe name without .exe
	label   string
}{
	"dota":     {"570", "dota 2 beta/game/dota", "dota2", "Dota 2"},
	"deadlock": {"1422450", "Deadlock/game/citadel", "project8", "Deadlock"},
	"cs2":      {"730", "Counter-Strike Global Offensive/game/csgo", "cs2", "Counter-Strike 2"},
}

// presetFiles locates the game's settings files for the signed-in Steam
// account.
func (a *App) presetFiles(game string) ([]PresetFile, []string) {
	g, ok := presetGames[game]
	if !ok {
		return nil, []string{"Unknown game."}
	}
	var reasons []string
	root := a.Gsi.steamRoot()
	acct := steamAccount()
	if root == "" || acct == 0 {
		return nil, []string{"Steam and its signed-in account couldn't be found."}
	}
	base := filepath.Join(root, "userdata", strconv.FormatUint(acct, 10), g.appID)
	var files []PresetFile
	add := func(kind, rel string, cloud bool) {
		p := filepath.Join(base, filepath.FromSlash(rel))
		_, err := os.Stat(p)
		files = append(files, PresetFile{Kind: kind, Path: p, Cloud: cloud, Found: err == nil})
	}
	switch game {
	case "dota":
		add("video", "local/cfg/video.txt", false)
		add("convars", "local/cfg/user_convars_0_slot0.vcfg", false)
		add("keys", "remote/cfg/dotakeys_personal.lst", true)
	case "deadlock":
		add("video", "local/cfg/video.txt", false)
		add("convars", "local/cfg/user_convars_0_slot0.vcfg", false)
		add("cloudconvars", "remote/user_convars.vcfg", true)
		add("keys", "remote/cfg/citadelkeys_personal.lst", true)
	case "cs2":
		add("video", "local/cfg/cs2_video.txt", false)
		add("convars", "local/cfg/cs2_user_convars_0_slot0.vcfg", false)
		add("keys", "local/cfg/cs2_user_keys_0_slot0.vcfg", false)
	}
	for _, f := range files {
		if !f.Found {
			reasons = append(reasons, fmt.Sprintf("%s has no %s settings file yet (open the game once and change any setting).", g.label, f.Kind))
		}
	}
	return files, reasons
}

func (a *App) presetInstalled(game string) bool {
	g := presetGames[game]
	for _, lib := range a.Gsi.steamLibraries() {
		if _, err := os.Stat(filepath.Join(lib, "steamapps", "appmanifest_"+g.appID+".acf")); err == nil {
			return true
		}
	}
	return false
}

func presetRunning(game string) bool {
	want := presetGames[game].process
	for _, name := range runningPrograms() {
		if strings.EqualFold(name, want) {
			return true
		}
	}
	return false
}

// ---------- Preview, apply, restore ----------

func (a *App) presetDocs(files []PresetFile) (map[string]*kvDoc, error) {
	docs := map[string]*kvDoc{}
	for _, f := range files {
		if !f.Found {
			continue
		}
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		doc, err := kvParse(string(raw))
		if err != nil {
			return nil, fmt.Errorf("%s couldn't be read as a settings file: %v", filepath.Base(f.Path), err)
		}
		docs[f.Kind] = doc
	}
	return docs, nil
}

func (a *App) PresetPreview(game, layout string) (PresetPreview, error) {
	g, ok := presetGames[game]
	if !ok {
		return PresetPreview{}, errors.New("Unknown game.")
	}
	p := PresetPreview{Game: game, Reasons: []string{}, Lines: []PresetLine{}, Layouts: presetLayouts[game], Layout: presetLayout(game, layout).ID}
	p.Installed = a.presetInstalled(game)
	p.Running = presetRunning(game)
	files, reasons := a.presetFiles(game)
	p.Files = files
	p.Reasons = append(p.Reasons, reasons...)
	if !p.Installed {
		p.Reasons = append(p.Reasons, g.label+" isn't installed on this PC; the preset applies once it is.")
	}
	if p.Running {
		p.Reasons = append(p.Reasons, g.label+" is running. Close it first: it rewrites its settings on exit.")
	}
	docs, err := a.presetDocs(files)
	if err != nil {
		return p, err
	}
	for _, line := range presetLines(game, docs, p.Layout) {
		if line.Info {
			p.Lines = append(p.Lines, line)
			continue
		}
		if doc := docs[line.File]; doc != nil {
			line.Current, line.Present = doc.Get(line.Path...)
		}
		if line.Remove {
			line.Changed = line.Present
		} else {
			line.Changed = !line.Present || !kvSame(line.Current, line.Value)
		}
		if line.Changed {
			p.Changes++
		}
		p.Lines = append(p.Lines, line)
	}
	p.CanApply = p.Installed && !p.Running && len(docs) > 0 && allFound(files)
	if dir := a.latestPresetBackup(game); dir != "" {
		p.LastBackup = filepath.Base(dir)
	}
	if dir, _ := a.oldestPresetBackupWith(game, "keys"); dir != "" {
		p.OriginalKeys = filepath.Base(dir)
	}
	return p, nil
}

// kvSame treats the games' two spellings of a boolean, and numbers that
// only differ in formatting, as the same value.
func kvSame(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	norm := func(s string) string {
		switch strings.ToLower(s) {
		case "true", "1":
			return "1"
		case "false", "0":
			return "0"
		}
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
		return s
	}
	return norm(a) == norm(b)
}

func allFound(files []PresetFile) bool {
	for _, f := range files {
		if !f.Found {
			return false
		}
	}
	return len(files) > 0
}

type presetManifest struct {
	Game  string            `json:"game"`
	At    string            `json:"at"`
	Files map[string]string `json:"files"` // original path → backup file name
}

func (a *App) presetBackupRoot(game string) string {
	return filepath.Join(a.Store.Dir, "presets-backup", game)
}

func (a *App) latestPresetBackup(game string) string {
	entries, err := os.ReadDir(a.presetBackupRoot(game))
	if err != nil || len(entries) == 0 {
		return ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return filepath.Join(a.presetBackupRoot(game), names[len(names)-1])
}

// replaceFile writes a file whole, via a temporary file and a rename, with
// the retries Windows needs when another program has the file open.
func replaceFile(path string, data []byte) error {
	tmp := path + ".thetracker-tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	err := os.Rename(tmp, path)
	for i := 0; err != nil && i < 8; i++ {
		time.Sleep(50 * time.Millisecond)
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// PresetApply backs the game's files up, then writes every line not in
// skip. The game must be closed.
func (a *App) PresetApply(game, layout string, skip []string) (PresetPreview, error) {
	p, err := a.PresetPreview(game, layout)
	if err != nil {
		return p, err
	}
	if !p.CanApply {
		return p, errors.New(strings.Join(p.Reasons, " "))
	}
	skipped := map[string]bool{}
	for _, id := range skip {
		skipped[id] = true
	}
	docs, err := a.presetDocs(p.Files)
	if err != nil {
		return p, err
	}
	touched := map[string]bool{}
	for _, line := range p.Lines {
		if skipped[line.ID] || !line.Changed || line.Info {
			continue
		}
		doc := docs[line.File]
		if doc == nil {
			continue
		}
		if line.Remove {
			doc.Delete(line.Path...)
		} else {
			doc.Set(line.Value, line.Path...)
		}
		touched[line.File] = true
	}
	if len(touched) == 0 {
		return p, errors.New("Nothing to change: every line is already set or was unticked.")
	}
	// Backup first, every touched file.
	stamp := time.Now().Format("2006-01-02_15-04-05")
	dir := filepath.Join(a.presetBackupRoot(game), stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return p, err
	}
	man := presetManifest{Game: game, At: stamp, Files: map[string]string{}}
	for _, f := range p.Files {
		if !touched[f.Kind] {
			continue
		}
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			return p, err
		}
		name := f.Kind + "_" + filepath.Base(f.Path)
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
			return p, err
		}
		man.Files[f.Path] = name
	}
	mb, _ := json.MarshalIndent(man, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		return p, err
	}
	// Then write.
	for _, f := range p.Files {
		if !touched[f.Kind] {
			continue
		}
		if err := replaceFile(f.Path, []byte(docs[f.Kind].String())); err != nil {
			return p, fmt.Errorf("Couldn't write %s: %v. Your backup is in %s.", filepath.Base(f.Path), err, dir)
		}
		// A fresh time, so Steam sees the cloud copy as older.
		now := time.Now()
		_ = os.Chtimes(f.Path, now, now)
	}
	return a.PresetPreview(game, layout)
}

// oldestPresetBackupWith finds the earliest backup holding a file of one
// kind, with that file's original path.
func (a *App) oldestPresetBackupWith(game, kind string) (dir, path string) {
	entries, err := os.ReadDir(a.presetBackupRoot(game))
	if err != nil {
		return "", ""
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		d := filepath.Join(a.presetBackupRoot(game), n)
		raw, err := os.ReadFile(filepath.Join(d, "manifest.json"))
		var man presetManifest
		if err != nil || json.Unmarshal(raw, &man) != nil {
			continue
		}
		for p, name := range man.Files {
			if strings.HasPrefix(name, kind+"_") {
				return d, p
			}
		}
	}
	return "", ""
}

// PresetRestoreOriginal puts one file back from the oldest backup that
// holds it: the player's own version from before any preset. The game
// must be closed.
func (a *App) PresetRestoreOriginal(game, kind, layout string) (PresetPreview, error) {
	if presetRunning(game) {
		return PresetPreview{}, errors.New(presetGames[game].label + " is running. Close it first.")
	}
	dir, path := a.oldestPresetBackupWith(game, kind)
	if dir == "" {
		return PresetPreview{}, errors.New("No backup holds that file.")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	var man presetManifest
	if json.Unmarshal(raw, &man) != nil {
		return PresetPreview{}, errors.New("The backup's manifest couldn't be read.")
	}
	data, err := os.ReadFile(filepath.Join(dir, man.Files[path]))
	if err != nil {
		return PresetPreview{}, err
	}
	if err := replaceFile(path, data); err != nil {
		return PresetPreview{}, fmt.Errorf("Couldn't restore %s: %v", filepath.Base(path), err)
	}
	now := time.Now()
	_ = os.Chtimes(path, now, now)
	return a.PresetPreview(game, layout)
}

// PresetRestore puts the latest backup's files back. The game must be
// closed.
func (a *App) PresetRestore(game, layout string) (PresetPreview, error) {
	if presetRunning(game) {
		return PresetPreview{}, errors.New(presetGames[game].label + " is running. Close it first.")
	}
	dir := a.latestPresetBackup(game)
	if dir == "" {
		return PresetPreview{}, errors.New("No backup to restore.")
	}
	var man presetManifest
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || json.Unmarshal(raw, &man) != nil {
		return PresetPreview{}, errors.New("The backup's manifest couldn't be read.")
	}
	for path, name := range man.Files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return PresetPreview{}, err
		}
		if err := replaceFile(path, data); err != nil {
			return PresetPreview{}, fmt.Errorf("Couldn't restore %s: %v", filepath.Base(path), err)
		}
		now := time.Now()
		_ = os.Chtimes(path, now, now)
	}
	// The backup stays, so a restore can be repeated; the next apply makes
	// a new one.
	return a.PresetPreview(game, layout)
}

package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakePresetSteam builds a Steam folder with the game installed and the
// player's settings files under userdata: the synthetic ones here, or with
// TT_KV_DIR set, copies of the real files.
func fakePresetSteam(t *testing.T, a *App, game string) map[string]string {
	t.Helper()
	root, _ := fakeSteam(t, "")
	g := presetGames[game]
	if err := os.MkdirAll(filepath.Join(root, "steamapps", "common", filepath.FromSlash(g.dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "\"AppState\"\n{\n\t\"appid\"\t\t\"" + g.appID + "\"\n}\n"
	if err := os.WriteFile(filepath.Join(root, "steamapps", "appmanifest_"+g.appID+".acf"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Gsi.steamRoot = func() string { return root }
	oldAcct := steamAccount
	steamAccount = func() uint64 { return 850402858 }
	t.Cleanup(func() { steamAccount = oldAcct })
	oldRun := runningPrograms
	runningPrograms = func() map[uint32]string { return map[uint32]string{} }
	t.Cleanup(func() { runningPrograms = oldRun })

	synthetic := map[string]map[string]string{
		"dota": {
			"video":   "\"video.cfg\"\r\n{\r\n\t\"setting.fullscreen\"\t\t\"0\"\r\n\t\"setting.r_ssao\"\t\t\"true\"\r\n}\r\n",
			"convars": "\"config\"\r\n{\r\n\t\"convars\"\r\n\t{\r\n\t\t\"dota_ability_quick_cast\"\t\t\"false\"\r\n\t\t\"dota_camera_speed\"\t\t\"3000\"\r\n\t}\r\n}\r\n",
			"keys":    "\"KeyBindings\"\r\n{\r\n\t\"Name\"\t\t\"x\"\r\n\t\"Keys\"\r\n\t{\r\n\t\t\"AbilityPrimary1\"\r\n\t\t{\r\n\t\t\t\"Key\"\t\t\"Q\"\r\n\t\t}\r\n\t\t\"SelectAllOthers\"\r\n\t\t{\r\n\t\t\t\"Key\"\t\t\"T\"\r\n\t\t\t\"Modifier\"\t\t\"ALT\"\r\n\t\t}\r\n\t}\r\n\t\"Items\"\r\n\t{\r\n\t\t\"item_blink\"\r\n\t\t{\r\n\t\t\t\"Mode\"\t\t\"1\"\r\n\t\t}\r\n\t\t\"item_wand\"\r\n\t\t{\r\n\t\t\t\"Mode\"\t\t\"0\"\r\n\t\t}\r\n\t}\r\n}\r\n",
		},
		"deadlock": {
			"video":        "\"video.cfg\"\r\n{\r\n\t\"setting.fullscreen\"\t\t\"1\"\r\n}\r\n",
			"convars":      "\"config\"\r\n{\r\n\t\"convars\"\r\n\t{\r\n\t\t\"citadel_camera_hero_fov\"\t\t\"90\"\r\n\t}\r\n}\r\n",
			"cloudconvars": "\"config\"\r\n{\r\n\t\"convars\"\r\n\t{\r\n\t\t\"citadel_camera_hero_fov\"\t\t\"80\"\r\n\t}\r\n}\r\n",
			"keys":         "\"KeyBindings\"\r\n{\r\n\t\"Keys\"\r\n\t{\r\n\t\t\"Item1\"\r\n\t\t{\r\n\t\t\t\"Key\"\t\t\"MOUSE5\"\r\n\t\t\t\"Key2\"\t\t\"NONE\"\r\n\t\t}\r\n\t}\r\n}\r\n",
		},
		"cs2": {
			"video":   "\"video.cfg\"\r\n{\r\n\t\"setting.fullscreen\"\t\t\"1\"\r\n}\r\n",
			"convars": "\"config\"\r\n{\r\n\t\"convars\"\r\n\t{\r\n\t\t\"volume\"\t\t\"1\"\r\n\t}\r\n}\r\n",
			"keys":    "\"config\"\r\n{\r\n\t\"bindings\"\r\n\t{\r\n\t\t\"MWHEELUP\"\t\t\"+jump\"\r\n\t}\r\n}\r\n",
		},
	}
	real := map[string]string{}
	if dir := os.Getenv("TT_KV_DIR"); dir != "" {
		prefix := map[string]string{"dota": "dota_", "deadlock": "dl_", "cs2": "cs2_"}[game]
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix) {
				kind := strings.TrimPrefix(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())), prefix)
				real[kind] = filepath.Join(dir, e.Name())
			}
		}
	}
	files, _ := a.presetFiles(game)
	contents := map[string]string{}
	for _, f := range files {
		var data []byte
		if p, ok := real[f.Kind]; ok {
			var err error
			if data, err = os.ReadFile(p); err != nil {
				t.Fatal(err)
			}
		} else {
			data = []byte(synthetic[game][f.Kind])
		}
		if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.Path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		contents[f.Path] = string(data)
	}
	return contents
}

func TestPresetsApplyThenRestoreExactly(t *testing.T) {
	for _, game := range []string{"dota", "deadlock", "cs2"} {
		t.Run(game, func(t *testing.T) {
			a := newTestApp(t)
			before := fakePresetSteam(t, a, game)
			p, err := a.PresetPreview(game)
			if err != nil {
				t.Fatal(err)
			}
			if !p.Installed || p.Running || !p.CanApply || p.Changes == 0 || len(p.Lines) < 40 {
				t.Fatalf("preview: installed %v running %v canApply %v changes %d lines %d reasons %v", p.Installed, p.Running, p.CanApply, p.Changes, len(p.Lines), p.Reasons)
			}
			// Every line names a group and a file that exists.
			kinds := map[string]bool{}
			for _, f := range p.Files {
				kinds[f.Kind] = true
			}
			for _, l := range p.Lines {
				if !kinds[l.File] || l.Group == "" || l.Label == "" || len(l.Path) < 2 {
					t.Fatalf("bad line %+v", l)
				}
			}
			// Skip one line, apply the rest.
			var skip []string
			for _, l := range p.Lines {
				if l.Changed {
					skip = append(skip, l.ID)
					break
				}
			}
			after, err := a.PresetApply(game, skip)
			if err != nil {
				t.Fatal(err)
			}
			if after.Changes != 1 || after.LastBackup == "" {
				t.Fatalf("after apply: %d changes, backup %q", after.Changes, after.LastBackup)
			}
			for _, l := range after.Lines {
				if l.Changed && l.ID != skip[0] {
					t.Fatalf("still differs: %+v", l)
				}
			}
			// Files were rewritten, still parse, kept their line endings, and
			// every original line that the preset didn't touch is still there.
			for path, orig := range before {
				now, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := kvParse(string(now)); err != nil {
					t.Fatalf("%s after apply: %v", path, err)
				}
				if strings.Contains(orig, "\r\n") != strings.Contains(string(now), "\r\n") {
					t.Fatalf("%s changed its line endings", path)
				}
				if _, err := os.Stat(path + ".thetracker-tmp"); err == nil {
					t.Fatalf("temp file left behind at %s", path)
				}
			}
			// Restore puts every byte back.
			restored, err := a.PresetRestore(game)
			if err != nil {
				t.Fatal(err)
			}
			if restored.Changes != p.Changes {
				t.Fatalf("after restore %d changes, want %d", restored.Changes, p.Changes)
			}
			for path, orig := range before {
				now, _ := os.ReadFile(path)
				if string(now) != orig {
					t.Fatalf("%s not restored exactly", path)
				}
			}
		})
	}
}

func TestPresetsRefuseWhileTheGameRuns(t *testing.T) {
	a := newTestApp(t)
	fakePresetSteam(t, a, "dota")
	runningPrograms = func() map[uint32]string { return map[uint32]string{1: "dota2"} }
	p, err := a.PresetPreview("dota")
	if err != nil || !p.Running || p.CanApply {
		t.Fatalf("running: %+v %v", p.Reasons, err)
	}
	if _, err := a.PresetApply("dota", nil); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("apply while running: %v", err)
	}
	if _, err := a.PresetRestore("dota"); err == nil {
		t.Fatal("restore while running")
	}
}

func TestDotaPresetKeepsAimedItemsOnNormalCast(t *testing.T) {
	t.Setenv("TT_KV_DIR", "")
	a := newTestApp(t)
	fakePresetSteam(t, a, "dota")
	p, err := a.PresetPreview("dota")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, l := range p.Lines {
		if l.File == "keys" {
			got[strings.Join(l.Path, "/")] = l.Value
			if l.Remove {
				got[strings.Join(l.Path, "/")] = "<removed>"
			}
		}
	}
	for path, want := range map[string]string{
		"KeyBindings/Items/item_blink/Mode":                 "0",
		"KeyBindings/Items/item_wand/Mode":                  "1",
		"KeyBindings/Keys/Inventory1/Key":                   "Z",
		"KeyBindings/Keys/Inventory1/Mode":                  "1",
		"KeyBindings/Keys/SelectAllOthers/Key":              "`",
		"KeyBindings/Keys/SelectAllOthers/Modifier":         "<removed>",
		"KeyBindings/Keys/AbilityPrimary1AutoCast/Modifier": "ALT",
	} {
		if got[path] != want {
			t.Errorf("%s = %q, want %q", path, got[path], want)
		}
	}
}

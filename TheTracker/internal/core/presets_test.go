package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	pubgDir := filepath.Join(root, "LocalAppData", "TslGame")
	oldPubg := pubgConfigDir
	pubgConfigDir = func() string { return pubgDir }
	t.Cleanup(func() { pubgConfigDir = oldPubg })
	valDir := filepath.Join(root, "LocalAppData", "VALORANT", "Saved", "Config")
	oldVal := valConfigDir
	valConfigDir = func() string { return valDir }
	t.Cleanup(func() { valConfigDir = oldVal })
	codDir := filepath.Join(root, "Documents", "Call of Duty", "players")
	oldCod := codPlayersDir
	codPlayersDir = func() string { return codDir }
	t.Cleanup(func() { codPlayersDir = oldCod })
	if game == "cod" {
		fakeCodFiles(t, codDir)
	}
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
		"valorant": {
			"settings": "[/Script/ShooterGame.ShooterGameUserSettings]\r\nDefaultMonitorIndex=0\r\nbUseVSync=True\r\nbUseDynamicResolution=False\r\nLastConfirmedFullscreenMode=1\r\nPreferredFullscreenMode=1\r\nFrameRateLimit=144.000000\r\nFullscreenMode=1\r\n\r\n[/Script/Engine.GameUserSettings]\r\nbUseDesiredScreenHeight=False\r\n",
		},
		"pubg": {
			"settings": "[/Script/Engine.GameUserSettings]\r\nbUseDesiredScreenHeight=False\r\n\r\n[ScalabilityGroups]\r\nsg.ShadowQuality=2\r\nsg.TextureQuality=1\r\n\r\n[/Script/TslGame.TslGameUserSettings]\r\nFpsCameraFov=103.000000\r\nbSharpen=False\r\nOutgameUserDatas=(Key=\"X\",Value=\"stays\")\r\nFullscreenMode=1\r\n",
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
		if game == "cod" {
			// fakeCodFiles wrote them already.
			var err error
			if data, err = os.ReadFile(f.Path); err != nil {
				t.Fatal(err)
			}
		} else if p, ok := real[f.Kind]; ok {
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
	for _, game := range []string{"dota", "deadlock", "cs2", "pubg", "cod", "valorant"} {
		for _, layout := range presetLayouts[game] {
			t.Run(game+"/"+layout.ID, func(t *testing.T) { presetRoundTrip(t, game, layout.ID) })
		}
	}
}

func presetRoundTrip(t *testing.T, game, layout string) {
	{
		{
			a := newTestApp(t)
			before := fakePresetSteam(t, a, game)
			p, err := a.PresetPreview(game, layout)
			if err != nil {
				t.Fatal(err)
			}
			// Valorant keeps most settings on the Riot account: fewer lines.
			minLines := 25
			if game == "valorant" {
				minLines = 8
			}
			if !p.Installed || p.Running || !p.CanApply || p.Changes == 0 || len(p.Lines) < minLines {
				t.Fatalf("preview: installed %v running %v canApply %v changes %d lines %d reasons %v", p.Installed, p.Running, p.CanApply, p.Changes, len(p.Lines), p.Reasons)
			}
			// Every line names a group and a file that exists.
			kinds := map[string]bool{}
			for _, f := range p.Files {
				kinds[f.Kind] = true
			}
			if p.Layout != layout || len(p.Layouts) == 0 {
				t.Fatalf("layout %q of %v", p.Layout, p.Layouts)
			}
			for _, l := range p.Lines {
				if l.Source == "" || l.Group == "" || l.Label == "" {
					t.Fatalf("line without a source or label: %+v", l)
				}
				if l.Info {
					if l.Changed || len(l.Path) != 0 {
						t.Fatalf("info line must never change: %+v", l)
					}
					continue
				}
				if !kinds[l.File] || len(l.Path) < 2 && game != "cod" {
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
			after, err := a.PresetApply(game, layout, skip)
			if err != nil {
				t.Fatal(err)
			}
			if after.Changes != 1 || after.LastBackup == "" || after.Layout != layout {
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
				if _, err := kvParse(string(now)); err != nil && !strings.HasSuffix(path, ".ini") && !isCodSettingsFile(path) {
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
			restored, err := a.PresetRestore(game, layout)
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
		}
	}
}

func TestPresetsResetHotkeysToTheOriginals(t *testing.T) {
	t.Setenv("TT_KV_DIR", "")
	a := newTestApp(t)
	before := fakePresetSteam(t, a, "dota")
	if _, err := a.PresetApply("dota", "yatoro", nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond) // backups are named to the second
	if _, err := a.PresetApply("dota", "puppey", nil); err != nil {
		t.Fatal(err)
	}
	p, err := a.PresetRestoreOriginal("dota", "keys", "puppey")
	if err != nil {
		t.Fatal(err)
	}
	if p.OriginalKeys == "" || p.Layout != "puppey" {
		t.Fatalf("preview after reset: %+v", p)
	}
	for path, orig := range before {
		now, _ := os.ReadFile(path)
		isKeys := strings.HasSuffix(path, ".lst")
		if isKeys && string(now) != orig {
			t.Fatalf("keys not back to the original")
		}
		if !isKeys && string(now) == orig {
			t.Fatalf("%s should still carry the preset", filepath.Base(path))
		}
	}
}

func TestPresetsRefuseWhileTheGameRuns(t *testing.T) {
	a := newTestApp(t)
	fakePresetSteam(t, a, "dota")
	runningPrograms = func() map[uint32]string { return map[uint32]string{1: "dota2"} }
	p, err := a.PresetPreview("dota", "")
	if err != nil || !p.Running || p.CanApply {
		t.Fatalf("running: %+v %v", p.Reasons, err)
	}
	if _, err := a.PresetApply("dota", "", nil); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("apply while running: %v", err)
	}
	if _, err := a.PresetRestore("dota", ""); err == nil {
		t.Fatal("restore while running")
	}
}

func presetKeyValues(t *testing.T, game, layout string) map[string]string {
	t.Helper()
	t.Setenv("TT_KV_DIR", "")
	a := newTestApp(t)
	fakePresetSteam(t, a, game)
	p, err := a.PresetPreview(game, layout)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, l := range p.Lines {
		if l.File == "keys" && !l.Info {
			got[strings.Join(l.Path, "/")] = l.Value
			if l.Remove {
				got[strings.Join(l.Path, "/")] = "<removed>"
			}
		}
	}
	return got
}

func expectKeys(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	for path, v := range want {
		if got[path] != v {
			t.Errorf("%s = %q, want %q", path, got[path], v)
		}
	}
}

func TestDotaLayouts(t *testing.T) {
	// Classic: default rows, quick cast off except the aimed items the
	// guides name; the synthetic file's Alt modifier on SelectAllOthers is
	// left alone because the layout doesn't bind that action.
	expectKeys(t, presetKeyValues(t, "dota", "standard"), map[string]string{
		"KeyBindings/Items/item_blink/Mode":    "0",
		"KeyBindings/Items/item_wand/Mode":     "0",
		"KeyBindings/Keys/Inventory1/Key":      "Z",
		"KeyBindings/Keys/Inventory1/Mode":     "0",
		"KeyBindings/Keys/AbilityPrimary1/Key": "Q",
		"KeyBindings/Keys/CameraGrip/Key":      "MOUSE3",
	})
	if _, has := presetKeyValues(t, "dota", "standard")["KeyBindings/Keys/SelectAllOthers/Modifier"]; has {
		t.Error("classic layout touched SelectAllOthers")
	}
	// Yatoro: items on the number row and Space, everything quick cast,
	// control groups moved off the numbers.
	expectKeys(t, presetKeyValues(t, "dota", "yatoro"), map[string]string{
		"KeyBindings/Items/item_blink/Mode":     "1",
		"KeyBindings/Items/item_wand/Mode":      "1",
		"KeyBindings/Keys/Inventory1/Key":       "1",
		"KeyBindings/Keys/Inventory5/Key":       "SPACE",
		"KeyBindings/Keys/Inventory6/Key":       "C",
		"KeyBindings/Keys/AbilityUltimate/Mode": "1",
		"KeyBindings/Keys/HeroSelect/Key":       "F1",
		"KeyBindings/Keys/ControlGroup1/Key":    "F2",
	})
	// miCKe: items on Alt+key, so the modifier is written.
	expectKeys(t, presetKeyValues(t, "dota", "micke"), map[string]string{
		"KeyBindings/Keys/Inventory1/Key":      "Q",
		"KeyBindings/Keys/Inventory1/Modifier": "ALT",
		"KeyBindings/Keys/AbilityPrimary3/Key": "F",
	})
	// An unknown layout id falls back to the first one.
	a := newTestApp(t)
	fakePresetSteam(t, a, "dota")
	if p, err := a.PresetPreview("dota", "nope"); err != nil || p.Layout != "yatoro" {
		t.Fatalf("fallback layout %q %v", p.Layout, err)
	}
}

func TestDeadlockLayouts(t *testing.T) {
	expectKeys(t, presetKeyValues(t, "deadlock", "lefaa"), map[string]string{
		"KeyBindings/Keys/Item1/Key":             "Z",
		"KeyBindings/Keys/Item2/Key":             "E",
		"KeyBindings/Keys/Item3/Key":             "MOUSE4",
		"KeyBindings/Keys/Item4/Key":             "MOUSE5",
		"KeyBindings/Keys/Item4/Key2":            "NONE",
		"KeyBindings/Keys/AbilityMelee/Key":      "Q",
		"KeyBindings/Keys/AltCast/Key":           "MOUSE3",
		"KeyBindings/Keys/ItemQuickCastMode/Key": "1",
	})
	expectKeys(t, presetKeyValues(t, "deadlock", "mikaels"), map[string]string{
		"KeyBindings/Keys/Item1/Key":        "Q",
		"KeyBindings/Keys/AbilityMelee/Key": "MOUSE5",
	})
}

// fakeCodFiles writes Call of Duty's settings files: with TT_COD_DIR set,
// copies of the real ones; else lines for every preset setting, holding
// other values, plus one the game would refuse and one comment.
func fakeCodFiles(t *testing.T, dir string) {
	t.Helper()
	if real := os.Getenv("TT_COD_DIR"); real != "" {
		filepath.WalkDir(real, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || !isCodSettingsFile(p) {
				return nil
			}
			rel, _ := filepath.Rel(real, p)
			raw, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755)
			os.WriteFile(filepath.Join(dir, rel), raw, 0o644)
			return nil
		})
		return
	}
	var sys, play strings.Builder
	sys.WriteString("1\n\n// Display\n")
	play.WriteString("1\n\n")
	seen := map[string]bool{}
	for _, l := range presetLines("cod", nil, "multiplayer") {
		if l.Info || seen[l.File+l.Path[0]] {
			continue
		}
		seen[l.File+l.Path[0]] = true
		line := l.Path[0] + ";1;2 = x\n"
		switch l.File {
		case "system":
			sys.WriteString(line)
		case "gameplay":
			play.WriteString(line)
		}
	}
	sys.WriteString("Fov@9;1;2 = 1 // 0 to 5\n")
	for _, name := range []string{"s.1.0.cod24.txt0", "s.1.0.cod24.txt1", "s.1.0.cod21.txt0"} {
		os.MkdirAll(dir, 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(sys.String()), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "123"), 0o755)
	for _, name := range []string{"g.1.0.l.txt0", "g.1.0.l.txt1"} {
		os.WriteFile(filepath.Join(dir, "123", name), []byte(play.String()), 0o644)
	}
}

func TestCodPresetFindsTheNewestFiles(t *testing.T) {
	a := newTestApp(t)
	fakePresetSteam(t, a, "cod")
	files, reasons := a.presetFiles("cod")
	if len(files) != 4 || len(reasons) != 0 {
		t.Fatalf("files %+v reasons %v", files, reasons)
	}
	if os.Getenv("TT_COD_DIR") == "" && (filepath.Base(files[0].Path) != "s.1.0.cod24.txt0" || filepath.Base(files[1].Path) != "s.1.0.cod24.txt1" || filepath.Base(files[3].Path) != "g.1.0.l.txt1") {
		t.Fatalf("picked %+v", files)
	}
	p, err := a.PresetPreview("cod", "warzone")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range p.Lines {
		if l.Info {
			continue
		}
		if l.Path[0] == "Fov@1" && l.Value != "105.000000" {
			t.Fatalf("warzone FOV %+v", l)
		}
		if !l.Present {
			t.Fatalf("a setting the file doesn't hold: %+v", l)
		}
	}
	if os.Getenv("TT_COD_DIR") != "" {
		t.Logf("%d lines, %d would change", len(p.Lines), p.Changes)
	}
}

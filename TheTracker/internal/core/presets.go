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
	// Remove the key instead of setting it (a modifier the layout drops).
	Remove bool `json:"remove,omitempty"`
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
	// How many lines would change.
	Changes    int    `json:"changes"`
	LastBackup string `json:"lastBackup,omitempty"`
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

// ---------- The tables ----------

type presetBuilder struct {
	game  string
	lines []PresetLine
}

func (b *presetBuilder) add(group, file, label, value, note string, path ...string) {
	id := b.game + "." + file + "." + strings.Join(path, "/")
	b.lines = append(b.lines, PresetLine{ID: id, Group: group, Label: label, File: file, Path: path, Value: value, Note: note})
}

func (b *presetBuilder) video(key, value, label, note string) {
	b.add("Video", "video", label, value, note, "video.cfg", "setting."+key)
}

func (b *presetBuilder) convar(file, group, name, value, label, note string) {
	b.add(group, file, label, value, note, "config", "convars", name)
}

// presetLines builds the lines for a game; docs (by file kind) let a few
// lines follow what the player's file holds (Dota's per-item cast modes).
func presetLines(game string, docs map[string]*kvDoc) []PresetLine {
	b := &presetBuilder{game: game}
	switch game {
	case "dota":
		dotaPreset(b, docs["keys"])
	case "deadlock":
		deadlockPreset(b)
	case "cs2":
		cs2Preset(b)
	}
	return b.lines
}

func dotaPreset(b *presetBuilder, keys *kvDoc) {
	// Video: visibility first on a strong GPU.
	b.video("fullscreen", "1", "Fullscreen (exclusive)", "Lowest input lag. Untick to keep borderless windowed.")
	b.video("nowindowborder", "1", "No window border", "")
	b.video("defaultres", "2560", "Width", "")
	b.video("defaultresheight", "1440", "Height", "")
	b.video("mat_vsync", "0", "V-sync off", "")
	b.video("r_low_latency", "1", "Low-latency mode", "")
	b.video("r_fullscreen_gamma", "2.2", "Gamma", "")
	b.video("r_ssao", "false", "Ambient occlusion off", "Darkens corners; hides nothing useful.")
	b.video("r_deferred_specular_bloom", "false", "Specular bloom off", "")
	b.video("r_depth_of_field", "0", "Depth of field off", "")
	b.video("dota_ambient_creatures", "false", "Ambient creatures off", "")
	b.video("dota_ambient_cloth", "false", "Cloth simulation off", "")
	b.video("r_grass_quality", "0", "Grass off", "Clearer ground, easier to read positions.")
	b.video("r_dota_allow_wind_on_trees", "false", "Tree wind off", "")
	b.video("cl_globallight_shadow_mode", "0", "Shadows low", "")
	b.video("dota_cheap_water", "true", "Simple water", "")
	b.video("r_dota_fxaa", "true", "Anti-aliasing on", "Kept: clean edges help reading the fight.")
	b.video("shaderquality", "1", "Shader quality high", "")
	b.video("r_dota_normal_maps", "true", "Normal maps on", "")
	b.video("mat_viewportscale", "1", "Render at native resolution", "")
	b.video("r_dota_fsr_upsample", "0", "No upscaling", "Upscaling softens the picture.")
	b.video("r_texture_stream_mip_bias", "0", "Full texture detail", "")

	cv := func(group, name, value, label, note string) { b.convar("convars", group, name, value, label, note) }
	// Audio: game sounds first, music out of the way.
	cv("Audio", "volume", "0.6", "Master volume 60%", "")
	cv("Audio", "snd_musicvolume", "0", "Music off", "")
	cv("Audio", "snd_gamevolume", "0.9", "Game sounds 90%", "Spell and attack sounds are information.")
	cv("Audio", "snd_gamevoicevolume", "0.6", "Hero voices 60%", "")
	cv("Audio", "snd_mute_losefocus", "false", "Keep sound when alt-tabbed", "")
	cv("Audio", "dota_force_default_death_stinger", "true", "Default death sound", "")
	cv("Audio", "dota_force_default_respawn_stinger", "true", "Default respawn sound", "")
	// Gameplay: how pros set the client.
	cv("Gameplay", "dota_ability_quick_cast", "true", "Quick cast on", "")
	cv("Gameplay", "dota_quickcast_onkeydown", "true", "Quick cast on key down", "Casts the moment the key goes down.")
	cv("Gameplay", "dota_smart_doubletap", "true", "Smart double-tap self-cast", "")
	cv("Gameplay", "dota_item_double_tap_self_cast", "true", "Double-tap items on self", "")
	cv("Gameplay", "dota_ability_legacy_mode_quick_cast", "false", "Legacy quick cast off", "")
	cv("Gameplay", "dota_player_units_auto_attack_mode", "1", "Auto-attack: standard", "")
	cv("Gameplay", "dota_disable_autoattack_during_stop", "true", "No auto-attack while stopped", "")
	cv("Gameplay", "dota_force_right_click_attack", "2", "Right-click to force attack", "")
	cv("Gameplay", "dota_player_teleport_requires_halt", "true", "Teleport needs a stop", "No accidental TP cancels.")
	cv("Gameplay", "dota_player_multipler_orders", "false", "Orders go to the hero only", "")
	cv("Gameplay", "dota_camera_speed", "4200", "Camera speed 4200", "")
	cv("Gameplay", "dota_camera_disable_zoom", "true", "Camera zoom locked", "")
	cv("Gameplay", "dota_camera_accelerate", "50", "Camera acceleration 50", "")
	cv("Gameplay", "dota_leftclick_cameragrip_new", "false", "No left-click camera grip", "")
	cv("Gameplay", "dota_screen_shake", "false", "Screen shake off", "")
	cv("Gameplay", "dota_camera_deatheffect", "false", "Death camera effect off", "")
	cv("Gameplay", "dota_enable_range_finder", "true", "Range finder on", "")
	cv("Gameplay", "dota_show_range_finder_on_quickcast_down", "true", "Range finder on quick cast", "")
	cv("Gameplay", "dota_shop_force_hotkeys", "true", "Shop hotkeys always", "")
	cv("Gameplay", "dota_quickbuy_rightclick_marksforbuy", "true", "Right-click marks quick-buy", "")
	cv("Gameplay", "dota_courier_autodeliver", "false", "Courier delivers on your call", "")
	// Visibility and accessibility.
	cv("Visibility", "dota_hud_colorblind", "1", "Colour-blind palette", "Clearer team colours even with normal sight; untick for the default.")
	cv("Visibility", "dota_hud_healthbar_local_player_high_visibility", "true", "Your health bar stands out", "")
	cv("Visibility", "dota_hud_disable_damage_numbers", "false", "Damage numbers on", "")
	cv("Visibility", "dota_hud_healthbar_number", "true", "Numbers on health bars", "")
	cv("Visibility", "dota_hero_overhead_names", "true", "Hero names overhead", "")
	cv("Visibility", "dota_minimap_simple_colors", "true", "Simple minimap colours", "Green allies, red enemies.")
	cv("Visibility", "dota_minimap_simple_background", "true", "Simple minimap background", "")
	cv("Visibility", "dota_minimap_hero_size", "900", "Big minimap icons", "")
	cv("Visibility", "dota_minimap_always_draw_hero_icons", "true", "Always draw hero icons", "")
	cv("Visibility", "dota_hud_extra_large_minimap", "1", "Extra-large minimap", "")
	cv("Visibility", "dota_alt_shows_tower_attack_range", "true", "Alt shows tower range", "")
	cv("Visibility", "dota_alt_shows_neutral_spawn_boxes", "true", "Alt shows camp boxes", "")
	cv("Visibility", "dota_hud_contextual_tips_disable", "true", "No contextual tips", "")
	cv("Visibility", "dota_help_tips_enabled", "false", "No help tips", "")
	cv("Visibility", "dota_hud_netgraph", "false", "Net graph off", "The app's Connection check covers it.")

	// Hotkeys: the full pro layout.
	type bind struct{ action, key, mod, mode string }
	binds := []bind{
		{"AbilityPrimary1", "Q", "", "1"}, {"AbilityPrimary2", "W", "", "1"}, {"AbilityPrimary3", "E", "", "1"},
		{"AbilitySecondary1", "D", "", "1"}, {"AbilitySecondary2", "F", "", "1"}, {"AbilityUltimate", "R", "", "1"},
		{"AbilityPrimary1AutoCast", "Q", "ALT", ""}, {"AbilityPrimary2AutoCast", "W", "ALT", ""}, {"AbilityPrimary3AutoCast", "E", "ALT", ""},
		{"AbilitySecondary1AutoCast", "D", "ALT", ""}, {"AbilitySecondary2AutoCast", "F", "ALT", ""}, {"AbilityUltimateAutoCast", "R", "ALT", ""},
		{"Inventory1", "Z", "", "1"}, {"Inventory2", "X", "", "1"}, {"Inventory3", "C", "", "1"},
		{"Inventory4", "V", "", "1"}, {"Inventory5", "B", "", "1"}, {"Inventory6", "N", "", "1"},
		{"InventoryTP", "T", "", "1"}, {"InventoryNeutral", "G", "", "1"},
		{"ControlGroup1", "1", "", ""}, {"ControlGroup2", "2", "", ""}, {"ControlGroup3", "3", "", ""}, {"ControlGroup4", "4", "", ""}, {"ControlGroup5", "5", "", ""},
		{"SelectAll", "F5", "", ""}, {"SelectAllOthers", "`", "", ""}, {"ScoreboardToggle", "TAB", "", ""},
		{"HeroSelect", "F1", "", ""}, {"CourierSelect", "F2", "", ""}, {"CourierDeliver", "F3", "", ""}, {"ScoutSelect", "F4", "", ""},
		{"LearnAbilities", "U", "", ""}, {"LearnStats", "O", "", ""},
		{"ShopToggle", "P", "", ""}, {"PurchaseQuickbuy", "P", "ALT", ""}, {"PurchaseSticky", "Q", "ALT", ""},
		{"ActivateGlyph", "J", "", ""}, {"ActivateRadar", "K", "", ""},
		{"HeroAttack", "A", "", ""}, {"HeroMove", "M", "", ""}, {"HeroStop", "S", "", ""}, {"HeroHold", "H", "", ""},
		{"CameraGrip", "MOUSE3", "", ""},
		{"ChatTeam", "ENTER", "", ""}, {"ChatGlobal", "ENTER", "SHIFT", ""}, {"ChatVoiceTeam", "MOUSE4", "", ""}, {"ChatWheel", "MOUSE5", "", ""},
		{"CombatLogToggle", "L", "", ""}, {"ScreenshotSettings", "F12", "", ""}, {"Console", `\`, "", ""},
	}
	for _, k := range binds {
		label := k.action
		if k.mod != "" {
			label += " = " + k.mod + "+" + k.key
		} else {
			label += " = " + k.key
		}
		b.add("Hotkeys", "keys", label, k.key, "", "KeyBindings", "Keys", k.action, "Key")
		if k.mod != "" {
			b.add("Hotkeys", "keys", k.action+" modifier", k.mod, "", "KeyBindings", "Keys", k.action, "Modifier")
		} else if keys != nil {
			if _, has := keys.Get("KeyBindings", "Keys", k.action, "Modifier"); has {
				line := PresetLine{ID: "dota.keys.KeyBindings/Keys/" + k.action + "/Modifier", Group: "Hotkeys", Label: k.action + " modifier removed", File: "keys",
					Path: []string{"KeyBindings", "Keys", k.action, "Modifier"}, Value: "", Remove: true}
				b.lines = append(b.lines, line)
			}
		}
		if k.mode != "" {
			b.add("Hotkeys", "keys", k.action+" quick cast", k.mode, "", "KeyBindings", "Keys", k.action, "Mode")
		}
	}
	// Per-item cast mode: quick cast on everything except the targeted
	// items pros keep on normal cast (a mis-aimed Blink or Force is a death).
	normal := map[string]bool{"item_blink": true, "item_overwhelming_blink": true, "item_swift_blink": true, "item_arcane_blink": true,
		"item_force_staff": true, "item_hurricane_pike": true, "item_glimmer_cape": true, "item_cyclone": true, "item_wind_waker": true,
		"item_sheepstick": true, "item_orchid": true, "item_bloodthorn": true, "item_abyssal_blade": true, "item_ghost": true,
		"item_dagon": true, "item_dagon_2": true, "item_dagon_3": true, "item_dagon_4": true, "item_dagon_5": true}
	if keys != nil {
		var items []string
		for _, n := range keys.Block("KeyBindings", "Items") {
			if strings.HasPrefix(n.Key, "item_") {
				items = append(items, n.Key)
			}
		}
		sort.Strings(items)
		for _, it := range items {
			mode, label := "1", "quick cast"
			if normal[it] {
				mode, label = "0", "normal cast (aimed)"
			}
			b.add("Hotkeys", "keys", strings.TrimPrefix(it, "item_")+": "+label, mode, "", "KeyBindings", "Items", it, "Mode")
		}
	}
}

func deadlockPreset(b *presetBuilder) {
	b.video("fullscreen", "1", "Fullscreen (exclusive)", "Lowest input lag. Untick to keep borderless windowed.")
	b.video("nowindowborder", "1", "No window border", "")
	b.video("defaultres", "2560", "Width", "")
	b.video("defaultresheight", "1440", "Height", "")
	b.video("mat_vsync", "0", "V-sync off", "")
	b.video("r_low_latency", "1", "Low-latency mode", "")
	b.video("r_fullscreen_gamma", "2.2", "Gamma", "")
	b.video("r_citadel_motion_blur", "0", "Motion blur off", "")
	b.video("r_depth_of_field", "false", "Depth of field off", "")
	b.video("r_effects_bloom", "false", "Bloom off", "")
	b.video("r_post_bloom", "false", "Post bloom off", "")
	b.video("r_citadel_ssao_quality", "0", "Ambient occlusion off", "")
	b.video("r_arealights", "false", "Area lights off", "")
	b.video("r_citadel_fog_quality", "0", "Fog low", "")
	b.video("r_citadel_shadow_quality", "0", "Shadows low", "")
	b.video("r_citadel_antialiasing", "1", "Anti-aliasing on", "Kept for clean edges.")
	b.video("shaderquality", "1", "Shader quality high", "")
	b.video("r_citadel_upscaling", "0", "No upscaling", "")
	b.video("r_texture_stream_mip_bias", "0", "Full texture detail", "")
	b.video("mat_viewportscale", "1", "Render at native resolution", "")

	for _, file := range []string{"convars", "cloudconvars"} {
		cv := func(group, name, value, label, note string) { b.convar(file, group, name, value, label, note) }
		cv("Audio", "volume", "0.6", "Master volume 60%", "")
		cv("Audio", "snd_musicvolume", "0", "Music off", "")
		cv("Audio", "snd_gamevolume", "0.9", "Game sounds 90%", "")
		cv("Audio", "snd_gamevoicevolume", "0.5", "Hero voices 50%", "")
		cv("Audio", "snd_mute_losefocus", "false", "Keep sound when alt-tabbed", "")
		cv("Gameplay", "citadel_camera_hero_fov", "90", "Field of view 90", "")
		cv("Gameplay", "citadel_reduce_camera_shake", "true", "Reduced camera shake", "")
		cv("Gameplay", "cl_citadel_auto_fire", "false", "Auto-fire off", "")
		cv("Gameplay", "cl_citadel_zoom_is_toggle", "false", "Hold to zoom", "")
		cv("Gameplay", "cl_citadel_auto_mantle_forward", "true", "Auto-mantle forward", "")
		cv("Gameplay", "cl_citadel_auto_mantle_in_air", "true", "Auto-mantle in the air", "")
		cv("Gameplay", "cl_citadel_auto_mantle_backward", "false", "No auto-mantle backward", "")
		cv("Gameplay", "cl_citadel_auto_move", "false", "Auto-move off", "")
		cv("Gameplay", "option_duck_method", "false", "Hold to crouch", "")
		cv("Gameplay", "citadel_player_cam_toggle", "false", "Hold for player camera", "")
		cv("Visibility", "citadel_custom_ui_colors", "true", "Custom UI colours on", "")
		cv("Visibility", "citadel_enemy_ui_color_r", "255", "Enemy colour: red", "")
		cv("Visibility", "citadel_enemy_ui_color_g", "70", "Enemy colour: green", "")
		cv("Visibility", "citadel_enemy_ui_color_b", "70", "Enemy colour: blue", "")
		cv("Visibility", "citadel_enemy_zipline_color_r", "255", "Enemy zipline colour: red", "")
		cv("Visibility", "citadel_enemy_zipline_color_g", "70", "Enemy zipline colour: green", "")
		cv("Visibility", "citadel_enemy_zipline_color_b", "70", "Enemy zipline colour: blue", "")
		cv("Visibility", "citadel_mouse_cursor_scale", "1.2", "Bigger cursor", "")
		cv("Visibility", "r_light_sensitivity_mode", "true", "Light-sensitivity mode", "Fewer bright flashes.")
		cv("Visibility", "citadel_unit_status_show_ally_health_value", "true", "Ally health numbers", "")
		cv("Visibility", "citadel_unit_status_hero_name_mode", "1", "Hero names shown", "")
		cv("Visibility", "citadel_subtitles_ally_abilities", "true", "Subtitles: ally abilities", "")
		cv("Visibility", "citadel_subtitles_enemy_abilities", "true", "Subtitles: enemy abilities", "")
		cv("Visibility", "citadel_subtitles_enemy_kills", "true", "Subtitles: enemy kills", "")
		cv("Visibility", "citadel_subtitles_outline_thickness", "2", "Subtitle outline", "")
		cv("Visibility", "citadel_subtitles_newscaster", "false", "Newscaster subtitles off", "")
	}

	type bind struct{ action, key string }
	binds := []bind{
		{"AbilityMelee", "Q"}, {"Item1", "Z"}, {"Item2", "X"}, {"Item3", "C"}, {"Item4", "V"},
		{"HeldItem", "F"}, {"Interact", "E"}, {"Reload", "R"}, {"Mantle", "SPACE"}, {"Zipline", "SPACE"},
		{"Crouch", "CTRL"}, {"Roll", "SHIFT"}, {"Teleport", "G"}, {"PurchaseQuickbuy", "B"},
		{"Scoreboard", "TAB"}, {"OpenHeroSheet", "N"}, {"Ping", "MOUSE3"}, {"InstantPing", "MOUSE4"}, {"PushToTalk", "MOUSE5"},
		{"CancelAbility", "CAPSLOCK"}, {"AltCast", "ALT"}, {"Chat", "ENTER"}, {"ChatTeam", "Y"}, {"ExtraInfo", "H"},
		{"MinimapZoomIn", "["}, {"MinimapZoomOut", "]"}, {"ToggleFreeCursor", "F6"},
	}
	for _, k := range binds {
		b.add("Hotkeys", "keys", k.action+" = "+k.key, k.key, "", "KeyBindings", "Keys", k.action, "Key")
		b.add("Hotkeys", "keys", k.action+" second key cleared", "NONE", "", "KeyBindings", "Keys", k.action, "Key2")
	}
	for i := 1; i <= 4; i++ {
		b.add("Hotkeys", "keys", fmt.Sprintf("Ability %d quick cast", i), "1", "", "KeyBindings", "Keys", fmt.Sprintf("AbilityQuickCastMode%d", i), "Key")
	}
	b.add("Hotkeys", "keys", "Items quick cast", "1", "", "KeyBindings", "Keys", "ItemQuickCastMode", "Key")
	b.add("Hotkeys", "keys", "Alt cast: normal", "0", "", "KeyBindings", "Keys", "AltCastMode", "Key")
}

func cs2Preset(b *presetBuilder) {
	b.video("fullscreen", "1", "Fullscreen (exclusive)", "")
	b.video("nowindowborder", "1", "No window border", "")
	b.video("defaultres", "2560", "Width", "Untick width, height and aspect for 1920×1080 stretched.")
	b.video("defaultresheight", "1440", "Height", "")
	b.video("aspectratiomode", "1", "Aspect: 16:9", "")
	b.video("mat_vsync", "0", "V-sync off", "")
	b.video("r_low_latency", "1", "Low-latency mode", "")
	b.video("shaderquality", "0", "Shader quality low", "")
	b.video("msaa_samples", "4", "MSAA 4x", "Clean edges on a strong GPU.")
	b.video("r_csgo_cmaa_enable", "0", "CMAA off", "")
	b.video("videocfg_shadow_quality", "0", "Shadows low", "")
	b.video("videocfg_dynamic_shadows", "1", "Dynamic shadows: all", "Enemy shadows are information.")
	b.video("videocfg_texture_detail", "1", "Textures medium", "")
	b.video("videocfg_particle_detail", "0", "Particles low", "")
	b.video("videocfg_ao_detail", "0", "Ambient occlusion off", "")
	b.video("videocfg_hdr_detail", "-1", "HDR: performance", "")
	b.video("videocfg_fsr_detail", "0", "No upscaling", "")
	b.video("r_texturefilteringquality", "3", "Anisotropic filtering 8x", "")

	cv := func(group, name, value, label, note string) { b.convar("convars", group, name, value, label, note) }
	cv("Audio", "volume", "0.6", "Master volume 60%", "")
	cv("Audio", "snd_musicvolume", "0.05", "Music 5%", "")
	cv("Audio", "snd_menumusic_volume", "0", "Menu music off", "")
	cv("Audio", "snd_tensecondwarning_volume", "0.6", "Ten-second warning 60%", "")
	cv("Audio", "snd_mute_mvp_music_live_players", "true", "Mute MVP music while alive", "")
	cv("Audio", "snd_headphone_eq", "0", "Headphone EQ: natural", "")
	cv("Audio", "snd_mute_losefocus", "false", "Keep sound when alt-tabbed", "")
	cv("Gameplay", "cl_crosshairstyle", "4", "Crosshair: classic static", "")
	cv("Gameplay", "cl_crosshairsize", "2", "Crosshair size 2", "")
	cv("Gameplay", "cl_crosshairgap", "-2", "Crosshair gap -2", "")
	cv("Gameplay", "cl_crosshairthickness", "1", "Crosshair thickness 1", "")
	cv("Gameplay", "cl_crosshair_drawoutline", "1", "Crosshair outline", "")
	cv("Gameplay", "cl_crosshair_outlinethickness", "1", "Outline thickness 1", "")
	cv("Gameplay", "cl_crosshaircolor", "1", "Crosshair green", "")
	cv("Gameplay", "cl_crosshairdot", "false", "No centre dot", "")
	cv("Gameplay", "cl_crosshair_t", "false", "Not a T", "")
	cv("Gameplay", "viewmodel_fov", "68", "Viewmodel FOV 68", "")
	cv("Gameplay", "viewmodel_offset_x", "2.5", "Viewmodel X", "")
	cv("Gameplay", "viewmodel_offset_y", "0", "Viewmodel Y", "")
	cv("Gameplay", "viewmodel_offset_z", "-1.5", "Viewmodel Z", "")
	cv("Gameplay", "cl_autowepswitch", "0", "Don't auto-switch to picked-up weapons", "")
	cv("Gameplay", "cl_use_opens_buy_menu", "0", "E doesn't open the buy menu", "")
	cv("Gameplay", "fps_max", "400", "FPS cap 400", "")
	cv("Visibility", "cl_hud_color", "1", "HUD colour: white", "")
	cv("Visibility", "hud_scaling", "0.85", "HUD scale 0.85", "")
	cv("Visibility", "cl_showloadout", "1", "Always show loadout", "")
	cv("Visibility", "cl_teamid_overhead_mode", "2", "Teammate names and equipment overhead", "")
	cv("Visibility", "cl_radar_always_centered", "0", "Radar not centred", "More map shown.")
	cv("Visibility", "cl_radar_scale", "0.35", "Radar scale 0.35", "")
	cv("Visibility", "cl_radar_icon_scale_min", "0.8", "Radar icons 0.8", "")
	cv("Visibility", "cl_radar_rotate", "1", "Radar rotates", "")
	cv("Visibility", "cl_showfps", "0", "FPS counter off", "")
	cv("Visibility", "r_drawtracers_firstperson", "1", "Own tracers on", "")

	type bind struct{ key, cmd, label string }
	binds := []bind{
		{"MWHEELUP", "+jump", "Scroll up: jump"}, {"MWHEELDOWN", "+jump", "Scroll down: jump"}, {"SPACE", "+jump", "Space: jump"},
		{"CTRL", "+duck", "Ctrl: crouch"}, {"SHIFT", "+sprint", "Shift: walk"}, {"E", "+use", "E: use"}, {"G", "drop", "G: drop"},
		{"B", "buymenu", "B: buy menu"}, {"Q", "lastinv", "Q: last weapon"}, {"R", "+reload", "R: reload"},
		{"F", "use weapon_flashbang", "F: flashbang"}, {"C", "use weapon_smokegrenade", "C: smoke"},
		{"X", "use weapon_hegrenade", "X: HE grenade"}, {"Z", "use weapon_molotov; use weapon_incgrenade", "Z: molotov"},
		{"T", "use weapon_knife", "T: knife"}, {"V", "+voicerecord", "V: voice"}, {"MOUSE4", "player_ping", "Mouse 4: ping"},
		{"TAB", "+showscores", "Tab: scoreboard"}, {"`", "toggleconsole", "`: console"},
		{"N", "toggle cl_radar_scale 0.35 0.6", "N: radar zoom"},
	}
	for _, k := range binds {
		b.add("Hotkeys", "keys", k.label, k.cmd, "", "config", "bindings", k.key)
	}
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

func (a *App) PresetPreview(game string) (PresetPreview, error) {
	g, ok := presetGames[game]
	if !ok {
		return PresetPreview{}, errors.New("Unknown game.")
	}
	p := PresetPreview{Game: game, Reasons: []string{}, Lines: []PresetLine{}}
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
	for _, line := range presetLines(game, docs) {
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
	return p, nil
}

// kvSame treats the games' two spellings of a boolean, and numbers that
// only differ in formatting, as the same value.
func kvSame(a, b string) bool {
	if a == b {
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
func (a *App) PresetApply(game string, skip []string) (PresetPreview, error) {
	p, err := a.PresetPreview(game)
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
		if skipped[line.ID] || !line.Changed {
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
	return a.PresetPreview(game)
}

// PresetRestore puts the latest backup's files back. The game must be
// closed.
func (a *App) PresetRestore(game string) (PresetPreview, error) {
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
	return a.PresetPreview(game)
}

package core

// The preset tables. Every line names where its value comes from: a pro
// player's published settings (prosettings.net, gamingcfg.com, the
// hawk.live and winio.ai write-ups), a settings guide (prosettings.net,
// cyberscore.live, Dexerto, BLAST, favade.org), or "my pick" where the
// research found no pro data and the value is this app's own judgement.
// Setting names come from the player's own files where they exist.

import (
	"fmt"
	"sort"
	"strings"
)

// PresetLayout is one hotkey layout a game's Hotkeys page can apply.
type PresetLayout struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	By    string `json:"by"`
	Desc  string `json:"desc"`
}

var presetLayouts = map[string][]PresetLayout{
	"dota": {
		{"yatoro", "Yatoro", "Team Spirit carry, two-time TI winner", "Abilities Q W E D F R, items 1 2 3 4 Space C, hero on F1 and unit groups on F2–F4, quick cast on everything, camera grip on Mouse 3."},
		{"puppey", "Puppey", "Team Secret captain", "Abilities Z X C V B R, items Q W E Mouse 4 Space D, camera grip on Mouse 3."},
		{"micke", "miCKe", "Team Liquid carry", "Abilities Q W F D G R, items Alt+Q W E A S D, camera grip on Mouse 3."},
		{"standard", "Classic", "Dota's default rows (Topson's \"classic layout\")", "Abilities Q W E D F R, items Z X C V B N, quick cast only on Hex, Euls and Wind Waker, camera grip on Mouse 3."},
	},
	"deadlock": {
		{"lefaa", "Lefaa", "Top-ranked Deadlock player", "Abilities 1 2 3 4, items Z E Mouse 4 Mouse 5, melee Q, parry F, cancel Space, self-cast Mouse 3, dash Shift, crouch Ctrl."},
		{"mikaels", "MikaelS", "Virtus.pro", "Abilities 1 2 3 4, items Q X C E, melee Mouse 5, parry F, cancel Space, self-cast Mouse 3."},
		{"hydration", "Hydration", "Pulsar Esports", "Abilities 1 2 3 4, items Z X C V, melee Q, parry F, cancel Space, self-cast Mouse 3."},
	},
	"pubg": {
		{"standard", "Standard", "TGLTN, Kickstart", "PUBG's own movement keys, which the pros keep, with scope on hold."},
	},
	"cs2": {
		{"standard", "Standard", "Guide-based", "Each grenade on its own key, jump on the wheel, push to talk on V, ping on Mouse 4. Pros' own binds aren't published."},
	},
}

func presetLayout(game, id string) PresetLayout {
	list := presetLayouts[game]
	for _, l := range list {
		if l.ID == id {
			return l
		}
	}
	return list[0]
}

type presetBuilder struct {
	game  string
	lines []PresetLine
}

func (b *presetBuilder) add(group, file, label, value, source, note string, path ...string) {
	id := b.game + "." + file + "." + strings.Join(path, "/")
	b.lines = append(b.lines, PresetLine{ID: id, Group: group, Label: label, File: file, Path: path, Value: value, Source: source, Note: note})
}

func (b *presetBuilder) video(key, value, label, source, note string) {
	b.add("Video", "video", label, value, source, note, "video.cfg", "setting."+key)
}

func (b *presetBuilder) convar(file, group, name, value, label, source, note string) {
	b.add(group, file, label, value, source, note, "config", "convars", name)
}

// bind adds one hotkey line for an action.
func (b *presetBuilder) bind(section, action, label, value, source string, path ...string) {
	id := b.game + ".keys." + strings.Join(path, "/")
	b.lines = append(b.lines, PresetLine{ID: id, Group: "Hotkeys", Label: label, File: "keys", Path: path, Value: value, Action: action, Section: section, Source: source})
}

// info adds a row that is shown but never written: a setting the game
// only takes through its own menu.
func (b *presetBuilder) info(section, action, label, value, source, note string) {
	b.lines = append(b.lines, PresetLine{ID: b.game + ".info." + action, Group: "Hotkeys", Label: label, Value: value, Action: action, Section: section, Source: source, Note: note, Info: true})
}

// presetLines builds the lines for a game and hotkey layout; docs (by file
// kind) let a few lines follow what the player's file holds (Dota's
// per-item cast modes).
func presetLines(game string, docs map[string]presetDoc, layout string) []PresetLine {
	b := &presetBuilder{game: game}
	switch game {
	case "dota":
		keys, _ := docs["keys"].(*kvDoc)
		dotaPreset(b, keys, presetLayout(game, layout).ID)
	case "deadlock":
		deadlockPreset(b, presetLayout(game, layout).ID)
	case "cs2":
		cs2Preset(b)
	case "pubg":
		pubgPreset(b)
	}
	return b.lines
}

const (
	srcGuide  = "settings guides"
	srcMyPick = "my pick"
)

func dotaPreset(b *presetBuilder, keys *kvDoc, layout string) {
	pros := "42 of 43 pros"
	// Video: pros play borderless at native resolution and strip the
	// effects; a strong PC keeps textures and shadows (Yatoro).
	b.video("fullscreen", "0", "Borderless window", pros, "Every pro with a listed mode plays borderless, not exclusive fullscreen.")
	b.video("nowindowborder", "1", "No window border", pros, "")
	b.video("mat_vsync", "0", "V-sync off", srcGuide, "")
	b.video("r_low_latency", "1", "Low-latency mode", srcGuide, "NVIDIA Reflex / low latency on.")
	b.video("r_ssao", "false", "Ambient occlusion off", srcGuide, "prosettings.net: the single biggest frame cost, no competitive value.")
	b.video("r_dota_normal_maps", "false", "Normal maps off", srcGuide, "")
	b.video("r_deferred_additive_pass", "false", "Additive light pass off", srcGuide, "")
	b.video("r_deferred_specular_bloom", "false", "Specular bloom off", srcGuide, "")
	b.video("dota_cheap_water", "true", "Simple water", srcGuide, "")
	b.video("r_deferred_height_fog", "false", "Atmospheric fog off", srcGuide, "")
	b.video("r_dota_allow_wind_on_trees", "false", "Tree wind off", srcGuide, "")
	b.video("r_dota_fxaa", "false", "Anti-aliasing off", srcGuide, "")
	b.video("dota_portrait_animate", "false", "Animated portrait off", srcGuide, "")
	b.video("dota_ambient_creatures", "false", "Ambient creatures off", srcGuide, "")
	b.video("dota_ambient_cloth", "false", "Cloth simulation off", srcGuide, "")
	b.video("r_depth_of_field", "0", "Depth of field off", srcGuide, "")
	b.video("r_grass_quality", "0", "Grass off", srcMyPick, "Guides call grass optional; off keeps the ground readable.")
	b.video("shaderquality", "1", "Texture quality high", "Yatoro, guides", "Yatoro keeps textures and shadows high on a strong PC.")
	b.video("cl_globallight_shadow_mode", "1", "Shadows medium", srcGuide, "")
	b.video("mat_viewportscale", "1", "Render at 100%", srcGuide, "")
	b.video("r_dota_fsr_upsample", "0", "No upscaling", srcGuide, "")
	b.video("r_texture_stream_mip_bias", "0", "Full texture detail", srcGuide, "")

	cv := func(group, name, value, label, source, note string) {
		b.convar("convars", group, name, value, label, source, note)
	}
	// Audio.
	cv("Audio", "snd_musicvolume", "0", "Music off", srcGuide, "prosettings.net: music all the way down.")
	cv("Audio", "snd_gamevoicevolume", "0.7", "Hero voices 70%", srcGuide, "Voice and unit speech slightly lowered.")
	cv("Audio", "dota_force_default_death_stinger", "true", "Default death sound", srcMyPick, "")
	cv("Audio", "dota_force_default_respawn_stinger", "true", "Default respawn sound", srcMyPick, "")
	// Gameplay: camera and orders the way the pros set them.
	cv("Gameplay", "dota_camera_speed", "6000", "Camera speed 6000", "Yatoro 6193, Miracle 7572, miCKe 4014", "Pros run the camera far faster than the 3000 default.")
	cv("Gameplay", "dota_camera_disable_zoom", "true", "Camera zoom disabled", "Yatoro", "")
	cv("Gameplay", "dota_screen_shake", "false", "Screen shake off", "Yatoro, guides", "")
	cv("Gameplay", "dota_reset_camera_on_spawn", "false", "Don't centre camera on respawn", srcGuide, "esports.gg: saves TPs wasted by the camera jump.")
	cv("Gameplay", "dota_camera_deatheffect", "false", "Death camera effect off", srcGuide, "cyberscore: no colour shift while dead.")
	cv("Gameplay", "dota_leftclick_cameragrip_new", "false", "Camera grip on its own key", srcGuide, "")
	cv("Gameplay", "dota_player_teleport_requires_halt", "true", "Teleport requires Stop", srcGuide, "Moving or casting won't cancel a TP.")
	cv("Gameplay", "dota_player_channels_require_stop", "true", "Channelled abilities require Stop", srcGuide, "")
	cv("Gameplay", "dota_player_interaction_channels_require_stop", "true", "Interactions require Stop", srcGuide, "")
	cv("Gameplay", "dota_item_double_tap_self_cast", "true", "Double-tap to self-cast", srcGuide, "")
	cv("Gameplay", "dota_smart_doubletap", "true", "Smart double-tap (Alt+key)", srcGuide, "")
	cv("Gameplay", "dota_player_units_auto_attack_mode", "1", "Auto-attack: standard", srcGuide, "esports.gg: Standard or Never.")
	cv("Gameplay", "dota_disable_autoattack_during_stop", "true", "No auto-attack while stopped", srcMyPick, "")
	cv("Gameplay", "dota_force_right_click_attack", "1", "Right-click to force attack", srcGuide, "")
	cv("Gameplay", "dota_settings_quick_move", "false", "Quick move off", srcGuide, "cyberscore: off, like most pros.")
	cv("Gameplay", "dota_settings_quick_target_attack", "false", "Quick attack off", srcGuide, "")
	cv("Gameplay", "dota_player_add_summoned_to_selection", "false", "Don't auto-select summoned units", srcGuide, "")
	cv("Gameplay", "dota_enable_range_finder", "true", "Range finder on", srcMyPick, "")
	cv("Gameplay", "dota_shop_force_hotkeys", "true", "Shop hotkeys always", srcMyPick, "")
	cv("Gameplay", "dota_quickbuy_rightclick_marksforbuy", "true", "Right-click marks quick-buy", srcMyPick, "")
	cv("Gameplay", "dota_courier_autodeliver", "false", "Courier delivers on your call", srcMyPick, "")
	// Visibility.
	cv("Visibility", "dota_hud_extra_large_minimap", "1", "Extra-large minimap", "Yatoro, guides", "")
	cv("Visibility", "dota_minimap_simple_background", "true", "Simple minimap background", "Yatoro", "")
	cv("Visibility", "dota_minimap_always_draw_hero_icons", "false", "Hero icons on the minimap only while Alt is held", "Yatoro", "Dots normally, icons on Alt: less clutter, same information.")
	cv("Visibility", "dota_minimap_simple_colors", "true", "Simple minimap colours", srcMyPick, "Green allies, red enemies.")
	cv("Visibility", "dota_minimap_hero_size", "900", "Big minimap icons", "pro autoexec", "A shared pro autoexec uses 1000; the default is 600.")
	cv("Visibility", "dota_alt_shows_tower_attack_range", "true", "Alt shows tower range", "Yatoro", "")
	cv("Visibility", "dota_hud_healthbar_local_player_high_visibility", "true", "Your health bar stands out", srcMyPick, "")
	cv("Visibility", "dota_hud_disable_damage_numbers", "false", "Damage numbers on", srcMyPick, "")
	cv("Visibility", "dota_hud_contextual_tips_disable", "true", "No contextual tips", srcMyPick, "")
	cv("Visibility", "dota_help_tips_enabled", "false", "No help tips", srcMyPick, "")
	cv("Visibility", "dota_hud_netgraph", "false", "Net graph off", srcMyPick, "The app's Connection check covers it.")

	// Hotkeys: a named pro layout.
	type bind struct{ section, action, label, key, mod, mode string }
	var binds []bind
	src := presetLayout("dota", layout).Label
	abilities := func(keys [6]string, mode string) {
		names := [6]string{"AbilityPrimary1", "AbilityPrimary2", "AbilityPrimary3", "AbilitySecondary1", "AbilitySecondary2", "AbilityUltimate"}
		labels := [6]string{"Ability 1", "Ability 2", "Ability 3", "Ability 4", "Ability 5", "Ultimate"}
		for i := range names {
			binds = append(binds, bind{"Abilities", names[i], labels[i], keys[i], "", mode})
		}
	}
	items := func(keys [6]string, mod, mode string) {
		for i := range keys {
			binds = append(binds, bind{"Items", fmt.Sprintf("Inventory%d", i+1), fmt.Sprintf("Item %d", i+1), keys[i], mod, mode})
		}
	}
	quick := "" // per-item cast mode policy: "" leaves the player's, "all" quick, "aimed" only the Hex/Euls family
	switch layout {
	case "yatoro":
		abilities([6]string{"Q", "W", "E", "D", "F", "R"}, "1")
		items([6]string{"1", "2", "3", "4", "SPACE", "C"}, "", "1")
		quick = "all"
		binds = append(binds,
			bind{"Units and selection", "HeroSelect", "Select hero", "F1", "", ""},
			bind{"Units and selection", "ControlGroup1", "Control group 1", "F2", "", ""},
			bind{"Units and selection", "ControlGroup2", "Control group 2", "F3", "", ""},
			bind{"Units and selection", "ControlGroup3", "Control group 3", "F4", "", ""},
			bind{"Units and selection", "ControlGroup4", "Control group 4", "F5", "", ""})
	case "puppey":
		abilities([6]string{"Z", "X", "C", "V", "B", "R"}, "")
		items([6]string{"Q", "W", "E", "MOUSE4", "SPACE", "D"}, "", "")
	case "micke":
		abilities([6]string{"Q", "W", "F", "D", "G", "R"}, "")
		items([6]string{"Q", "W", "E", "A", "S", "D"}, "ALT", "")
	default:
		abilities([6]string{"Q", "W", "E", "D", "F", "R"}, "0")
		items([6]string{"Z", "X", "C", "V", "B", "N"}, "", "0")
		quick = "aimed"
	}
	binds = append(binds, bind{"Hero orders", "CameraGrip", "Camera grip", "MOUSE3", "", ""})
	for _, k := range binds {
		source := src
		switch {
		case k.action == "CameraGrip":
			source = srcGuide
		case strings.HasPrefix(k.action, "ControlGroup"):
			source = srcMyPick
		}
		b.bind(k.section, k.action, k.label, k.key, source, "KeyBindings", "Keys", k.action, "Key")
		if k.mod != "" {
			b.bind(k.section, k.action, k.label+" modifier", k.mod, source, "KeyBindings", "Keys", k.action, "Modifier")
		} else if keys != nil {
			if _, has := keys.Get("KeyBindings", "Keys", k.action, "Modifier"); has {
				b.bind(k.section, k.action, k.label+" modifier removed", "", source, "KeyBindings", "Keys", k.action, "Modifier")
				b.lines[len(b.lines)-1].Remove = true
			}
		}
		if k.mode != "" {
			b.bind(k.section, k.action, k.label+" quick cast", k.mode, source, "KeyBindings", "Keys", k.action, "Mode")
		}
	}
	// Per-item cast modes. Yatoro: quick cast on everything. Classic: only
	// the targeted items the guides name (Hex, Euls, Wind Waker).
	if quick != "" && keys != nil {
		aimed := map[string]bool{"item_sheepstick": true, "item_cyclone": true, "item_wind_waker": true}
		var items []string
		for _, n := range keys.Block("KeyBindings", "Items") {
			if strings.HasPrefix(n.Key, "item_") {
				items = append(items, n.Key)
			}
		}
		sort.Strings(items)
		for _, it := range items {
			mode, label, source := "1", "quick cast", "Yatoro"
			if quick == "aimed" {
				source = srcGuide
				if !aimed[it] {
					mode, label = "0", "normal cast"
				}
			}
			name := strings.ReplaceAll(strings.TrimPrefix(it, "item_"), "_", " ")
			b.bind("Item cast modes", it, strings.ToUpper(name[:1])+name[1:]+": "+label, mode, source, "KeyBindings", "Items", it, "Mode")
		}
	}
}

func deadlockPreset(b *presetBuilder, layout string) {
	pros := "MikaelS, Lefaa"
	b.video("fullscreen", "1", "Fullscreen", "5 of 5 pros", "Every listed pro plays Full Screen.")
	b.video("nowindowborder", "1", "No window border", "5 of 5 pros", "")
	b.video("mat_vsync", "0", "V-sync off", pros, "")
	b.video("r_low_latency", "1", "Low-latency mode", srcMyPick, "")
	b.video("r_citadel_antialiasing", "0", "Anti-aliasing off", pros, "")
	b.video("r_citadel_ssao_quality", "0", "Screen-space AO off", pros, "")
	b.video("r_citadel_distancefield_ao_quality", "0", "Distance-field AO off", pros, "")
	b.video("r_citadel_motion_blur", "0", "Motion blur off", pros, "")
	b.video("r_effects_bloom", "false", "Effects bloom off", pros, "")
	b.video("r_post_bloom", "false", "Post-process bloom off", pros, "")
	b.video("r_arealights", "false", "Area lights off", pros, "")
	b.video("r_depth_of_field", "false", "Depth of field off", pros, "")
	b.video("r_citadel_fog_quality", "0", "Fog low", srcGuide, "prosettings.net: higher fog hides things.")
	b.video("r_citadel_shadow_quality", "1", "Shadows medium", srcGuide, "prosettings.net: shadows help locate enemies; MikaelS and Lefaa run them off for frames.")
	b.video("r_citadel_upscaling", "0", "No upscaling", srcMyPick, "Pros render at 50–67% for frames; native is sharper.")
	b.video("mat_viewportscale", "1", "Render at 100%", srcMyPick, "")
	b.video("r_texture_stream_mip_bias", "0", "Full texture detail", srcMyPick, "MikaelS and Lefaa use low textures for frames.")
	b.video("r_light_sensitivity_mode", "true", "Reduce flashing effects", "Lefaa, guides", "")
	b.video("shaderquality", "1", "Shader quality high", srcMyPick, "")

	for _, file := range []string{"convars", "cloudconvars"} {
		cv := func(group, name, value, label, source, note string) {
			b.convar(file, group, name, value, label, source, note)
		}
		cv("Audio", "snd_musicvolume", "0", "Music off", srcMyPick, "")
		cv("Audio", "snd_gamevoicevolume", "0.7", "Hero voices 70%", srcMyPick, "")
		cv("Gameplay", "citadel_camera_hero_fov", "90", "Field of view 90", "prosettings.net guide", "The maximum; every guide says 90.")
		cv("Gameplay", "zoom_sensitivity_ratio", "1", "Zoom sensitivity 1", "3 of 5 pros, guides", "")
		cv("Gameplay", "sensitivity_y_scale", "1", "Vertical scaling 1", "5 of 5 pros", "")
		cv("Gameplay", "cl_citadel_zoom_is_toggle", "false", "Hold to zoom", srcGuide, "BLAST: keeps the field of view wide.")
		cv("Gameplay", "option_duck_method", "false", "Hold to crouch", "5 of 5 pros", "Every listed pro keeps crouch on Ctrl, held.")
		cv("Gameplay", "citadel_reduce_camera_shake", "true", "Reduced camera shake", srcGuide, "")
		cv("Gameplay", "cl_citadel_auto_fire", "false", "Auto-fire off", srcMyPick, "")
		cv("Gameplay", "cl_citadel_auto_mantle_forward", "true", "Auto-mantle forward", srcMyPick, "")
		cv("Gameplay", "cl_citadel_auto_mantle_in_air", "true", "Auto-mantle in the air", srcMyPick, "")
		cv("Visibility", "citadel_custom_ui_colors", "true", "Custom UI colours on", srcMyPick, "")
		cv("Visibility", "citadel_enemy_ui_color_r", "255", "Enemy colour: red", srcMyPick, "")
		cv("Visibility", "citadel_enemy_ui_color_g", "70", "Enemy colour: green", srcMyPick, "")
		cv("Visibility", "citadel_enemy_ui_color_b", "70", "Enemy colour: blue", srcMyPick, "")
		cv("Visibility", "citadel_enemy_zipline_color_r", "255", "Enemy zipline colour: red", srcMyPick, "")
		cv("Visibility", "citadel_enemy_zipline_color_g", "70", "Enemy zipline colour: green", srcMyPick, "")
		cv("Visibility", "citadel_enemy_zipline_color_b", "70", "Enemy zipline colour: blue", srcMyPick, "")
		cv("Visibility", "citadel_mouse_cursor_scale", "1.2", "Bigger cursor", srcMyPick, "")
		cv("Visibility", "r_light_sensitivity_mode", "true", "Light-sensitivity mode", "Lefaa, guides", "")
		cv("Visibility", "citadel_unit_status_show_ally_health_value", "true", "Ally health numbers", srcMyPick, "")
		cv("Visibility", "citadel_unit_status_hero_name_mode", "1", "Hero names shown", srcMyPick, "")
		cv("Visibility", "citadel_subtitles_enemy_abilities", "true", "Subtitles: enemy abilities", srcMyPick, "")
		cv("Visibility", "citadel_subtitles_enemy_kills", "true", "Subtitles: enemy kills", srcMyPick, "")
	}

	type bind struct{ section, action, label, key string }
	src := presetLayout("deadlock", layout).Label
	all := "5 of 5 pros"
	binds := []bind{
		{"Abilities and items", "Ability1", "Ability 1", "1"}, {"Abilities and items", "Ability2", "Ability 2", "2"},
		{"Abilities and items", "Ability3", "Ability 3", "3"}, {"Abilities and items", "Ability4", "Ability 4", "4"},
		{"Abilities and items", "HeldItem", "Parry / throw held item", "F"}, {"Abilities and items", "CancelAbility", "Cancel ability", "SPACE"},
		{"Abilities and items", "AltCast", "Self-cast", "MOUSE3"},
		{"Movement", "Mantle", "Jump / mantle", "SPACE"}, {"Movement", "Roll", "Dash", "SHIFT"}, {"Movement", "Crouch", "Crouch / slide", "CTRL"},
	}
	own := map[string]bool{}
	switch layout {
	case "mikaels":
		binds = append(binds, bind{"Abilities and items", "Item1", "Item 1", "Q"}, bind{"Abilities and items", "Item2", "Item 2", "X"},
			bind{"Abilities and items", "Item3", "Item 3", "C"}, bind{"Abilities and items", "Item4", "Item 4", "E"}, bind{"Abilities and items", "AbilityMelee", "Melee", "MOUSE5"})
	case "hydration":
		binds = append(binds, bind{"Abilities and items", "Item1", "Item 1", "Z"}, bind{"Abilities and items", "Item2", "Item 2", "X"},
			bind{"Abilities and items", "Item3", "Item 3", "C"}, bind{"Abilities and items", "Item4", "Item 4", "V"}, bind{"Abilities and items", "AbilityMelee", "Melee", "Q"})
	default:
		binds = append(binds, bind{"Abilities and items", "Item1", "Item 1", "Z"}, bind{"Abilities and items", "Item2", "Item 2", "E"},
			bind{"Abilities and items", "Item3", "Item 3", "MOUSE4"}, bind{"Abilities and items", "Item4", "Item 4", "MOUSE5"}, bind{"Abilities and items", "AbilityMelee", "Melee", "Q"})
	}
	for _, k := range binds[len(binds)-5:] {
		own[k.action] = true
	}
	for _, k := range binds {
		source := all
		if own[k.action] {
			source = src
		}
		b.bind(k.section, k.action, k.label, k.key, source, "KeyBindings", "Keys", k.action, "Key")
		b.bind(k.section, k.action, k.label+" second key cleared", "NONE", source, "KeyBindings", "Keys", k.action, "Key2")
	}
	for i := 1; i <= 4; i++ {
		b.bind("Cast modes", fmt.Sprintf("AbilityQuickCastMode%d", i), fmt.Sprintf("Ability %d: quick cast", i), "1", "Dexerto, BLAST", "KeyBindings", "Keys", fmt.Sprintf("AbilityQuickCastMode%d", i), "Key")
	}
	b.bind("Cast modes", "ItemQuickCastMode", "Items: quick cast", "1", "Dexerto, BLAST", "KeyBindings", "Keys", "ItemQuickCastMode", "Key")
	b.info("Crosshair", "reticle", "Reticle (set in Settings › Game › Reticle)", "MikaelS: gap 1, width 3, height 5, pip border on, no dot, green 0 255 0", "MikaelS",
		"Deadlock stores the reticle as a code pasted in its own menu, so the app can't write it. Pros use a small static cross in green, cyan or white with little or no dot (Recrent: gap -2, width 1, height 4, green).")
}

func cs2Preset(b *presetBuilder) {
	six := "5 of 6 pros"
	b.video("fullscreen", "1", "Fullscreen", "6 of 6 pros", "")
	b.video("nowindowborder", "1", "No window border", "6 of 6 pros", "")
	b.video("defaultres", "2560", "Width 2560", "ropz", "ropz plays native 16:9; 64% of pros play 1280×960 stretched for bigger models.")
	b.video("defaultresheight", "1440", "Height 1440", "ropz", "")
	b.video("aspectratiomode", "1", "Aspect 16:9", "ropz", "")
	b.video("mat_vsync", "0", "V-sync off", "6 of 6 pros", "")
	b.video("r_low_latency", "1", "Low-latency mode", "ZywOo, ropz", "")
	b.video("msaa_samples", "2", "MSAA 2x", "ropz", "At native 16:9 ropz runs 2x; the 4:3 players run 8x.")
	b.video("r_csgo_cmaa_enable", "0", "CMAA off", six, "")
	b.video("videocfg_shadow_quality", "2", "Global shadows high", six, "Enemy shadows show round corners before the player does.")
	b.video("videocfg_dynamic_shadows", "1", "Dynamic shadows: all", "6 of 6 pros", "")
	b.video("videocfg_texture_detail", "0", "Model / texture detail low", six, "")
	b.video("r_texturefilteringquality", "0", "Texture filtering bilinear", "4 of 6 pros", "")
	b.video("shaderquality", "0", "Shader detail low", six, "")
	b.video("videocfg_particle_detail", "0", "Particle detail low", six, "Smokes and fire are easier to see through.")
	b.video("videocfg_ao_detail", "0", "Ambient occlusion off", "4 of 6 pros", "")
	b.video("videocfg_hdr_detail", "1", "HDR quality", "6 of 6 pros", "")
	b.video("videocfg_fsr_detail", "0", "FSR off", "6 of 6 pros", "")

	cv := func(group, name, value, label, source, note string) {
		b.convar("convars", group, name, value, label, source, note)
	}
	guide := "prosettings.net guide"
	cv("Audio", "snd_musicvolume", "0", "Music off", guide, "")
	cv("Audio", "snd_menumusic_volume", "0.1", "Menu music 10%", guide, "")
	cv("Audio", "snd_roundstart_volume", "0", "Round start music off", guide, "")
	cv("Audio", "snd_roundaction_volume", "0", "Round action music off", guide, "")
	cv("Audio", "snd_roundend_volume", "0", "Round end music off", guide, "")
	cv("Audio", "snd_mvp_volume", "0", "MVP music off", guide, "")
	cv("Audio", "snd_mapobjective_volume", "0", "Bomb / hostage music off", guide, "")
	cv("Audio", "snd_deathcamera_volume", "0", "Death camera music off", guide, "")
	cv("Audio", "snd_tensecondwarning_volume", "0.1", "Ten-second warning 10%", guide, "")
	cv("Audio", "snd_mute_mvp_music_live_players", "true", "Mute MVP music while players are alive", guide, "")
	cv("Audio", "snd_headphone_eq", "1", "Headphone EQ: crisp", guide, "")
	cv("Audio", "snd_mute_losefocus", "true", "No audio in the background", guide, "")
	cv("Gameplay", "viewmodel_fov", "68", "Viewmodel FOV 68", "6 of 6 pros", "")
	cv("Gameplay", "viewmodel_offset_x", "2.5", "Viewmodel X 2.5", six, "")
	cv("Gameplay", "viewmodel_offset_y", "0", "Viewmodel Y 0", six, "")
	cv("Gameplay", "viewmodel_offset_z", "-1.5", "Viewmodel Z -1.5", "6 of 6 pros", "")
	cv("Gameplay", "viewmodel_presetpos", "2", "Viewmodel preset 2", "4 of 6 pros", "")
	cv("Gameplay", "cl_crosshairstyle", "4", "Crosshair: classic static", "6 of 6 pros", "")
	cv("Gameplay", "cl_crosshairsize", "2", "Crosshair length 2", "ZywOo, donk, m0NESY, NiKo", "")
	cv("Gameplay", "cl_crosshairthickness", "2", "Crosshair thickness 2", "ZywOo, donk, m0NESY", "")
	cv("Gameplay", "cl_crosshairgap", "1", "Crosshair gap 1", "donk, m0NESY, NiKo", "")
	cv("Gameplay", "cl_crosshairdot", "false", "No centre dot", "6 of 6 pros", "")
	cv("Gameplay", "cl_crosshair_t", "false", "Not a T", "6 of 6 pros", "")
	cv("Gameplay", "cl_crosshair_drawoutline", "false", "No outline", "6 of 6 pros", "")
	cv("Gameplay", "cl_crosshaircolor", "5", "Crosshair colour: custom", "donk", "")
	cv("Gameplay", "cl_crosshaircolor_r", "11", "Crosshair red 11", "donk", "")
	cv("Gameplay", "cl_crosshaircolor_g", "255", "Crosshair green 255", "donk", "")
	cv("Gameplay", "cl_crosshaircolor_b", "0", "Crosshair blue 0", "donk", "")
	cv("Gameplay", "cl_crosshairalpha", "255", "Crosshair opaque", "6 of 6 pros", "")
	cv("Gameplay", "cl_autowepswitch", "0", "Don't auto-switch to picked-up weapons", srcMyPick, "")
	cv("Gameplay", "cl_use_opens_buy_menu", "0", "E doesn't open the buy menu", srcMyPick, "")
	cv("Gameplay", "cl_showloadout", "1", "Always show loadout", srcMyPick, "")
	cv("Gameplay", "fps_max", "400", "FPS cap 400", "ZywOo, NiKo", "donk 600, m0NESY and ropz 999.")
	cv("Visibility", "cl_radar_always_centered", "1", "Radar centred on you", "5 of 5 pros", "Every listed pro keeps the radar centred; guides say otherwise.")
	cv("Visibility", "cl_radar_rotate", "1", "Radar rotates", "5 of 5 pros", "")
	cv("Visibility", "cl_hud_radar_scale", "1", "Radar size 1", "4 of 5 pros", "")
	cv("Visibility", "cl_radar_scale", "0.4", "Radar zoom 0.4", "ZywOo, m0NESY", "NiKo 0.35, ropz 0.65, donk 0.7.")
	cv("Visibility", "cl_radar_square_with_scoreboard", "1", "Square radar with the scoreboard", "4 of 5 pros", "")
	cv("Visibility", "cl_radar_icon_scale_min", "0.7", "Radar icons 0.7", "favade.org", "")
	cv("Visibility", "hud_scaling", "1", "HUD scale 1", "4 of 5 pros", "NiKo 1.1.")

	// CS2 binds go from key to command, so the key is the action. Pros'
	// own binds aren't published; these follow the guide's rules.
	type bind struct{ section, key, cmd, label string }
	binds := []bind{
		{"Movement", "MWHEELUP", "+jump", "Jump (scroll up)"}, {"Movement", "MWHEELDOWN", "+jump", "Jump (scroll down)"}, {"Movement", "SPACE", "+jump", "Jump"},
		{"Movement", "CTRL", "+duck", "Crouch"}, {"Movement", "SHIFT", "+sprint", "Walk"}, {"Movement", "E", "+use", "Use"},
		{"Weapons and grenades", "Q", "lastinv", "Last weapon"}, {"Weapons and grenades", "R", "+reload", "Reload"}, {"Weapons and grenades", "G", "drop", "Drop"}, {"Weapons and grenades", "T", "use weapon_knife", "Knife"},
		{"Weapons and grenades", "F", "use weapon_flashbang", "Flashbang"}, {"Weapons and grenades", "C", "use weapon_smokegrenade", "Smoke"},
		{"Weapons and grenades", "X", "use weapon_hegrenade", "HE grenade"}, {"Weapons and grenades", "Z", "use weapon_molotov; use weapon_incgrenade", "Molotov / incendiary"},
		{"Buying and info", "B", "buymenu", "Buy menu"}, {"Buying and info", "TAB", "+showscores", "Scoreboard"}, {"Buying and info", "N", "toggle cl_radar_scale 0.4 0.7", "Radar zoom"},
		{"Voice and pings", "V", "+voicerecord", "Push to talk"}, {"Voice and pings", "MOUSE4", "player_ping", "Ping"},
		{"Other", "`", "toggleconsole", "Console"},
	}
	for _, k := range binds {
		source := srcMyPick
		if strings.HasPrefix(k.cmd, "use weapon_") || k.cmd == "+jump" {
			source = "prosettings.net guide"
		}
		b.bind(k.section, k.key, k.label, k.cmd, source, "config", "bindings", k.key)
	}
}

// PUBG keeps everything in one Unreal .ini. Values follow the game's own
// spelling (6 decimals for floats); quality levels run 0 (Very Low) to 4
// (Ultra). Sources: prosettings.net's PUBG guide (68 pros), its player
// pages (TGLTN, Kickstart), and vortexgaming.io's pro settings round-up.
func pubgPreset(b *presetBuilder) {
	const tsl, sg = "/Script/TslGame.TslGameUserSettings", "ScalabilityGroups"
	set := func(group, section, key, value, label, source, note string) {
		b.add(group, "settings", label, value, source, note, section, key)
	}
	pros := "TGLTN, Kickstart"
	set("Video", tsl, "FullscreenMode", "0", "Fullscreen", pros+", guide", "Lowest input lag; borderless is 1.")
	set("Video", tsl, "PreferredFullscreenMode", "0", "Fullscreen (preferred)", pros, "")
	set("Video", tsl, "LastConfirmedFullscreenMode", "0", "Fullscreen (confirmed)", pros, "")
	set("Video", tsl, "ScreenScale", "100.000000", "Render scale 100", pros+", guide", "Sharp at native resolution.")
	set("Video", sg, "sg.ResolutionQuality", "100.000000", "Resolution quality 100", pros, "")
	set("Video", tsl, "FpsCameraFov", "95.000000", "First-person FOV 95", "21 of 48 pros listing FOV", "The most common pro FOV; 103 is the widest and the guide's pick.")
	set("Video", sg, "sg.AntiAliasingQuality", "0", "Anti-aliasing very low", pros, "The guide prefers medium to cut shimmer on wires and fences.")
	set("Video", sg, "sg.PostProcessQuality", "0", "Post-processing very low", pros+", guides", "Higher adds shadows and blur that hide players indoors.")
	set("Video", sg, "sg.ShadowQuality", "0", "Shadows very low", pros+", guide", "Players' own shadows still render.")
	set("Video", sg, "sg.TextureQuality", "2", "Textures medium", "Kickstart, guides", "Clearer camouflaged items; TGLTN plays very low.")
	set("Video", sg, "sg.EffectsQuality", "0", "Effects very low", pros+", guide", "")
	set("Video", sg, "sg.FoliageQuality", "0", "Foliage very low", pros+", guide", "Less grass to hide in.")
	set("Video", sg, "sg.ViewDistanceQuality", "1", "View distance low", "prosettings.net guide", "TGLTN very low, Kickstart medium.")
	set("Video", tsl, "bSharpen", "True", "Sharpen", "Kickstart, vortexgaming.io", "Sharper silhouettes at range; TGLTN leaves it off.")
	set("Video", tsl, "bUseVSync", "False", "V-sync", pros+", guide", "")
	set("Video", tsl, "bMotionBlur", "False", "Motion blur", pros+", guide", "")
	set("Video", tsl, "bUseInGameSmoothedFrameRate", "False", "Smoothed frame rate", "prosettings.net guide", "It adds input lag.")
	set("Audio", tsl, "bIsRenewalWeaponSoundEnabled", "False", "Remastered gun sounds", "vortexgaming.io", "Off keeps the legacy sounds pros grew up hearing direction in.")
	set("Audio", tsl, "bIsEnabledHrtfRemoteWeaponSound", "True", "HRTF for distant gunfire", "vortexgaming.io", "")
	set("Audio", tsl, "bIsBGMSoundMute", "True", "Mute lobby music", srcMyPick, "")
	set("Gameplay", tsl, "InputModeADS", "Hold", "Scope: hold", "vortexgaming.io", "Let go to leave the scope.")
	set("Gameplay", tsl, "bAutoEquipAttachmentByInteraction", "True", "Auto-equip attachments on pickup", "vortexgaming.io", "")
	set("Gameplay", tsl, "bAutoEquipAttachmentFromInventory", "True", "Auto-equip attachments from inventory", "vortexgaming.io", "")
	set("Gameplay", tsl, "bAutoReplaceAttachment", "True", "Auto-replace attachments", "vortexgaming.io", "")
	set("Gameplay", tsl, "bAutoWeaponSkinReplacement", "False", "Automatic weapon skins", "vortexgaming.io", "")
	set("Gameplay", tsl, "bUseBulletRecommendOption", "False", "Ammo recommendations", "vortexgaming.io", "")
	set("Gameplay", tsl, "bUseEquipmentRecommendOption", "False", "Equipment recommendations", "vortexgaming.io", "")
	set("Gameplay", tsl, "bUseAttachmentRecommendOption", "False", "Attachment recommendations", "vortexgaming.io", "")
	set("Visibility", tsl, "KillMessageListSizeLimit", "10", "Kill feed shows 10", "vortexgaming.io", "")
	set("Visibility", tsl, "bEnableUniversalGamma", "True", "One brightness for every map", "prosettings.net guide", "")
	// Keys: PUBG writes only the keys a player changed, and the pros keep
	// its defaults, so these are shown, not written.
	b.info("Movement and combat", "pubg_keys", "Movement and combat keys", "Crouch C, prone Z, jump Space, walk L-Ctrl, sprint L-Shift, peek Q / E, interact F, auto-run =", pros,
		"PUBG's own defaults, which TGLTN and Kickstart keep. If you changed one, set it back under Settings, Key Bindings in the game.")
}

//! Hero/item display names, CDN icon URLs, and the key-item whitelist.
//! Ported 1:1 from the original tracker's constants.

pub const HERO_CDN: &str = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/";
pub const ITEM_CDN: &str = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/";

pub const KEY_ITEMS: &[&str] = &[
    "boots", "boots_of_elves", "phase_boots", "power_treads", "arcane_boots",
    "tranquil_boots", "travel_boots", "travel_boots_2", "guardian_greaves",
    "blink", "overwhelming_blink", "swift_blink", "arcane_blink",
    "black_king_bar", "aghanims_scepter", "ultimate_scepter", "aghanims_shard",
    "refresher", "refresher_shard", "heart", "assault", "shivas_guard",
    "satanic", "skadi", "manta", "sange_and_yasha", "yasha_and_kaya",
    "daedalus", "butterfly", "silver_edge", "nullifier", "abyssal_blade",
    "monkey_king_bar", "linkens_sphere", "sheepstick", "rod_of_atos",
    "eye_of_skadi", "octarine_core", "bloodthorn", "dagon", "dagon_5",
];

pub fn is_key_item(name: &str) -> bool {
    KEY_ITEMS.contains(&name)
}

fn hero_name_override(clean: &str) -> Option<&'static str> {
    Some(match clean {
        "antimage" => "Anti-Mage",
        "nevermore" => "Shadow Fiend",
        "windrunner" => "Windranger",
        "vengefulspirit" => "Vengeful Spirit",
        "queenofpain" => "Queen of Pain",
        "skeleton_king" => "Wraith King",
        "doom_bringer" => "Doom",
        "necrolyte" => "Necrophos",
        "furion" => "Nature's Prophet",
        "life_stealer" => "Lifestealer",
        "rattletrap" => "Clockwerk",
        "obsidian_destroyer" => "Outworld Destroyer",
        "treant" => "Treant Protector",
        "wisp" => "Io",
        "zuus" => "Zeus",
        "shredder" => "Timbersaw",
        "magnataur" => "Magnus",
        "centaur" => "Centaur Warrunner",
        "abyssal_underlord" => "Underlord",
        "keeper_of_the_light" => "Keeper of the Light",
        _ => return None,
    })
}

/// Strips the `npc_dota_hero_` prefix, e.g. `npc_dota_hero_antimage` -> `antimage`.
pub fn hero_clean_name(raw: Option<&str>) -> Option<String> {
    let raw = raw?;
    let clean = raw.strip_prefix("npc_dota_hero_").unwrap_or(raw);
    if clean.is_empty() { None } else { Some(clean.to_string()) }
}

fn title_case(clean: &str) -> String {
    clean
        .split('_')
        .map(|w| {
            let mut c = w.chars();
            match c.next() {
                Some(first) => first.to_uppercase().collect::<String>() + c.as_str(),
                None => String::new(),
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

/// Human-readable hero name, e.g. `npc_dota_hero_antimage` -> `Anti-Mage`.
pub fn hero_display_name(raw: Option<&str>) -> String {
    match hero_clean_name(raw) {
        None => "Unknown Hero".to_string(),
        Some(clean) => match hero_name_override(&clean) {
            Some(name) => name.to_string(),
            None => title_case(&clean),
        },
    }
}

pub fn roshan_drops(death_count: u32) -> &'static str {
    if death_count <= 1 {
        "Aegis of the Immortal"
    } else if death_count == 2 {
        "Aegis + Cheese + (Refresher Shard or Aghanim's Blessing)"
    } else {
        "Aegis + Cheese + Aghanim's Blessing + Refresher Shard"
    }
}

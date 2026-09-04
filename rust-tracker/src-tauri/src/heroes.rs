//! Item constants used by the tracking logic. Hero/item *display* concerns
//! (readable names, CDN icon URLs) live in the frontend now — see
//! `ui/app.js` — so only what the backend actually reasons about is kept
//! here: which items count as "key" purchases, and Roshan's drop table.

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

pub fn roshan_drops(death_count: u32) -> &'static str {
    if death_count <= 1 {
        "Aegis of the Immortal"
    } else if death_count == 2 {
        "Aegis + Cheese + (Refresher Shard or Aghanim's Blessing)"
    } else {
        "Aegis + Cheese + Aghanim's Blessing + Refresher Shard"
    }
}

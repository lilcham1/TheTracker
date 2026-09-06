//! Item constants used by the tracking logic. Hero/item *display* concerns
//! (readable names, CDN icon URLs) live in the frontend now — see
//! `ui/app.js` — so only what the backend actually reasons about is kept
//! here: which items count as "key" purchases, and Roshan's drop table.

/// GSI reports items by Valve's *internal* name, not the name printed on the
/// shop icon, and for several items the two differ: Daedalus is
/// `greater_crit`, Linken's Sphere is `sphere`, Aghanim's Blessing is
/// `ultimate_scepter_2`.
///
/// Three entries here used to be shop names — `daedalus`, `linkens_sphere`
/// and `aghanims_scepter` — so those purchases matched nothing and were
/// silently never logged, while `eye_of_skadi` duplicated `skadi` under a
/// name that does not exist. Every string below is checked against Valve's
/// item constants; anything added later must be too, because a wrong name
/// fails quietly rather than loudly.
pub const KEY_ITEMS: &[&str] = &[
    "boots", "boots_of_elves", "phase_boots", "power_treads", "arcane_boots",
    "tranquil_boots", "travel_boots", "travel_boots_2", "guardian_greaves",
    "blink", "overwhelming_blink", "swift_blink", "arcane_blink",
    "black_king_bar", "ultimate_scepter", "ultimate_scepter_2", "aghanims_shard",
    "refresher", "refresher_shard", "heart", "assault", "shivas_guard",
    "satanic", "skadi", "manta", "sange_and_yasha", "yasha_and_kaya",
    "greater_crit", "butterfly", "silver_edge", "nullifier", "abyssal_blade",
    "monkey_king_bar", "sphere", "sheepstick", "rod_of_atos",
    "octarine_core", "bloodthorn", "dagon", "dagon_5",
    "mage_slayer",
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn key_items_use_valve_internal_names() {
        // The three that were wrong, and the shape of the mistake: a shop
        // name where GSI sends an internal one. These match nothing at
        // runtime and produce no error — the purchase simply never appears.
        for shop_name in ["daedalus", "linkens_sphere", "aghanims_scepter", "eye_of_skadi"] {
            assert!(
                !is_key_item(shop_name),
                "{shop_name} is a shop name, not an item name GSI ever sends"
            );
        }

        for internal in ["greater_crit", "sphere", "ultimate_scepter", "skadi", "mage_slayer"] {
            assert!(is_key_item(internal), "{internal} is a real item name and should be tracked");
        }
    }

    #[test]
    fn key_item_list_has_no_duplicates() {
        let mut seen = std::collections::HashSet::new();
        for item in KEY_ITEMS {
            assert!(seen.insert(item), "{item} is listed twice");
        }
    }
}

//! Detects which Steam account is signed in on this PC, so linking Dota and
//! Deadlock doesn't require typing a name and picking yourself out of a list
//! of strangers with the same display name.
//!
//! # What this reads, and what it deliberately does not
//!
//! Only two things are read, both of which are plain account *identity*:
//!
//! - `HKCU\Software\Valve\Steam\ActiveProcess\ActiveUser` — a number: the
//!   32-bit id of the account currently signed in. Zero when Steam is closed.
//! - `<Steam>\config\loginusers.vdf` — the list of accounts that have signed
//!   in on this machine. Only the SteamID64 key, `PersonaName` and
//!   `MostRecent` are taken from it.
//!
//! Nothing else in the Steam directory is opened. In particular this never
//! touches `config.vdf` or the `ssfn*` files, which is where Steam keeps
//! login secrets and machine auth tokens — no password, token, cookie or
//! session of any kind is read, stored, transmitted or logged. The account
//! id is public: it is the same number that appears in a Steam profile URL
//! and is what OpenDota and the Deadlock API are keyed on.

use serde::Serialize;

/// SteamID64 values are the 32-bit account id plus this constant, so going
/// the other way is a subtraction.
const STEAMID64_BASE: u64 = 76_561_197_960_265_728;

#[derive(Debug, Clone, Serialize)]
pub struct SteamAccount {
    #[serde(rename = "accountId")]
    pub account_id: u64,
    pub personaname: Option<String>,
    /// "signed in" for the account Steam is currently running as, otherwise
    /// "previously signed in" — shown so it's obvious which is which.
    pub source: String,
}

#[cfg(windows)]
fn steam_path() -> Option<std::path::PathBuf> {
    use winreg::enums::HKEY_CURRENT_USER;
    use winreg::RegKey;

    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    let key = hkcu.open_subkey(r"Software\Valve\Steam").ok()?;
    let path: String = key.get_value("SteamPath").ok()?;
    Some(std::path::PathBuf::from(path.replace('/', "\\")))
}

#[cfg(not(windows))]
fn steam_path() -> Option<std::path::PathBuf> {
    let home = dirs::home_dir()?;
    for candidate in [".steam/steam", ".local/share/Steam", "Library/Application Support/Steam"] {
        let p = home.join(candidate);
        if p.is_dir() {
            return Some(p);
        }
    }
    None
}

/// The account Steam is signed in as right now, if it's running.
#[cfg(windows)]
fn active_account_id() -> Option<u64> {
    use winreg::enums::HKEY_CURRENT_USER;
    use winreg::RegKey;

    let hkcu = RegKey::predef(HKEY_CURRENT_USER);
    let key = hkcu.open_subkey(r"Software\Valve\Steam\ActiveProcess").ok()?;
    let active: u32 = key.get_value("ActiveUser").ok()?;
    // Zero means "no one is signed in" rather than "account 0".
    if active == 0 {
        None
    } else {
        Some(active as u64)
    }
}

#[cfg(not(windows))]
fn active_account_id() -> Option<u64> {
    None
}

/// Pulls account ids and display names out of `loginusers.vdf`.
///
/// Valve's VDF is a small nested key/value format. Rather than take a
/// dependency on a full parser for one file, this walks it line by line: a
/// quoted 17-digit number at the start of a line is an account block, and
/// the `PersonaName` / `MostRecent` inside it are the only fields wanted.
fn parse_login_users(text: &str) -> Vec<SteamAccount> {
    let mut out: Vec<(u64, Option<String>, bool)> = Vec::new();
    let mut current: Option<u64> = None;

    for line in text.lines() {
        let trimmed = line.trim();
        let fields: Vec<&str> = trimmed.split('"').filter(|s| !s.trim().is_empty()).collect();

        // A block header is a lone quoted SteamID64.
        if fields.len() == 1 {
            if let Ok(id64) = fields[0].trim().parse::<u64>() {
                if id64 > STEAMID64_BASE {
                    current = Some(id64 - STEAMID64_BASE);
                    out.push((id64 - STEAMID64_BASE, None, false));
                    continue;
                }
            }
        }

        let Some(active_id) = current else { continue };
        if fields.len() < 2 {
            continue;
        }
        let key = fields[0].trim();
        let value = fields[1].trim();

        if key.eq_ignore_ascii_case("PersonaName") {
            if let Some(entry) = out.iter_mut().find(|(id, _, _)| *id == active_id) {
                entry.1 = Some(value.to_string());
            }
        } else if key.eq_ignore_ascii_case("MostRecent") && value == "1" {
            if let Some(entry) = out.iter_mut().find(|(id, _, _)| *id == active_id) {
                entry.2 = true;
            }
        }
    }

    out.into_iter()
        .map(|(id, name, most_recent)| SteamAccount {
            account_id: id,
            personaname: name,
            source: if most_recent { "most recent".to_string() } else { "previously signed in".to_string() },
        })
        .collect()
}

/// Every Steam account this PC knows about, the currently signed-in one
/// first. Empty when Steam isn't installed or has never been signed into.
pub fn detect() -> Vec<SteamAccount> {
    let mut accounts: Vec<SteamAccount> = Vec::new();

    if let Some(path) = steam_path() {
        let vdf = path.join("config").join("loginusers.vdf");
        if let Ok(text) = std::fs::read_to_string(&vdf) {
            accounts = parse_login_users(&text);
        }
    }

    // Mark (and float) whichever account Steam is actually running as.
    if let Some(active) = active_account_id() {
        if let Some(pos) = accounts.iter().position(|a| a.account_id == active) {
            accounts[pos].source = "signed in now".to_string();
            accounts.swap(0, pos);
        } else {
            accounts.insert(
                0,
                SteamAccount { account_id: active, personaname: None, source: "signed in now".to_string() },
            );
        }
    } else {
        // Otherwise lead with the most recently used account.
        if let Some(pos) = accounts.iter().position(|a| a.source == "most recent") {
            accounts.swap(0, pos);
        }
    }

    accounts
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_accounts_and_ignores_everything_else() {
        // Shape matches a real loginusers.vdf, including fields that must be
        // ignored rather than mistaken for an account block.
        let sample = r#"
"users"
{
	"76561198810668586"
	{
		"AccountName"		"lilcham"
		"PersonaName"		"lilcham"
		"RememberPassword"		"1"
		"MostRecent"		"1"
		"Timestamp"		"1788560000"
	}
	"76561198000000001"
	{
		"AccountName"		"someone"
		"PersonaName"		"Someone Else"
		"MostRecent"		"0"
	}
}
"#;
        let accounts = parse_login_users(sample);
        assert_eq!(accounts.len(), 2);
        assert_eq!(accounts[0].account_id, 850_402_858);
        assert_eq!(accounts[0].personaname.as_deref(), Some("lilcham"));
        assert_eq!(accounts[0].source, "most recent");
        assert_eq!(accounts[1].account_id, 39_734_273);
        assert_eq!(accounts[1].personaname.as_deref(), Some("Someone Else"));
    }

    #[test]
    fn ignores_files_with_no_accounts() {
        assert!(parse_login_users("\"users\"\n{\n}\n").is_empty());
    }
}

//! Account sign-in against Convex Auth's password provider.
//!
//! Convex Auth is normally driven by its JavaScript client, but the whole
//! flow is just the `auth:signIn` **action** returning a pair of tokens, so
//! the Rust backend calls it directly — no JS auth client bundled into the
//! frontend, and the UI stays a plain form talking to Tauri commands.
//!
//! Two tokens are involved: a short-lived JWT that authenticates function
//! calls, and a long-lived refresh token that mints new JWTs. Only the
//! refresh token is persisted (in `auth.json` beside the history), so
//! signing in survives a restart.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use convex::{ConvexClient, FunctionResult, Value};
use serde::Serialize;
use serde_json::Value as JsonValue;

use crate::convex_sync::convex_url;
use crate::storage::log_dir;

#[derive(Debug, Clone, Default, Serialize)]
pub struct AuthState {
    #[serde(rename = "signedIn")]
    pub signed_in: bool,
    pub email: Option<String>,
    /// Convex `users` document id. The leaderboard returns it per row so
    /// the app can mark which entries are yours.
    #[serde(rename = "userId")]
    pub user_id: Option<String>,
    /// Short-lived JWT. Not exposed to the frontend — only the fact that
    /// we have one is.
    #[serde(skip)]
    pub token: Option<String>,
    #[serde(skip)]
    pub refresh_token: Option<String>,
    #[serde(rename = "lastError")]
    pub last_error: Option<String>,
}

pub type SharedAuth = Arc<Mutex<AuthState>>;

// ---------- Persistence ----------

fn auth_file() -> std::path::PathBuf {
    log_dir().join("auth.json")
}

#[derive(serde::Serialize, serde::Deserialize, Default)]
struct StoredAuth {
    email: Option<String>,
    #[serde(rename = "refreshToken")]
    refresh_token: Option<String>,
}

fn save_stored(email: Option<String>, refresh_token: Option<String>) {
    let _ = std::fs::create_dir_all(log_dir());
    if let Ok(json) = serde_json::to_string_pretty(&StoredAuth { email, refresh_token }) {
        let _ = std::fs::write(auth_file(), json);
    }
}

fn load_stored() -> StoredAuth {
    match std::fs::read_to_string(auth_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => StoredAuth::default(),
    }
}

pub fn clear_stored() {
    let _ = std::fs::remove_file(auth_file());
}

// ---------- Convex calls ----------

async fn call_sign_in(args: BTreeMap<String, Value>) -> Result<(String, String), String> {
    let mut client = ConvexClient::new(&convex_url())
        .await
        .map_err(|e| format!("Couldn't reach Convex: {e}"))?;

    let result = client
        .action("auth:signIn", args)
        .await
        .map_err(|e| format!("Sign-in request failed: {e}"))?;

    let value = match result {
        FunctionResult::Value(v) => JsonValue::from(v),
        FunctionResult::ErrorMessage(e) => return Err(friendly_error(&e)),
        FunctionResult::ConvexError(e) => return Err(friendly_error(&format!("{e:?}"))),
    };

    let tokens = value.get("tokens").ok_or_else(|| {
        "Convex didn't return a session — check the email and password.".to_string()
    })?;
    if tokens.is_null() {
        return Err("Convex didn't return a session — check the email and password.".to_string());
    }
    let token = tokens.get("token").and_then(|t| t.as_str()).ok_or("Missing token")?;
    let refresh = tokens.get("refreshToken").and_then(|t| t.as_str()).ok_or("Missing refresh token")?;
    Ok((token.to_string(), refresh.to_string()))
}

/// Convex surfaces provider failures as long server-error strings; turn the
/// common ones into something worth showing a person.
fn friendly_error(raw: &str) -> String {
    let lower = raw.to_lowercase();
    if lower.contains("invalidsecret") || lower.contains("invalid password") || lower.contains("invalidaccountid") {
        "Wrong email or password.".to_string()
    } else if lower.contains("already exists") || lower.contains("account already") {
        "An account with that email already exists — try signing in.".to_string()
    } else if lower.contains("password") && lower.contains("8") {
        "Password must be at least 8 characters.".to_string()
    } else if lower.contains("invalid") && lower.contains("email") {
        "That doesn't look like a valid email address.".to_string()
    } else {
        raw.lines().next().unwrap_or(raw).to_string()
    }
}

fn password_args(email: &str, password: &str, flow: &str) -> BTreeMap<String, Value> {
    let mut params = BTreeMap::new();
    params.insert("email".to_string(), Value::String(email.to_string()));
    params.insert("password".to_string(), Value::String(password.to_string()));
    params.insert("flow".to_string(), Value::String(flow.to_string()));

    let mut args = BTreeMap::new();
    args.insert("provider".to_string(), Value::String("password".to_string()));
    args.insert("params".to_string(), Value::Object(params));
    args
}

/// Looks up the signed-in account's id, so the leaderboard can highlight
/// your own rows. Best-effort — a failure here doesn't invalidate the session.
async fn fetch_user_id(token: &str) -> Option<String> {
    let mut client = ConvexClient::new(&convex_url()).await.ok()?;
    client.set_auth(Some(token.to_string())).await;
    match client.query("profiles:whoami", BTreeMap::new()).await.ok()? {
        FunctionResult::Value(v) => JsonValue::from(v)
            .get("userId")
            .and_then(|u| u.as_str())
            .map(|s| s.to_string()),
        _ => None,
    }
}

/// `flow` is "signUp" to create an account or "signIn" to use an existing one.
pub async fn sign_in(auth: SharedAuth, email: String, password: String, flow: &str) -> Result<(), String> {
    let (token, refresh) = call_sign_in(password_args(&email, &password, flow)).await?;
    let user_id = fetch_user_id(&token).await;
    save_stored(Some(email.clone()), Some(refresh.clone()));
    if let Ok(mut a) = auth.lock() {
        a.signed_in = true;
        a.email = Some(email);
        a.user_id = user_id;
        a.token = Some(token);
        a.refresh_token = Some(refresh);
        a.last_error = None;
    }
    Ok(())
}

/// Exchanges a stored refresh token for a fresh JWT. Called at startup and
/// whenever a call is rejected as unauthenticated.
pub async fn refresh(auth: SharedAuth) -> Result<(), String> {
    let (stored_refresh, email) = {
        let a = auth.lock().map_err(|_| "auth state poisoned")?;
        (a.refresh_token.clone(), a.email.clone())
    };
    let Some(refresh_token) = stored_refresh else {
        return Err("Not signed in".to_string());
    };

    let mut args = BTreeMap::new();
    args.insert("refreshToken".to_string(), Value::String(refresh_token));

    match call_sign_in(args).await {
        Ok((token, new_refresh)) => {
            let user_id = fetch_user_id(&token).await;
            save_stored(email, Some(new_refresh.clone()));
            if let Ok(mut a) = auth.lock() {
                a.signed_in = true;
                a.user_id = user_id;
                a.token = Some(token);
                a.refresh_token = Some(new_refresh);
                a.last_error = None;
            }
            Ok(())
        }
        Err(e) => {
            // A dead refresh token means the session is genuinely over —
            // clear it rather than retrying forever on every sync.
            clear_stored();
            if let Ok(mut a) = auth.lock() {
                a.signed_in = false;
                a.token = None;
                a.refresh_token = None;
                a.last_error = Some(format!("Session expired — sign in again ({e})"));
            }
            Err(e)
        }
    }
}

pub async fn sign_out(auth: SharedAuth) {
    let token = auth.lock().ok().and_then(|a| a.token.clone());
    if let Some(token) = token {
        if let Ok(mut client) = ConvexClient::new(&convex_url()).await {
            client.set_auth(Some(token)).await;
            let _ = client.action("auth:signOut", BTreeMap::new()).await;
        }
    }
    clear_stored();
    if let Ok(mut a) = auth.lock() {
        *a = AuthState::default();
    }
}

/// Rebuilds auth state from disk at startup, refreshing the JWT if a
/// refresh token was stored.
pub fn restore(auth: SharedAuth) {
    let stored = load_stored();
    if stored.refresh_token.is_none() {
        return;
    }
    if let Ok(mut a) = auth.lock() {
        a.email = stored.email;
        a.refresh_token = stored.refresh_token;
    }
    tauri::async_runtime::spawn(async move {
        let _ = refresh(auth).await;
    });
}

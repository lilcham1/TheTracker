//! HTTP listener for Valve's official Dota 2 Game State Integration (GSI)
//! feed. Dota 2 itself POSTs JSON updates to this endpoint roughly every
//! 0.1-1s while a match is running (the config that tells it to is written
//! by gsi_setup.rs). This is the *only* HTTP route the app needs —
//! everything else (live view, history, profile) is read and written
//! directly against shared in-process state by the native UI, no
//! browser/JSON API required.

use std::sync::{Arc, Mutex};

use crate::state::Tracker;

pub const GSI_PORT: u16 = 3000;

/// Loopback only. Dota posts to `localhost`, so nothing legitimate ever
/// arrives from another machine — and the listener used to bind every
/// interface, which let anyone on the same Wi-Fi or LAN post a fabricated
/// match into the player's history, and from there into the shared
/// leaderboard if they were signed in. It also made Windows Firewall ask
/// to let a stats app accept network connections, which it never needed.
const BIND_ADDR: &str = "127.0.0.1";

pub enum ServerStatus {
    Listening,
    Failed(String),
}

/// Starts the GSI listener on a background thread. Returns immediately;
/// binding success/failure is reported back through `on_status`.
pub fn spawn_server(state: Arc<Mutex<Tracker>>, on_status: impl FnOnce(ServerStatus) + Send + 'static) {
    std::thread::spawn(move || {
        let server = match tiny_http::Server::http((BIND_ADDR, GSI_PORT)) {
            Ok(s) => s,
            Err(e) => {
                let technical = e.to_string();
                let in_use = technical.contains("Only one usage")
                    || technical.contains("Address already in use")
                    || technical.contains("os error 10048");
                // A second TheTracker can no longer get this far — the
                // single-instance lock hands it to the running one first — so
                // a taken port means some other program has it. Port 3000 is
                // the default for most web dev servers, which is the usual
                // culprit, and saying "another TheTracker window" sent people
                // looking for a window that did not exist.
                let message = if in_use {
                    format!(
                        "Another program is using port {GSI_PORT}, so Dota's live feed can't reach TheTracker. \
                         Close it (web dev servers often use this port) and restart TheTracker."
                    )
                } else {
                    format!("Live tracking could not start on port {GSI_PORT}: {technical}")
                };
                on_status(ServerStatus::Failed(message));
                return;
            }
        };
        on_status(ServerStatus::Listening);

        for mut request in server.incoming_requests() {
            let is_post_root = *request.method() == tiny_http::Method::Post && request.url() == "/";
            if is_post_root {
                let mut body = String::new();
                let _ = request.as_reader().read_to_string(&mut body);
                if let Ok(json) = serde_json::from_str::<serde_json::Value>(&body) {
                    if let Ok(mut tracker) = state.lock() {
                        tracker.handle_update(&json);
                    }
                }
                let _ = request.respond(tiny_http::Response::from_string("ok"));
            } else {
                let _ = request.respond(tiny_http::Response::empty(404));
            }
        }
    });
}

//! HTTP listener for Valve's official Dota 2 Game State Integration (GSI)
//! feed. Dota 2 itself POSTs JSON updates to this endpoint roughly every
//! 0.1-1s while a match is running (see the bundled
//! `gamestate_integration_dota_tracker.cfg`). This is the *only* HTTP route
//! the app needs — everything else (live view, history, profile) is read
//! and written directly against shared in-process state by the native UI,
//! no browser/JSON API required.

use std::sync::{Arc, Mutex};

use crate::state::Tracker;

pub const GSI_PORT: u16 = 3000;

pub enum ServerStatus {
    Listening,
    Failed(String),
}

/// Starts the GSI listener on a background thread. Returns immediately;
/// binding success/failure is reported back through `on_status`.
pub fn spawn_server(state: Arc<Mutex<Tracker>>, on_status: impl FnOnce(ServerStatus) + Send + 'static) {
    std::thread::spawn(move || {
        let server = match tiny_http::Server::http(("0.0.0.0", GSI_PORT)) {
            Ok(s) => s,
            Err(e) => {
                on_status(ServerStatus::Failed(format!(
                    "Couldn't bind to port {GSI_PORT}: {e}. Is another instance of the tracker already running?"
                )));
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

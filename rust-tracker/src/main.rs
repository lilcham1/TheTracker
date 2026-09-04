//! Dota Tracker — native desktop match tracker built on Valve's official
//! Game State Integration (GSI) feed. No memory reading, no third-party
//! game data, nothing that touches Dota 2's process — just the same
//! official local HTTP feed pro broadcast overlays use.

mod app;
mod gsi;
mod heroes;
mod model;
mod state;
mod storage;

use std::sync::{Arc, Mutex};

fn main() -> eframe::Result<()> {
    let tracker = Arc::new(Mutex::new(state::Tracker::new()));
    let server_error: Arc<Mutex<Option<String>>> = Arc::new(Mutex::new(None));

    {
        let tracker_for_server = tracker.clone();
        let server_error_for_server = server_error.clone();
        gsi::spawn_server(tracker_for_server, move |status| {
            if let gsi::ServerStatus::Failed(msg) = status {
                *server_error_for_server.lock().unwrap() = Some(msg);
            }
        });
    }

    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default().with_inner_size([440.0, 780.0]),
        ..Default::default()
    };

    eframe::run_native(
        "Dota Tracker",
        options,
        Box::new(move |cc| {
            egui_extras::install_image_loaders(&cc.egui_ctx);
            Ok(Box::new(app::App::new(tracker.clone(), server_error.clone())))
        }),
    )
}

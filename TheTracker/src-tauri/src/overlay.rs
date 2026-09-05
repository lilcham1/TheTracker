//! The in-game overlay window.
//!
//! A second Tauri window — transparent, borderless, always-on-top and
//! click-through — that floats over the game showing the same live data the
//! Live tab does. It is a plain desktop window: nothing is injected into
//! either game, no game memory is read, and it draws only data the player
//! already has (their own GSI feed for Dota, their own match presence for
//! Deadlock).
//!
//! Click-through matters: without it the overlay would eat mouse input
//! meant for the game. It can be toggled off so the overlay can be dragged
//! into position, then re-enabled.

use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder};

pub const OVERLAY_LABEL: &str = "overlay";

/// Creates the overlay window if it doesn't exist, otherwise shows it.
pub fn show(app: &AppHandle) -> Result<(), String> {
    if let Some(win) = app.get_webview_window(OVERLAY_LABEL) {
        win.show().map_err(|e| e.to_string())?;
        win.set_always_on_top(true).map_err(|e| e.to_string())?;
        return Ok(());
    }

    let win = WebviewWindowBuilder::new(app, OVERLAY_LABEL, WebviewUrl::App("overlay.html".into()))
        .title("Dota Tracker Overlay")
        .inner_size(320.0, 420.0)
        .position(24.0, 24.0)
        .decorations(false)
        .transparent(true)
        .always_on_top(true)
        .skip_taskbar(true)
        .resizable(false)
        .shadow(false)
        .focused(false)
        .build()
        .map_err(|e| format!("Couldn't create the overlay window: {e}"))?;

    // Start click-through so it never steals input from the game.
    let settings = crate::prefs::load().overlay;
    win.set_ignore_cursor_events(settings.click_through).map_err(|e| e.to_string())?;
    apply_settings(app, &settings);
    Ok(())
}

pub fn hide(app: &AppHandle) -> Result<(), String> {
    if let Some(win) = app.get_webview_window(OVERLAY_LABEL) {
        win.hide().map_err(|e| e.to_string())?;
    }
    Ok(())
}

pub fn is_visible(app: &AppHandle) -> bool {
    app.get_webview_window(OVERLAY_LABEL)
        .and_then(|w| w.is_visible().ok())
        .unwrap_or(false)
}

/// Turning click-through off lets the player drag the overlay somewhere
/// else; turning it back on returns input to the game.
pub fn set_click_through(app: &AppHandle, click_through: bool) -> Result<(), String> {
    let Some(win) = app.get_webview_window(OVERLAY_LABEL) else {
        return Err("Overlay isn't open".to_string());
    };
    win.set_ignore_cursor_events(click_through).map_err(|e| e.to_string())?;
    // While being positioned it needs to accept focus to receive the drag.
    let _ = win.set_focusable(!click_through);
    if !click_through {
        let _ = win.set_focus();
    }
    Ok(())
}

/// Applies saved appearance settings to the overlay window if it's open.
/// Opacity and scale are handled by the overlay page itself (CSS), so this
/// only deals with what the window manager owns: size, corner, and whether
/// the window swallows clicks.
pub fn apply_settings(app: &AppHandle, settings: &crate::prefs::OverlaySettings) {
    let Some(win) = app.get_webview_window(OVERLAY_LABEL) else { return };

    let _ = win.set_ignore_cursor_events(settings.click_through);
    let _ = win.set_focusable(!settings.click_through);

    let width = 320.0 * settings.scale;
    let height = 420.0 * settings.scale;
    let _ = win.set_size(tauri::LogicalSize::new(width, height));

    // Corner placement is computed against the monitor the overlay is on,
    // so it lands correctly on multi-monitor setups.
    if let Ok(Some(monitor)) = win.current_monitor() {
        let scale_factor = monitor.scale_factor();
        let screen = monitor.size().to_logical::<f64>(scale_factor);
        let margin = 24.0;
        let (x, y) = match settings.corner.as_str() {
            "top-right" => (screen.width - width - margin, margin),
            "bottom-left" => (margin, screen.height - height - margin),
            "bottom-right" => (screen.width - width - margin, screen.height - height - margin),
            _ => (margin, margin),
        };
        let _ = win.set_position(tauri::LogicalPosition::new(x, y));
    }
}

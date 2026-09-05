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

/// Builds the overlay window once, hidden, at startup.
///
/// Creating it lazily on first show was a race: the auto-show watcher and a
/// manual toggle could both find no window and both build one, leaving two
/// identical always-on-top windows stacked over the game. Building it once
/// up front and only ever toggling visibility makes that impossible rather
/// than merely unlikely.
pub fn ensure(app: &AppHandle) -> Result<(), String> {
    if app.get_webview_window(OVERLAY_LABEL).is_some() {
        return Ok(());
    }

    let win = WebviewWindowBuilder::new(app, OVERLAY_LABEL, WebviewUrl::App("overlay.html".into()))
        .title("TheTracker Overlay")
        .inner_size(320.0, 420.0)
        .position(24.0, 24.0)
        .decorations(false)
        .transparent(true)
        .always_on_top(true)
        .skip_taskbar(true)
        .resizable(false)
        .shadow(false)
        .focused(false)
        // Starts hidden: the watcher shows it when a match begins.
        .visible(false)
        .build()
        .map_err(|e| format!("Couldn't create the overlay window: {e}"))?;

    // Click-through from the outset, so it can never steal input from the game.
    let settings = crate::prefs::load().overlay;
    win.set_ignore_cursor_events(settings.click_through).map_err(|e| e.to_string())?;
    Ok(())
}

/// Shows the overlay, creating it first if startup somehow hadn't.
pub fn show(app: &AppHandle) -> Result<(), String> {
    ensure(app)?;
    let Some(win) = app.get_webview_window(OVERLAY_LABEL) else {
        return Err("Overlay window is unavailable".to_string());
    };

    let settings = crate::prefs::load().overlay;
    // Position before showing, so it never flashes in the wrong corner or on
    // the wrong monitor on the way to the right one.
    apply_settings(app, &settings);
    win.show().map_err(|e| e.to_string())?;
    win.set_always_on_top(true).map_err(|e| e.to_string())?;

    // Tell the page when it appeared. The window is built once at startup
    // and afterwards only toggled, so it is never reloaded and cannot work
    // this out from load time — without the stamp it has no way to tell a
    // fresh manual open from having sat hidden for an hour, and so no way
    // to acknowledge the button and then get out of the way.
    let _ = win.eval("window.__overlayShownAt = Date.now()");
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

    // Place the overlay on whichever monitor the main window is on. Using the
    // overlay's own monitor would pin it to wherever it happened to be
    // created — in practice the primary screen — so on a multi-monitor setup
    // it would appear on a display the player isn't looking at, which reads
    // exactly like the button doing nothing.
    // An explicitly chosen display wins. Otherwise fall back to whichever
    // one the main window is on.
    let chosen = if settings.monitor.is_empty() {
        None
    } else {
        win.available_monitors()
            .ok()
            .and_then(|list| list.into_iter().find(|m| m.name().map(|n| n.as_str()) == Some(settings.monitor.as_str())))
    };

    let monitor = chosen
        .or_else(|| app.get_webview_window("main").and_then(|w| w.current_monitor().ok().flatten()))
        .or_else(|| win.current_monitor().ok().flatten());

    let Some(monitor) = monitor else { return };

    let scale_factor = monitor.scale_factor();
    let screen = monitor.size().to_logical::<f64>(scale_factor);
    // Monitor coordinates are relative to the whole virtual desktop, so the
    // monitor's own origin has to be added — without it every position is
    // computed as though that screen started at 0,0, which lands the window
    // on the primary display no matter which monitor was chosen.
    let origin = monitor.position().to_logical::<f64>(scale_factor);

    let margin = 24.0;
    let (dx, dy) = match settings.corner.as_str() {
        "top-right" => (screen.width - width - margin, margin),
        "bottom-left" => (margin, screen.height - height - margin),
        "bottom-right" => (screen.width - width - margin, screen.height - height - margin),
        _ => (margin, margin),
    };

    let _ = win.set_position(tauri::LogicalPosition::new(origin.x + dx, origin.y + dy));
}

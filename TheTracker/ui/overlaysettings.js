// Overlay controls: open/close, click-through, appearance, and which
// panels the overlay draws.
//
// Changes are saved and applied to the live window immediately, so what you
// see while adjusting is what you get in game.

function renderOverlaySettings() {
  const root = document.getElementById("tab-overlaysettings");
  if (!root) return;

  const o = (state.prefs && state.prefs.overlay) || {};
  const open = state.overlay.visible;

  const corners = [
    ["top-left", "Top left"],
    ["top-right", "Top right"],
    ["bottom-left", "Bottom left"],
    ["bottom-right", "Bottom right"],
  ];

  const panels = [
    ["showStats", "Stats (last hits, deaths, gold lost)"],
    ["showRoshan", "Roshan timer"],
    ["showCheckpoints", "Last-hit checkpoints"],
    ["showItems", "Key items"],
    ["showDeaths", "Death log"],
  ];

  root.innerHTML = `
    <div class="card col">
      <div class="section-head">
        <h3 class="section-title">Overlay</h3>
        <span class="badge ${open ? "badge-win" : "badge-neutral"}">${open ? "Open" : "Closed"}</span>
      </div>
      <p class="hint">
        A separate always-on-top window that floats over the game. It is an
        ordinary desktop window — nothing is injected into Dota or Deadlock,
        and it only shows data you already have.
      </p>
      <div class="row">
        <button class="btn" id="ovToggle" type="button">${open ? "Close overlay" : "Open overlay"}</button>
        <button class="btn btn-secondary" id="ovReposition" type="button" ${open ? "" : "disabled"}>
          ${o.clickThrough ? "Unlock to move" : "Lock in place"}
        </button>
      </div>
      <p class="hint">
        ${
          o.clickThrough
            ? "Click-through is <b>on</b> — the overlay can't steal mouse input from the game. Unlock it to drag the overlay somewhere else."
            : "Click-through is <b>off</b> — you can drag the overlay, but it will intercept clicks. Lock it again before playing."
        }
      </p>
    </div>

    <div class="card col">
      <div class="section-head"><h3 class="section-title">Appearance</h3></div>

      <div>
        <label class="form-label">Opacity — ${Math.round((o.opacity ?? 0.85) * 100)}%</label>
        <input type="range" id="ovOpacity" min="25" max="100" step="5" value="${Math.round((o.opacity ?? 0.85) * 100)}" />
      </div>

      <div>
        <label class="form-label">Size — ${Math.round((o.scale ?? 1) * 100)}%</label>
        <input type="range" id="ovScale" min="75" max="150" step="5" value="${Math.round((o.scale ?? 1) * 100)}" />
      </div>

      <div>
        <label class="form-label">Corner</label>
        <div class="chip-row">
          ${corners
            .map(
              ([id, label]) =>
                `<button class="chip ${o.corner === id ? "selected" : ""}" data-ov-corner="${id}" type="button">${label}</button>`
            )
            .join("")}
        </div>
      </div>
    </div>

    <div class="card col">
      <div class="section-head"><h3 class="section-title">Panels</h3></div>
      <p class="hint">Trim the overlay down to only what you want on screen mid-game.</p>
      ${panels
        .map(
          ([key, label]) => `
        <label class="switch-row">
          <input type="checkbox" data-ov-panel="${key}" ${o[key] ? "checked" : ""} />
          <span>${label}</span>
        </label>`
        )
        .join("")}
    </div>`;

  // ----- wiring -----

  root.querySelector("#ovToggle").addEventListener("click", async () => {
    await toggleOverlay();
    renderOverlaySettings();
  });

  const repo = root.querySelector("#ovReposition");
  if (repo)
    repo.addEventListener("click", async () => {
      await saveOverlay({ clickThrough: !o.clickThrough });
      // Reflect the flip on the live window straight away.
      await invoke("overlay_click_through", { clickThrough: !o.clickThrough }).catch(() => {});
      renderOverlaySettings();
    });

  root.querySelector("#ovOpacity").addEventListener("change", (e) =>
    saveOverlay({ opacity: Number(e.target.value) / 100 }).then(renderOverlaySettings)
  );
  root.querySelector("#ovScale").addEventListener("change", (e) =>
    saveOverlay({ scale: Number(e.target.value) / 100 }).then(renderOverlaySettings)
  );
  root.querySelectorAll("[data-ov-corner]").forEach((el) =>
    el.addEventListener("click", () => saveOverlay({ corner: el.dataset.ovCorner }).then(renderOverlaySettings))
  );
  root.querySelectorAll("[data-ov-panel]").forEach((el) =>
    el.addEventListener("change", () => saveOverlay({ [el.dataset.ovPanel]: el.checked }).then(renderOverlaySettings))
  );
}

/// Merges a partial change into the saved overlay settings. The backend
/// clamps the values and pushes them onto the live window.
async function saveOverlay(patch) {
  const current = (state.prefs && state.prefs.overlay) || {};
  state.prefs = await invoke("save_overlay_settings", { settings: { ...current, ...patch } });
  return state.prefs;
}

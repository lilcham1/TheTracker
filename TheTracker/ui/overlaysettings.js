// Overlay controls: open/close, click-through, appearance, and which panels
// each game's overlay draws.
//
// The overlay covers Dota only. Deadlock publishes no live feed, so a
// Deadlock overlay could show nothing beyond "you are in a match", which
// isn't worth a window floating over the game.

// Displays available to pin the overlay to. Fetched once; the list only
// changes when a monitor is plugged in or removed.
let OVERLAY_MONITORS = null;

async function loadMonitors() {
  if (OVERLAY_MONITORS) return OVERLAY_MONITORS;
  try {
    OVERLAY_MONITORS = await invoke("list_monitors");
  } catch (_) {
    OVERLAY_MONITORS = [];
  }
  return OVERLAY_MONITORS;
}

function renderOverlaySettings() {
  const root = document.getElementById("tab-overlaysettings");
  if (!root) return;

  const o = (state.prefs && state.prefs.overlay) || {};
  const dota = o.dota || {};
  const open = state.overlay.visible;

  const corners = [
    ["top-left", "Top left"],
    ["top-right", "Top right"],
    ["bottom-left", "Bottom left"],
    ["bottom-right", "Bottom right"],
  ];

  const dotaPanels = [
    ["runes", "Runes — bounty, water, power, wisdom"],
    ["lotus", "Healing lotus spawns"],
    ["stacks", "Neutral camp stack pull (:53)"],
    ["daynight", "Day / night flip"],
  ];

  root.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head">
        <h2 class="home-title">Overlay</h2>
        <div class="home-meta">${open ? "Open" : "Closed"}</div>
        <button class="link-btn" id="ovToggle" type="button">${open ? "Close overlay" : "Open overlay"}</button>
      </div>
      <p class="hint" style="max-width:70ch">
        A separate always-on-top window that floats over the game. It is an
        ordinary desktop window &mdash; nothing is injected into either game,
        no game memory is read, and it only ever draws information you
        already have.
      </p>
      <p class="hint" style="max-width:70ch;margin-top:8px">
        It draws one thing: a countdown in the <b>last five seconds</b>
        before an event. At every other moment &mdash; between matches,
        during the draft, and most of the match itself &mdash; the window is
        empty, so expect to see nothing until something is about to happen.
        To place it, use <b>Unlock to move</b> below; a single marker stands
        in while it is unlocked, since an empty transparent window cannot be
        dragged anywhere you can see.
      </p>

      <label class="switch-row" style="margin-top:10px">
        <input type="checkbox" id="ovAuto" ${o.auto !== false ? "checked" : ""} />
        <span>Open by itself when a match starts, and close when it ends</span>
      </label>

      <div class="row" style="margin-top:12px">
        <button class="btn btn-secondary" id="ovReposition" type="button" ${open ? "" : "disabled"}>
          ${o.clickThrough ? "Unlock to move" : "Lock in place"}
        </button>
        <span class="hint">
          ${
            o.clickThrough
              ? "Click-through is <b>on</b> &mdash; the overlay cannot take mouse input from the game."
              : "Click-through is <b>off</b> &mdash; you can drag it, but it will intercept clicks. Lock it before playing."
          }
        </span>
      </div>
    </section>

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">Appearance</h2></div>
      <div class="split">
        <div class="split-item">
          <label class="form-label">Opacity &mdash; ${Math.round((o.opacity ?? 0.85) * 100)}%</label>
          <input type="range" id="ovOpacity" min="25" max="100" step="5" value="${Math.round((o.opacity ?? 0.85) * 100)}" />
        </div>
        <div class="split-item">
          <label class="form-label">Size &mdash; ${Math.round((o.scale ?? 1) * 100)}%</label>
          <input type="range" id="ovScale" min="75" max="150" step="5" value="${Math.round((o.scale ?? 1) * 100)}" />
        </div>
        <div class="split-item">
          <label class="form-label">Display</label>
          <select class="text-input" id="ovMonitor">
            <option value="" ${!o.monitor ? "selected" : ""}>Follow the app window</option>
            ${(OVERLAY_MONITORS || [])
              .map(
                (m) =>
                  `<option value="${escapeHtml(m.name)}" ${o.monitor === m.name ? "selected" : ""}>${escapeHtml(
                    m.name.split("\\").pop() || m.name
                  )} — ${m.width}×${m.height}${m.primary ? " (primary)" : ""}</option>`
              )
              .join("")}
          </select>
          <p class="field-hint" style="margin-top:6px">
            Pick the screen you play on. Following the app window puts the
            overlay wherever the tracker is, which is the wrong screen if you
            keep it on a second monitor.
          </p>
        </div>

        <div class="split-item">
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
    </section>

    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Warn me about</h2>
        <div class="home-meta">five seconds before each</div>
      </div>
      ${dotaPanels
        .map(
          ([key, label]) => `
        <label class="switch-row">
          <input type="checkbox" data-ov-dota="${key}" ${dota[key] ? "checked" : ""} />
          <span>${label}</span>
        </label>`
        )
        .join("")}
      <p class="hint" style="margin-top:10px;max-width:72ch">
        These are the timers every established Dota overlay shows, and all of
        them are arithmetic on the match clock you can already see. Valve
        ships Game State Integration specifically so tools can read this, and
        it only ever exposes your own state &mdash; never an opponent's.
      </p>
    </section>

`;

  // ----- wiring -----

  root.querySelector("#ovToggle").addEventListener("click", async () => {
    await toggleOverlay();
    renderOverlaySettings();
  });

  const repo = root.querySelector("#ovReposition");
  if (repo) {
    repo.addEventListener("click", async () => {
      const next = !o.clickThrough;
      await saveOverlay({ clickThrough: next });
      await invoke("overlay_click_through", { clickThrough: next }).catch(() => {});
      renderOverlaySettings();
    });
  }

  // Populate the display list, then redraw once so the picker fills in.
  if (!OVERLAY_MONITORS) loadMonitors().then(renderOverlaySettings);

  const mon = root.querySelector("#ovMonitor");
  if (mon) {
    mon.addEventListener("change", () =>
      saveOverlay({ monitor: mon.value }).then(() => {
        // Re-show so the move is visible straight away.
        if (state.overlay.visible) invoke("overlay_show").catch(() => {});
        renderOverlaySettings();
      })
    );
  }

  const auto = root.querySelector("#ovAuto");
  if (auto) {
    auto.addEventListener("change", () =>
      saveOverlay({ auto: auto.checked }).then(renderOverlaySettings)
    );
  }

  root.querySelector("#ovOpacity").addEventListener("change", (e) =>
    saveOverlay({ opacity: Number(e.target.value) / 100 }).then(renderOverlaySettings)
  );
  root.querySelector("#ovScale").addEventListener("change", (e) =>
    saveOverlay({ scale: Number(e.target.value) / 100 }).then(renderOverlaySettings)
  );
  root.querySelectorAll("[data-ov-corner]").forEach((el) =>
    el.addEventListener("click", () => saveOverlay({ corner: el.dataset.ovCorner }).then(renderOverlaySettings))
  );

  root.querySelectorAll("[data-ov-dota]").forEach((el) =>
    el.addEventListener("change", () =>
      saveOverlay({ dota: { ...dota, [el.dataset.ovDota]: el.checked } }).then(renderOverlaySettings)
    )
  );
}

/// Merges a partial change into the saved overlay settings. The backend
/// clamps the values and pushes them onto the live window.
async function saveOverlay(patch) {
  const current = (state.prefs && state.prefs.overlay) || {};
  state.prefs = await invoke("save_overlay_settings", { settings: { ...current, ...patch } });
  return state.prefs;
}

// Update checking and installing.
//
// Two paths, deliberately: a quiet automatic check shortly after launch that
// only shows something if there is genuinely a new version, and an explicit
// "Check now" button for people who want to ask. Nothing installs without a
// click — an app that restarts itself mid-game would be worse than useless
// for a match tracker.

const UPDATES = {
  info: null, // last successful check
  checking: false,
  installing: false,
  error: null,
  dismissed: false,
};

async function checkForUpdate({ quiet = false } = {}) {
  if (UPDATES.checking || UPDATES.installing) return UPDATES.info;
  UPDATES.checking = true;
  if (!quiet) UPDATES.error = null;
  renderUpdateBanner();
  if (state.view === "about") renderAbout();

  try {
    UPDATES.info = await invoke("check_for_update");
    UPDATES.error = null;
  } catch (e) {
    // A failed automatic check stays silent — the app still works offline,
    // and a nag about the update server is noise. An explicit check says so.
    UPDATES.error = quiet ? null : String(e);
  }

  UPDATES.checking = false;
  renderUpdateBanner();
  if (state.view === "about") renderAbout();
  return UPDATES.info;
}

async function installUpdate() {
  if (UPDATES.installing) return;
  UPDATES.installing = true;
  UPDATES.error = null;
  renderUpdateBanner();
  if (state.view === "about") renderAbout();

  try {
    // On Windows this hands off to the installer and the app exits, so
    // anything after this line usually never runs.
    await invoke("install_update");
  } catch (e) {
    UPDATES.error = String(e);
    UPDATES.installing = false;
    renderUpdateBanner();
    if (state.view === "about") renderAbout();
  }
}

/// A slim bar above the content, shown only when an update is actually
/// waiting and the player hasn't dismissed it this session.
function renderUpdateBanner() {
  const host = document.getElementById("updateBanner");
  if (!host) return;

  const info = UPDATES.info;
  const show = info && info.available && !UPDATES.dismissed;
  host.hidden = !show;
  if (!show) return;

  host.innerHTML = `
    <span class="upd-dot"></span>
    <span class="upd-text">
      <b>Version ${escapeHtml(info.version || "")}</b> is available
      &mdash; you're on ${escapeHtml(info.current || "")}.
    </span>
    <button class="btn upd-btn" id="updInstall" type="button" ${UPDATES.installing ? "disabled" : ""}>
      ${UPDATES.installing ? "Installing&hellip;" : "Update &amp; restart"}
    </button>
    <button class="link-btn" id="updLater" type="button">Later</button>`;

  host.querySelector("#updInstall").addEventListener("click", installUpdate);
  host.querySelector("#updLater").addEventListener("click", () => {
    UPDATES.dismissed = true;
    renderUpdateBanner();
  });
}

/// The About view: version, an explicit check, and what updating involves.
function renderAbout() {
  const root = document.getElementById("tab-about");
  if (!root) return;

  const info = UPDATES.info;
  const current = (info && info.current) || APP_VERSION;

  let status = "";
  if (UPDATES.checking) {
    status = `<span class="hint">Checking&hellip;</span>`;
  } else if (UPDATES.error) {
    status = `<div class="note err" style="max-width:70ch">${escapeHtml(UPDATES.error)}</div>`;
  } else if (info && info.available) {
    status = `<span class="rail-value win" style="font-size:15px">Version ${escapeHtml(info.version)} available</span>`;
  } else if (info) {
    status = `<span class="hint">You're on the latest version.</span>`;
  }

  root.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head">
        <h2 class="home-title">TheTracker</h2>
        <div class="home-meta">version ${escapeHtml(current)}</div>
      </div>

      <div class="row" style="margin-bottom:12px">
        <button class="btn" id="aboutCheck" type="button" ${UPDATES.checking ? "disabled" : ""}>
          ${UPDATES.checking ? "Checking&hellip;" : "Check for updates"}
        </button>
        ${
          info && info.available
            ? `<button class="btn btn-secondary" id="aboutInstall" type="button" ${UPDATES.installing ? "disabled" : ""}>
                 ${UPDATES.installing ? "Installing&hellip;" : "Update &amp; restart"}
               </button>`
            : ""
        }
        ${status}
      </div>

      ${
        info && info.available && info.notes
          ? `<div class="note" style="max-width:72ch"><b>What's new</b><br />${escapeHtml(info.notes)}</div>`
          : ""
      }

      <p class="hint" style="max-width:72ch;margin-top:14px">
        Updates are published to this project's GitHub releases and checked
        automatically a few seconds after launch. Nothing installs on its own
        &mdash; installing restarts the app, which would be unwelcome mid-match,
        so it always waits for you to press the button.
      </p>
      <p class="hint" style="max-width:72ch">
        Every download is signature-checked against a key built into this app.
        Tauri enforces that and it cannot be disabled, so a tampered or
        unsigned package is refused rather than installed.
      </p>
    </section>

    <section class="home-section">
      <div class="home-head"><h2 class="home-title">Data sources</h2></div>
      <p class="hint" style="max-width:72ch">
        Live Dota tracking uses Valve's official Game State Integration feed.
        Dota match history comes from OpenDota, and Deadlock from the
        community-run Deadlock API &mdash; Valve publishes no live feed for
        Deadlock. Nothing reads either game's memory or injects into it.
      </p>
    </section>`;

  const check = root.querySelector("#aboutCheck");
  if (check) check.addEventListener("click", () => checkForUpdate());
  const inst = root.querySelector("#aboutInstall");
  if (inst) inst.addEventListener("click", installUpdate);
}

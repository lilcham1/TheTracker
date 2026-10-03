// Settings: how the app runs, the overlay, the account, and about/updates.

const SET = {
  monitors: null, authFlow: "signIn", authBusy: false, authError: null,
  diag: null, diagRunning: false, backups: null, backupPath: null, restoreArmed: null,
};

const RANKS = ["Herald", "Guardian", "Crusader", "Archon", "Legend", "Ancient", "Divine", "Immortal"];
const ROLES = ["Carry", "Mid", "Offlane", "Support", "Hard support"];

const SETTINGS_TABS = [["general", "General"], ["overlay", "Overlay"], ["account", "Account"], ["about", "About and updates"]];

act("settings-tab", (el) => go("settings", { tab: el.dataset.tab }));

// ---------- General ----------

onChange("bg-start", async (el) => {
  const bg = await attempt(() => invoke("set_start_with_windows", { enabled: el.checked }));
  if (bg) S.boot.background = bg;
  renderBanners();
  lastHtml = null;
  rerender();
});
onChange("bg-tray", async (el) => {
  const bg = await attempt(() => invoke("set_close_to_tray", { enabled: el.checked }));
  if (bg) S.boot.background = bg;
  lastHtml = null;
  rerender();
});
act("backup", async () => {
  const out = await attempt(() => invoke("backup"), "Backup saved.");
  if (out) {
    SET.backupPath = out.path;
    SET.backups = await invoke("list_backups").catch(() => SET.backups);
    rerender();
  }
});
act("restore", async (el) => {
  const path = el.dataset.path;
  if (SET.restoreArmed !== path) {
    SET.restoreArmed = path;
    return rerender();
  }
  const out = await attempt(() => invoke("restore", { path }));
  SET.restoreArmed = null;
  if (out) {
    toast("Restored. Your current data was backed up first, in the data folder.");
    S.boot = await invoke("boot");
    for (const r of Object.values(RES)) r.clear();
    await loadHistory();
  }
  rerender();
});
act("quit", () => invoke("quit"));

function generalHtml() {
  const bg = S.boot.background;
  const backups = SET.backups || [];
  const name = (p) => p.split(/[\\/]/).pop().replace("TheTracker-backup_", "").replace(".zip", "").replace("_", " at ").replace(/(\d{2})(\d{2})(\d{2})$/, "$1:$2");
  return `<section class="set">
      <h3>Running in the background</h3>
      <p class="muted">TheTracker records a match only while it's running. If it's closed when you play, that game never reaches your Sessions.</p>
      <label class="switch"><input type="checkbox" data-change="bg-start" ${bg.startWithWindows ? "checked" : ""} /><span>Start with Windows <span class="muted">(opens in the tray, not on screen)</span></span></label>
      <label class="switch"><input type="checkbox" data-change="bg-tray" ${bg.closeToTray ? "checked" : ""} ${bg.trayAvailable ? "" : "disabled"} /><span>Keep running in the tray when I close the window</span></label>
      ${bg.trayAvailable
        ? `<p class="hint">To quit completely, right-click the TheTracker icon in the tray and choose Quit, or <button class="link" data-act="quit" type="button">quit now</button>.</p>`
        : `<div class="note warn">The tray icon couldn't be created on this PC, so closing the window quits the app.</div>`}
    </section>
    <section class="set">
      <h3>Your data</h3>
      <p class="muted">Everything is stored on this PC, in <code>${esc(S.boot.dataDir)}</code>. <button class="link" data-act="reveal" data-path="${esc(S.boot.dataDir + "\\history.json")}" type="button">Show the folder</button></p>
      <div class="row wrap"><button class="btn" data-act="backup" type="button">Back up now</button>
        <span class="muted">Saves your sessions, settings, builds and linked accounts as one file in ${esc(S.boot.exportDir)}.</span></div>
      ${SET.backupPath ? `<div class="note">Saved to ${esc(SET.backupPath)} <button class="link" data-act="reveal" data-path="${esc(SET.backupPath)}" type="button">Show in folder</button></div>` : ""}
      ${backups.length ? `<h4>Restore a backup</h4><div class="lines">${backups.map((p) => `<div class="line static"><span class="grow">Backup from ${esc(name(p))}</span>
          <button class="link ${SET.restoreArmed === p ? "danger" : ""}" data-act="restore" data-path="${esc(p)}" type="button">${SET.restoreArmed === p ? "Press again to replace your current data" : "Restore"}</button></div>`).join("")}</div>
          <p class="hint">Restoring replaces your current sessions and settings with the backup's. What you have now is saved first, so it can be undone.</p>` : ""}
    </section>`;
}

// ---------- Overlay ----------

async function saveOverlay(patch) {
  const next = { ...S.boot.prefs.overlay, ...patch, dota: { ...S.boot.prefs.overlay.dota, ...(patch.dota || {}) } };
  const prefs = await attempt(() => invoke("save_overlay_settings", next));
  if (prefs) S.boot.prefs = prefs;
  rerender();
}

onChange("ov-range", (el) => {
  // The label follows the slider as it moves; the save happens on release.
  const out = document.getElementById(el.id + "Val");
  if (out) out.textContent = el.dataset.unit === "%" ? `${Math.round(el.value * 100)}%` : `${el.value} s`;
});
onChange("ov-save", (el) => saveOverlay({ [el.dataset.key]: el.type === "checkbox" ? el.checked : el.type === "range" ? Number(el.value) : el.value }));
onChange("ov-panel", (el) => saveOverlay({ dota: { [el.dataset.key]: el.checked } }));
act("ov-corner", (el) => saveOverlay({ corner: el.dataset.corner }));
act("ov-test", async () => {
  if (await attempt(() => invoke("sim_start", { seconds: 90 }))) {
    await attempt(() => invoke("overlay_show"));
    S.overlayVisible = true;
    paintTop();
    toast("Test match running for 90 seconds. Reminders appear on the overlay as events come up.");
    pollLive();
  }
});

function monitorLabel(m, i) {
  return `Display ${i + 1} (${m.width} × ${m.height})${m.primary ? ", main" : ""}`;
}

function overlayHtml() {
  const o = S.boot.prefs.overlay;
  const monitors = SET.monitors || [];
  const corners = [["top-left", "Top left"], ["top-right", "Top right"], ["bottom-left", "Bottom left"], ["bottom-right", "Bottom right"]];
  const sim = S.live && S.live.simulating;
  return `<section class="set">
      <h3>In-game overlay</h3>
      <p class="muted">A small transparent window over Dota that counts down to rune spawns, lotuses and the stack pull, and shows nothing the rest of the time. It works from the game clock only: nothing an opponent is doing, nothing you couldn't work out yourself. Dota must be in borderless or windowed mode for any overlay to show on top of it.</p>
      <label class="switch"><input type="checkbox" data-change="ov-save" data-key="auto" ${o.auto ? "checked" : ""} /><span>Show it when a match starts and hide it when the match ends</span></label>
      <label class="switch"><input type="checkbox" data-change="ov-save" data-key="clickThrough" ${o.clickThrough ? "checked" : ""} /><span>Let clicks pass through it to the game <span class="muted">(recommended)</span></span></label>
      <div class="row wrap"><button class="btn" data-act="${sim ? "sim-stop" : "ov-test"}" type="button">${sim ? "Stop the test match" : "Test it now"}</button>
        <span class="muted">Runs a short fake match so you can see the reminders and check the position. Nothing is saved.</span></div>
    </section>
    <section class="set">
      <h3>Reminders</h3>
      <label class="switch"><input type="checkbox" data-change="ov-panel" data-key="runes" ${o.dota.runes ? "checked" : ""} /><span>Runes <span class="muted">(bounty, water, power and wisdom)</span></span></label>
      <label class="switch"><input type="checkbox" data-change="ov-panel" data-key="lotus" ${o.dota.lotus ? "checked" : ""} /><span>Healing lotus</span></label>
      <label class="switch"><input type="checkbox" data-change="ov-panel" data-key="stacks" ${o.dota.stacks ? "checked" : ""} /><span>Stack pull <span class="muted">(at :53 each minute)</span></span></label>
      <label class="field"><span>Warn me <b id="ovLeadVal">${o.leadSeconds} s</b> before each one</span>
        <input type="range" id="ovLead" min="3" max="30" step="1" value="${o.leadSeconds}" data-input="ov-range" data-unit="s" data-change="ov-save" data-key="leadSeconds" /></label>
    </section>
    <section class="set">
      <h3>Position and size</h3>
      <div class="field"><span>Corner</span><div class="corner-grid">${corners.map(([k, l]) => `<button class="chip ${o.corner === k ? "on" : ""}" data-act="ov-corner" data-corner="${k}" type="button">${l}</button>`).join("")}</div></div>
      <label class="field"><span>Display</span><select class="input" data-change="ov-save" data-key="monitor">
        <option value="" ${o.monitor ? "" : "selected"}>The one TheTracker's window is on</option>
        ${monitors.map((m, i) => `<option value="${esc(m.name)}" ${o.monitor === m.name ? "selected" : ""}>${esc(monitorLabel(m, i))}</option>`).join("")}</select></label>
      <label class="field"><span>Size <b id="ovScaleVal">${Math.round(o.scale * 100)}%</b></span>
        <input type="range" id="ovScale" min="0.75" max="1.5" step="0.05" value="${o.scale}" data-input="ov-range" data-unit="%" data-change="ov-save" data-key="scale" /></label>
      <label class="field"><span>Opacity <b id="ovOpacityVal">${Math.round(o.opacity * 100)}%</b></span>
        <input type="range" id="ovOpacity" min="0.25" max="1" step="0.05" value="${o.opacity}" data-input="ov-range" data-unit="%" data-change="ov-save" data-key="opacity" /></label>
    </section>`;
}

// ---------- Account ----------

act("profile-save", async () => {
  const p = { username: $("#profName").value, rank: $("#profRank").value || null, role: $("#profRole").value || null };
  const saved = await attempt(() => invoke("save_profile", p), "Profile saved.");
  if (saved) S.boot.profile = saved;
  rerender();
});
act("auth-flow", (el) => {
  SET.authFlow = el.dataset.flow;
  SET.authError = null;
  rerender();
});
act("auth-submit", async () => {
  const email = $("#authEmail").value, password = $("#authPassword").value;
  SET.authBusy = true;
  SET.authError = null;
  rerender();
  try {
    S.boot.auth = await invoke("sign_in", { email, password, flow: SET.authFlow });
    toast(SET.authFlow === "signUp" ? "Account created. Your sessions are syncing." : "Signed in. Your sessions are syncing.");
  } catch (e) {
    SET.authError = e.message;
  }
  SET.authBusy = false;
  pollSlow();
  rerender();
});
act("sign-out", async () => {
  const auth = await attempt(() => invoke("sign_out"), "Signed out. Your sessions stay on this PC.");
  if (auth) S.boot.auth = auth;
  pollSlow();
  rerender();
});
act("sync-all", async () => {
  const out = await attempt(() => invoke("sync_all"));
  if (out) toast(out.queued ? `Syncing ${out.queued} sessions…` : "Nothing to sync yet.");
  pollSlow();
});
act("cloud-delete", async (el) => {
  if (el.dataset.confirm !== "yes") {
    el.dataset.confirm = "yes";
    el.textContent = "Press again to delete everything you've published";
    return;
  }
  const out = await attempt(() => invoke("delete_cloud_data"));
  if (out) toast(`Deleted ${out.deleted} published sessions. Your local copies are untouched.`);
  rerender();
});
act("unlink", async (el) => {
  const game = el.dataset.game;
  const link = await attempt(() => invoke(game === "deadlock" ? "deadlock_unlink" : "dota_unlink"), "Account disconnected.");
  if (!link) return;
  if (game === "deadlock") {
    S.boot.deadlockLink = link;
    dlOverview.clear();
  } else {
    S.boot.dotaLink = link;
    dHist.clear();
    dPlayer.clear();
  }
  rerender();
});

function accountHtml() {
  const p = S.boot.profile, auth = S.boot.auth, sync = S.sync || {};
  const linkRow = (game, label, link) => `<div class="line static">${imgHtml(link.avatar, "avatar small")}
    <span class="grow"><b>${label}</b><span class="sub">${link.accountId ? `${esc(link.personaname || "")} (Steam account ${link.accountId})` : "Not connected"}</span></span>
    ${link.accountId ? `<button class="link" data-act="unlink" data-game="${game}" type="button">Disconnect</button>` : `<button class="link" data-act="go" data-view="${game === "deadlock" ? "dl-overview" : "overview"}" type="button">Connect</button>`}</div>`;
  return `<section class="set">
      <h3>Steam accounts</h3>
      <p class="muted">Which Steam account each game's match history is read for.</p>
      <div class="lines">${linkRow("dota", "Dota 2", S.boot.dotaLink)}${linkRow("deadlock", "Deadlock", S.boot.deadlockLink)}</div>
    </section>
    <section class="set">
      <h3>Leaderboard profile</h3>
      <p class="muted">The name shown beside your games on the shared leaderboard.</p>
      <div class="form-grid">
        <label class="field"><span>Display name</span><input class="input" id="profName" type="text" maxlength="40" value="${esc(p.username)}" placeholder="Your name" /></label>
        <label class="field"><span>Rank</span><select class="input" id="profRank"><option value="">Not set</option>${RANKS.map((r) => `<option ${p.rank === r ? "selected" : ""}>${r}</option>`).join("")}</select></label>
        <label class="field"><span>Role</span><select class="input" id="profRole"><option value="">Not set</option>${ROLES.map((r) => `<option ${p.role === r ? "selected" : ""}>${r}</option>`).join("")}</select></label>
      </div>
      <button class="btn" data-act="profile-save" type="button">Save profile</button>
    </section>
    <section class="set">
      <h3>TheTracker account</h3>
      ${auth.signedIn ? `
        <p class="muted">Signed in as <b>${esc(auth.email || "")}</b>. Recorded sessions are published to the shared leaderboard as they finish.</p>
        <p class="muted">${sync.pending ? `Syncing ${sync.pending}…` : sync.lastError ? `<span class="loss">Last sync failed: ${esc(sync.lastError)}</span>` : sync.lastSync ? `Last synced at ${esc(sync.lastSync)}.` : "Up to date."}${sync.synced ? ` ${sync.synced} sent this session.` : ""}</p>
        <div class="row wrap"><button class="btn" data-act="sync-all" type="button">Sync everything now</button><button class="btn ghost" data-act="sign-out" type="button">Sign out</button>
          <span class="grow"></span><button class="link danger" data-act="cloud-delete" type="button">Delete what I've published</button></div>`
      : `
        <p class="muted">Optional. An account publishes your recorded sessions to the shared leaderboard. Without one, everything stays on this PC and nothing is sent anywhere.</p>
        ${auth.lastError ? `<div class="note warn">${esc(auth.lastError)}</div>` : ""}
        <div class="chips"><button class="chip ${SET.authFlow === "signIn" ? "on" : ""}" data-act="auth-flow" data-flow="signIn" type="button">Sign in</button><button class="chip ${SET.authFlow === "signUp" ? "on" : ""}" data-act="auth-flow" data-flow="signUp" type="button">Create an account</button></div>
        <div class="form-grid">
          <label class="field"><span>Email</span><input class="input" id="authEmail" type="email" autocomplete="username" data-enter="auth-submit" /></label>
          <label class="field"><span>Password${SET.authFlow === "signUp" ? " (at least 8 characters)" : ""}</span><input class="input" id="authPassword" type="password" autocomplete="${SET.authFlow === "signUp" ? "new-password" : "current-password"}" data-enter="auth-submit" /></label>
        </div>
        ${SET.authError ? `<div class="note err">${esc(SET.authError)}</div>` : ""}
        <button class="btn" data-act="auth-submit" type="button" ${SET.authBusy ? "disabled" : ""}>${SET.authBusy ? "Working…" : SET.authFlow === "signUp" ? "Create account" : "Sign in"}</button>`}
    </section>`;
}

// ---------- About ----------

act("update-check", () => checkForUpdate(false));
act("diag-run", async () => {
  SET.diagRunning = true;
  rerender();
  try {
    SET.diag = await invoke("run_diagnostics");
  } catch (e) {
    toast(e.message, "err");
  }
  SET.diagRunning = false;
  rerender();
});

function aboutHtml() {
  const u = S.update;
  let status = `<p class="muted">Checking for updates…</p>`;
  if (S.updateError) status = `<div class="note warn">${esc(S.updateError)}</div>`;
  else if (u && u.available) {
    status = `<div class="note"><b>Version ${esc(u.version)} is available.</b>${u.notes ? `<p class="pre">${esc(u.notes)}</p>` : ""}
      <button class="btn" data-act="install-update" type="button" ${S.updating ? "disabled" : ""}>${S.updating ? "Downloading…" : "Update and restart"}</button>
      <p class="hint">The app closes, updates and reopens. Don't do it mid-match: the game in progress wouldn't be recorded.</p></div>`;
  } else if (u) status = `<p class="muted">You're on the latest version.</p>`;

  return `<section class="set">
      <h3>TheTracker ${esc(S.boot.version)}</h3>
      ${status}
      <div class="row"><button class="btn ghost" data-act="update-check" type="button">Check for updates</button>
        <button class="link" data-act="open-url" data-url="https://github.com/lilcham1/TheTracker/releases" type="button">Release notes</button></div>
    </section>
    <section class="set">
      <h3>Is everything working?</h3>
      <p class="muted">Checks each thing the app depends on and says which are fine.</p>
      <button class="btn ghost" data-act="diag-run" type="button" ${SET.diagRunning ? "disabled" : ""}>${SET.diagRunning ? "Checking…" : "Run the check"}</button>
      ${SET.diag ? `<div class="checks">${SET.diag.map((c) => setupRow(c.ok, esc(c.name), esc(c.detail) + (known(c.latencyMs) && c.ok ? ` <span class="muted">(${c.latencyMs} ms)</span>` : ""))).join("")}</div>` : ""}
    </section>
    <section class="set">
      <h3>Where the data comes from</h3>
      <p class="muted">Live Dota tracking uses Valve's own Game State Integration, which Dota sends to this PC only. Dota match history comes from the public OpenDota API. Deadlock comes from the community-run Deadlock API. TheTracker never reads either game's memory, and never shows anything about another player that the game doesn't already show you.</p>
    </section>`;
}

view("settings", {
  game: null, title: "Settings", live: true,
  sub: () => "",
  load() {
    const tab = S.params.tab || "general";
    if (tab === "overlay" && !SET.monitors) invoke("list_monitors").then((m) => { SET.monitors = m; rerender(); }).catch(() => {});
    if (tab === "general") invoke("list_backups").then((b) => { SET.backups = b; rerender(); }).catch(() => {});
    if (tab === "about" && (!S.updateChecked || Date.now() - S.updateChecked > 60000)) checkForUpdate(true);
    if (tab === "account") pollSlow();
    return invoke("background_settings").then((bg) => { S.boot.background = bg; rerender(); }).catch(() => {});
  },
  render() {
    const tab = S.params.tab || "general";
    const body = { general: generalHtml, overlay: overlayHtml, account: accountHtml, about: aboutHtml }[tab] || generalHtml;
    return `<div class="tabs" role="tablist">${SETTINGS_TABS.map(([k, l]) => `<button class="tab ${tab === k ? "on" : ""}" data-act="settings-tab" data-tab="${k}" role="tab" aria-selected="${tab === k}" type="button">${l}</button>`).join("")}</div>
      <div class="settings">${body()}</div>`;
  },
});

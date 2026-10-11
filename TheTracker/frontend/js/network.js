// Connection: ping to the router and to Valve, line use and programs online,
// measured while a match runs or during a short test from Settings. It only
// measures; nothing on the PC is changed.

S.net = null;

const NET_VERDICT = {
  measuring: ["idle", "Measuring"],
  ok: ["ok", "Connection fine"],
  local: ["bad", "Home network"],
  internet: ["bad", "Internet provider"],
  busy: ["bad", "Line busy"],
};

const msText = (v) => (v < 1 ? "<1 ms" : `${Math.round(v)} ms`);

function pingCell(label, st, where) {
  if (!st || !st.samples) return `<div class="net-cell"><div class="stat-label">${label}</div><div class="muted">–</div></div>`;
  const bad = st.loss >= 3 || st.jitter >= 15;
  return `<div class="net-cell"><div class="stat-label">${label}</div>
    <div class="display ${bad ? "loss" : ""}">${st.avg || st.loss < 100 ? msText(st.avg) : "no reply"}</div>
    <div class="muted">${st.jitter ? `±${Math.round(st.jitter)} ms` : "steady"}${st.loss ? `, <span class="loss">${st.loss}% lost</span>` : ""}${where ? `<br>${esc(where)}` : ""}</div></div>`;
}

/// The connection block: verdict, pings, line use and programs online.
function netHtml(s, { compact = false } = {}) {
  if (!s) return "";
  const [tone, word] = NET_VERDICT[s.verdict] || NET_VERDICT.measuring;
  return `<div class="net ${compact ? "compact" : ""}">
    <div class="net-head"><span class="check-dot ${tone}"></span><b>${word}</b><span class="muted"> ${esc(s.detail || "")}</span></div>
    <div class="net-grid">
      ${pingCell("Your router", s.router, s.routerIp)}
      ${pingCell("Valve", s.valve, s.valveWhere)}
      <div class="net-cell"><div class="stat-label">Line use now</div><div class="display">${s.downMbps.toFixed(1)} <span class="muted small">down</span></div><div class="muted">${s.upMbps.toFixed(1)} Mbit/s up</div></div>
    </div>
    ${!compact && s.apps.length ? `<p class="hint">Programs online now (open connections, not how much they use): ${s.apps.map((a) => `<b>${esc(a.name)}</b> ${a.connections}`).join(", ")}.</p>` : ""}
  </div>`;
}

/// On the live match panel.
function netBlockHtml() {
  if (!S.net || !S.net.active) return "";
  return `<div class="sec-head"><h3>Connection</h3></div>${netHtml(S.net, { compact: true })}`;
}

// ---------- Game mode ----------

const GM = { data: null, picked: null, busy: false, error: null };

async function loadGameMode() {
  try {
    GM.data = await invoke("game_mode");
  } catch (e) {
    GM.error = e.message;
  }
  if (!GM.picked && GM.data) GM.picked = new Set(GM.data.status.categories.length ? GM.data.status.categories : GM.data.catalogue.map((c) => c.id));
  rerender();
}

function gameModeHtml() {
  if (!GM.data) return `<section class="set"><h3>Game mode</h3><p class="muted">Loading…</p></section>`;
  const st = GM.data.status;
  const set = st.state !== "not_set_up";
  const saved = new Set(st.categories);
  const changed = set && (GM.picked.size !== saved.size || [...GM.picked].some((c) => !saved.has(c)));
  const stateLine = st.state === "on"
    ? `<span class="check-dot ok"></span><b>On for this match</b> <span class="muted">${st.policies - 1} programs slowed, Dota marked priority${st.updatePaused ? ", Windows Update sharing paused" : ""}.</span>`
    : set ? `<span class="check-dot idle"></span><b>Ready</b> <span class="muted">Switches on when a match starts and off when it ends.</span>`
    : `<span class="check-dot idle"></span><b>Off</b>`;
  return `<section class="set">
      <h3>Game mode</h3>
      <p class="muted">During a Dota match, slows other programs' <b>uploads</b> so the game's packets don't wait behind them, and marks Dota's traffic as priority. Undone when the match ends, and Windows clears it at every restart anyway. Windows can't slow downloads from your PC (Steam already pauses its own while you play), and the priority mark only helps if your router honours it. Discord and voice apps are never touched.</p>
      <div class="net-head">${stateLine}</div>
      <div class="gm-cats">${GM.data.catalogue.map((c) => `<label class="switch"><input type="checkbox" data-change="gm-cat" data-cat="${c.id}" ${GM.picked.has(c.id) ? "checked" : ""} ${GM.busy ? "disabled" : ""} />
        <span>${esc(c.label)} <span class="muted">${c.mbps ? `(uploads capped at ${c.mbps} Mbit/s: ${esc(c.apps.join(", "))})` : "(paused for the match)"}</span></span></label>`).join("")}</div>
      ${GM.error ? `<div class="note err">${esc(GM.error)}</div>` : ""}
      <div class="row wrap">
        ${!set ? `<button class="btn" data-act="gm-setup" type="button" ${GM.busy || !GM.picked.size ? "disabled" : ""}>${GM.busy ? "Waiting for Windows…" : "Turn on Game mode"}</button>` : ""}
        ${set && changed ? `<button class="btn" data-act="gm-setup" type="button" ${GM.busy || !GM.picked.size ? "disabled" : ""}>${GM.busy ? "Waiting for Windows…" : "Save changes"}</button>` : ""}
        ${set ? `<button class="btn ghost" data-act="gm-remove" type="button" ${GM.busy ? "disabled" : ""}>Turn off Game mode</button>` : ""}
      </div>
      <p class="hint">Windows asks for your approval once when you turn it on or change it. After that TheTracker switches it on and off by itself, with no prompts or windows over the game.</p>
    </section>`;
}

onChange("gm-cat", (el) => {
  if (el.checked) GM.picked.add(el.dataset.cat);
  else GM.picked.delete(el.dataset.cat);
  rerender();
});
act("gm-setup", async () => {
  GM.busy = true;
  GM.error = null;
  rerender();
  try {
    const status = await invoke("game_mode_setup", { categories: [...GM.picked] });
    GM.data.status = status;
    toast("Game mode is on. It switches on by itself when your next match starts.");
  } catch (e) {
    GM.error = e.message;
  }
  GM.busy = false;
  rerender();
});
act("gm-remove", async () => {
  GM.busy = true;
  GM.error = null;
  rerender();
  try {
    GM.data.status = await invoke("game_mode_remove");
    toast("Game mode is off and removed.");
  } catch (e) {
    GM.error = e.message;
  }
  GM.busy = false;
  rerender();
});

function networkHtml() {
  const s = S.net;
  const testing = s && s.testUntil && s.testUntil * 1000 > Date.now();
  return `<section class="set">
      <h3>Connection</h3>
      <p class="muted">While a Dota match runs, TheTracker pings your router and Valve's nearest server once a second and watches how busy your line is, then says where any lag comes from: your Wi-Fi or home network, your internet provider, or other programs using the line. It only measures; nothing on your PC is changed. It also shows on Overview during a match.</p>
      <div class="row wrap"><button class="btn" data-act="net-test" type="button" ${testing ? "disabled" : ""}>${testing ? "Testing…" : "Test my connection"}</button>
        <span class="muted">20 seconds. Try it while something is uploading to see the difference.</span></div>
      ${s && (s.router || s.valve) ? netHtml(s) : ""}
    </section>
    <section class="set">
      <h3>Lag tips</h3>
      <ul class="tips">
        <li>A cable instead of Wi-Fi removes most "home network" lag.</li>
        <li>Pause cloud sync (OneDrive, Google Drive, Dropbox) and big uploads while you play: a full upload slows every packet, the game's too.</li>
        <li>Steam already pauses its downloads while a game runs. Other launchers may not.</li>
        <li>If your router has QoS or "SQM", turning it on keeps game traffic quick even when the line is busy.</li>
      </ul>
    </section>
    ${gameModeHtml()}`;
}

async function pollNet() {
  try {
    S.net = await invoke("net_status");
  } catch (_) {
    return;
  }
  if (S.view === "overview") {
    const slot = $("#liveSlot");
    if (slot && VIEWS.overview.liveSlot) slot.innerHTML = VIEWS.overview.liveSlot();
  } else if (S.view === "settings" && S.params.tab === "network") rerender();
}

act("net-test", async () => {
  const s = await attempt(() => invoke("net_test"));
  if (s) S.net = s;
  rerender();
});

// Poll only while something shows it.
setInterval(() => {
  if (document.visibilityState !== "visible") return;
  const onOverview = S.view === "overview" && S.live && S.live.live;
  const onSettings = S.view === "settings" && S.params.tab === "network";
  if (onOverview || onSettings) pollNet();
}, 2000);

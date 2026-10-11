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
    </section>`;
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

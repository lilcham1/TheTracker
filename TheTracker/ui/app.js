// Dota Tracker frontend. Plain JS, no framework/bundler — Tauri serves this
// folder as-is. Talks to the Rust backend exclusively through `invoke`
// (see src-tauri/src/main.rs for the command list).

const HERO_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/";
const ITEM_CDN = "https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/";
const CHECKPOINT_MINUTES = [5, 10, 15, 20, 25];

const GAME_TYPES = [
  { id: "ranked", label: "Ranked" },
  { id: "all_pick", label: "All Pick" },
  { id: "turbo", label: "Turbo" },
  { id: "other", label: "Other" },
];

const LB_METRICS = [
  { key: "last_hits_25", label: "Most LH @25m", higherBetter: true },
  { key: "fewest_deaths", label: "Fewest Deaths", higherBetter: false },
  { key: "least_gold_lost", label: "Least Gold Lost", higherBetter: false },
  { key: "most_kills", label: "Most Kills", higherBetter: true },
];

const HERO_NAME_OVERRIDES = {
  antimage: "Anti-Mage",
  nevermore: "Shadow Fiend",
  windrunner: "Windranger",
  vengefulspirit: "Vengeful Spirit",
  queenofpain: "Queen of Pain",
  skeleton_king: "Wraith King",
  doom_bringer: "Doom",
  necrolyte: "Necrophos",
  furion: "Nature's Prophet",
  life_stealer: "Lifestealer",
  rattletrap: "Clockwerk",
  obsidian_destroyer: "Outworld Destroyer",
  treant: "Treant Protector",
  wisp: "Io",
  zuus: "Zeus",
  shredder: "Timbersaw",
  magnataur: "Magnus",
  centaur: "Centaur Warrunner",
  abyssal_underlord: "Underlord",
  keeper_of_the_light: "Keeper of the Light",
};

// ---------- Tauri bridge ----------

/// Every backend call goes through here.
///
/// Opened as a plain webpage there is no backend, and this used to answer
/// with several hundred lines of fixtures so the layout could be eyeballed
/// in a browser. They were deleted: fixtures drift from the shapes the Rust
/// side actually returns, and a page that renders plausible fake data is
/// worse than one that admits it has none.
const invoke = window.__TAURI__
  ? window.__TAURI__.core.invoke
  : (cmd) => Promise.reject(new Error(`No backend for "${cmd}" — TheTracker has to run as the app.`));

// ---------- Small helpers ----------

function gameTypeLabel(t) {
  const found = GAME_TYPES.find((g) => g.id === t) || (t === "unranked" ? GAME_TYPES[1] : null);
  return found ? found.label : "Unspecified";
}

function heroCleanName(raw) {
  if (!raw) return null;
  const clean = raw.startsWith("npc_dota_hero_") ? raw.slice("npc_dota_hero_".length) : raw;
  return clean.length ? clean : null;
}

function titleCase(clean) {
  return clean
    .split("_")
    .map((w) => (w.length ? w[0].toUpperCase() + w.slice(1) : w))
    .join(" ");
}

function heroDisplayName(raw) {
  const clean = heroCleanName(raw);
  if (!clean) return "Unknown Hero";
  return HERO_NAME_OVERRIDES[clean] || titleCase(clean);
}

function heroPortraitUrl(raw) {
  const clean = heroCleanName(raw);
  return clean ? `${HERO_CDN}${clean}.png` : null;
}

function fmtClock(seconds) {
  if (seconds === null || seconds === undefined) return "??:??";
  const neg = seconds < 0;
  const abs = Math.floor(Math.abs(seconds));
  const m = Math.floor(abs / 60);
  const s = abs % 60;
  return `${neg ? "-" : ""}${m}:${String(s).padStart(2, "0")}`;
}

function formatDate(iso) {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

function formatNum(v) {
  if (v === null || v === undefined) return "—";
  return Math.abs(v - Math.round(v)) < 1e-9 ? String(Math.round(v)) : v.toFixed(1);
}

function roshanDrops(deathCount) {
  if (deathCount <= 1) return "Aegis of the Immortal";
  if (deathCount === 2) return "Aegis + Cheese + (Refresher Shard or Aghanim's Blessing)";
  return "Aegis + Cheese + Aghanim's Blessing + Refresher Shard";
}

/// "black_king_bar" -> "Black King Bar", for image tooltips/fallback text.
function itemDisplayName(item) {
  return titleCase(item);
}

/// Every image failure in the app, handled in one place.
///
/// CDN icons 404 from time to time — renamed items, a wrong slug, or simply
/// no connection. Every `<img>` in the app used to carry its own
/// `onerror="…"` attribute, and not one of them ever ran: the CSP is
/// `script-src 'self'`, which blocks inline handlers, so a failed icon
/// showed the browser's broken-image glyph instead of the intended
/// fallback. That is also how three wrong item slugs went unnoticed.
///
/// `error` does not bubble, but it does capture, so a single listener on
/// the document catches every image — including ones rendered long after
/// this runs, which per-element wiring kept missing.
document.addEventListener(
  "error",
  (e) => {
    const img = e.target;
    if (!(img instanceof HTMLImageElement) || img.dataset.failed) return;
    img.dataset.failed = "1";

    // An item icon becomes its initials, which still identifies it.
    const item = img.dataset.itemImg;
    if (item) {
      const tile = document.createElement("div");
      tile.className = "item-fallback";
      tile.title = itemDisplayName(item);
      tile.textContent = itemDisplayName(item)
        .split(" ")
        .map((w) => w[0])
        .join("")
        .slice(0, 3);
      img.replaceWith(tile);
      return;
    }

    // A hero portrait keeps its footprint so the row stays aligned.
    if (img.classList.contains("hero-portrait")) {
      img.classList.add("portrait-missing");
      img.removeAttribute("src");
      return;
    }

    // Anything else just gets out of the way.
    img.style.visibility = "hidden";
  },
  true
);

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---------- Steam auto-detect ----------
//
// Reads only the signed-in Steam account's id and display name (see
// steam.rs for exactly what is and isn't touched — no credentials, no
// tokens). Shared by both link screens.

const APP_VERSION = "0.12.0";

const STEAM_DETECT = { accounts: [], tried: false, busy: false, error: null };

async function steamDetect(onDone) {
  STEAM_DETECT.busy = true;
  STEAM_DETECT.error = null;
  if (onDone) onDone();
  try {
    STEAM_DETECT.accounts = await invoke("steam_accounts");
    if (!STEAM_DETECT.accounts.length) {
      STEAM_DETECT.error =
        "No Steam account found on this PC. Sign in to Steam once, or search by name instead.";
    }
  } catch (e) {
    STEAM_DETECT.error = String(e);
  }
  STEAM_DETECT.tried = true;
  STEAM_DETECT.busy = false;
  if (onDone) onDone();
}

function steamDetectHtml() {
  const rows = STEAM_DETECT.accounts
    .map(
      (a) => `
      <div class="result-row" data-steam-pick="${a.accountId}" data-steam-name="${escapeHtml(a.personaname || "")}">
        <div class="grow">
          <div class="result-name">${escapeHtml(a.personaname || `Account ${a.accountId}`)}</div>
          <div class="result-id">${a.accountId} · ${escapeHtml(a.source)}</div>
        </div>
        <span class="badge badge-brand">Use this</span>
      </div>`
    )
    .join("");

  return `
    <div class="row">
      <button class="btn btn-secondary" id="steamDetectBtn" type="button" ${STEAM_DETECT.busy ? "disabled" : ""}>
        ${STEAM_DETECT.busy ? "Checking…" : "Detect from Steam"}
      </button>
      <span class="hint">Reads your Steam account ID only — never passwords or tokens.</span>
    </div>
    ${STEAM_DETECT.error ? `<div class="note warn">${escapeHtml(STEAM_DETECT.error)}</div>` : ""}
    ${rows ? `<div class="col">${rows}</div>` : ""}`;
}

/// `onPick(accountId, personaname)` links the account for whichever game
/// the calling screen belongs to.
function wireSteamDetect(root, rerender, onPick) {
  const btn = root.querySelector("#steamDetectBtn");
  if (btn) btn.addEventListener("click", () => steamDetect(rerender));

  root.querySelectorAll("[data-steam-pick]").forEach((el) =>
    el.addEventListener("click", () =>
      onPick(Number(el.dataset.steamPick), el.dataset.steamName || `Account ${el.dataset.steamPick}`)
    )
  );
}

// ---------- App state ----------

const state = {
  view: "live",
  game: "dota",
  prefs: { favorites: {}, builds: [], overlay: {} },
  dlTab: "dloverview",
  overlay: { visible: false, clickThrough: true, error: null },
  tab: "live",
  live: null,
  history: [],
  profile: { username: "", rank: null, role: null },
  profileDraft: { username: "", rank: null, role: null },
  lbType: "all",
  lbMetric: "last_hits_25",
  lbScope: "personal", // "personal" = local history, "global" = Convex
  lbGlobal: { rows: [], loading: false, error: null },
  deviceId: null,
  sync: null,
  auth: { signedIn: false, email: null },
  authForm: { email: "", password: "", error: null, busy: false },
  openHistory: new Set(),
  openTypeMenu: null,
};

// ---------- Rendering: Live tab ----------

function renderLive() {
  const root = document.getElementById("tab-live");
  const m = state.live && state.live.current;

  if (!m) {
    root.innerHTML = `<div class="empty-state">Waiting for a match to start&hellip;<br/>Launch Dota 2 with GSI enabled to begin tracking.</div>`;
    return;
  }

  const portrait = heroPortraitUrl(m.heroName);
  const parts = [];

  if (m.ended && m.summary) {
    parts.push(summaryCardHtml(m.summary));
  }

  parts.push(`
    <div class="live-hero">
      ${portrait ? `<img class="hero-portrait" src="${portrait}" alt="" />` : `<div class="hero-portrait"></div>`}
      <div>
        <p class="live-hero-name">${escapeHtml(heroDisplayName(m.heroName))}</p>
        <span class="live-clock">${fmtClock(m.lastClockTime)}</span>
      </div>
    </div>

    <div class="chip-row" id="liveGameTypeRow">
      ${GAME_TYPES.map(
        (g) => `<button class="chip ${m.gameType === g.id ? "selected" : ""}" data-live-type="${g.id}">${g.label}</button>`
      ).join("")}
    </div>

    <div class="stat-row">
      <div class="stat">
        <div class="stat-label">Last Hits / Denies</div>
        <div class="stat-value">${m.lastHits} / ${m.denies}</div>
      </div>
      <div class="stat">
        <div class="stat-label">Deaths / Gold Lost</div>
        <div class="stat-value loss">${m.deaths.length} / ${totalGoldLost(m.deaths)}g</div>
      </div>
    </div>

    ${roshanCardHtml(m.roshan, m.lastClockTime)}

    <div class="card">
      <p class="section-title">Last Hit Checkpoints</p>
      ${checkpointsRowHtml(m.checkpoints)}
    </div>

    <div class="card">
      <p class="section-title">Key Items</p>
      ${itemGridHtml(m.keyItemLog)}
    </div>

    <div class="card">
      <p class="section-title">Deaths</p>
      ${deathListHtml(m.deaths)}
    </div>
  `);

  root.innerHTML = parts.join("");

  root.querySelectorAll("[data-live-type]").forEach((btn) => {
    btn.addEventListener("click", () => {
      invoke("set_live_game_type", { gameType: btn.dataset.liveType }).then(refreshLive);
    });
  });

  const roshBtn = root.querySelector("[data-mark-roshan]");
  if (roshBtn) {
    roshBtn.addEventListener("click", () => {
      invoke("mark_roshan_death").then(refreshLive);
    });
  }
}

function totalGoldLost(deaths) {
  return deaths.reduce((sum, d) => sum + (d.goldLost ?? 0), 0);
}

function roshanCardHtml(roshan, clockTime) {
  let statusClass = "alive";
  let status = "Roshan — Alive";
  let sub = "No deaths recorded yet this game";

  if (roshan.lastDeathClock !== null && roshan.lastDeathClock !== undefined) {
    const minRespawn = roshan.lastDeathClock + 480;
    const maxRespawn = roshan.lastDeathClock + 660;
    const countdown = (target) => {
      const rem = Math.max(0, target - clockTime);
      return `${Math.floor(rem / 60)}:${String(Math.floor(rem % 60)).padStart(2, "0")}`;
    };
    if (clockTime < minRespawn) {
      statusClass = "dead";
      status = "Roshan — Dead";
      sub = `Respawn window opens in ${countdown(minRespawn)}`;
    } else if (clockTime < maxRespawn) {
      statusClass = "maybe";
      status = "Roshan — Maybe Alive";
      sub = `Guaranteed alive in ${countdown(maxRespawn)}`;
    } else {
      status = "Roshan — Alive";
      sub = "Respawn window has passed";
    }
    sub += ` · Death #${roshan.deaths} — drops: ${roshanDrops(roshan.deaths)}`;
  }

  return `
    <div class="card rosh">
      <span class="rosh-icon">\u{1F409}</span>
      <div class="rosh-body grow">
        <div class="rosh-status ${statusClass}">${status}</div>
        <div class="rosh-sub">${sub}</div>
      </div>
      <button class="btn btn-secondary" data-mark-roshan>Mark Death</button>
    </div>
  `;
}

function checkpointsRowHtml(checkpoints) {
  return `
    <div class="cp-row">
      ${CHECKPOINT_MINUTES.map((min) => {
        const cp = checkpoints[min];
        return `
          <div class="cp">
            <div class="cp-min">${min}m</div>
            <div class="cp-val ${cp ? "" : "pending"}">${cp ? cp.lastHits : "—"}</div>
          </div>
        `;
      }).join("")}
    </div>
  `;
}

function itemGridHtml(items) {
  if (!items || items.length === 0) {
    return `<div class="empty-state" style="padding:8px 0;">No key items yet</div>`;
  }
  return `
    <div class="item-grid">
      ${items
        .map(
          (it) => `
        <div class="item-chip" title="${escapeHtml(itemDisplayName(it.item))} — ${escapeHtml(it.clock)}">
          <img src="${ITEM_CDN}${it.item}.png" alt="" data-item-img="${escapeHtml(it.item)}" />
          <span class="">${escapeHtml(it.clock)}</span>
        </div>`
        )
        .join("")}
    </div>
  `;
}

function deathListHtml(deaths) {
  if (!deaths || deaths.length === 0) {
    return `<div class="empty-state" style="padding:8px 0;">No deaths yet</div>`;
  }
  return `
    <div class="death-list">
      ${deaths
        .map(
          (d) => `
        <div class="death-row">
          <span class="clock">${escapeHtml(d.clock)}</span>
          <span class="gold">${d.goldLost !== null && d.goldLost !== undefined ? `-${d.goldLost}g` : "-?g"}</span>
        </div>`
        )
        .join("")}
    </div>
  `;
}

function badgeHtml(verdict) {
  const map = {
    better: ["Better", "badge-win"],
    worse: ["Worse", "badge-loss"],
    similar: ["Average", "badge-neutral"],
  };
  const [text, cls] = map[verdict] || ["New", "badge-neutral"];
  return `<span class="badge ${cls}">${text}</span>`;
}

function compareRowHtml(label, comp) {
  const valStr = comp.value !== null && comp.value !== undefined ? formatNum(comp.value) : "—";
  const avgStr = comp.avg !== null && comp.avg !== undefined ? ` (avg ${formatNum(comp.avg)})` : "";
  return `
    <div class="cmp-row">
      <span class="cmp-label">${label}</span>
      <span class="cmp-value">${valStr}${avgStr}</span>
      ${badgeHtml(comp.verdict)}
      ${comp.isBest ? `<span title="Personal best">\u{1F3C6}</span>` : ""}
    </div>
  `;
}

function comparisonBlockHtml(summary) {
  if (!summary.comparison) return "";
  const cmp = summary.comparison;
  const rows = [compareRowHtml("Deaths", cmp.deaths), compareRowHtml("Gold Lost to Deaths", cmp.goldLost)];
  for (const min of CHECKPOINT_MINUTES) {
    const c = cmp.checkpoints[min];
    if (c && c.value !== null && c.value !== undefined) {
      rows.push(compareRowHtml(`${min} min Last Hits`, c));
    }
  }
  return `
    <div class="summary-sub">vs your last ${summary.gamesComparedAgainst ?? 0} ${gameTypeLabel(summary.gameType)} games</div>
    ${rows.join("")}
  `;
}

function summaryCardHtml(summary) {
  return `
    <div class="card">
      <p class="summary-title">\u{1F3C1} Match Summary — ${gameTypeLabel(summary.gameType)}</p>
      <div class="summary-sub" style="margin-bottom:2px">${escapeHtml(heroDisplayName(summary.heroName))} — ${escapeHtml(summary.duration)}</div>
      ${comparisonBlockHtml(summary)}
    </div>
  `;
}

// ---------- Rendering: History tab ----------

function renderHistory() {
  const root = document.getElementById("tab-history");
  if (state.history.length === 0) {
    root.innerHTML = `<div class="empty-state">No finished matches yet.</div>`;
    return;
  }

  const items = [...state.history].reverse();
  root.innerHTML = items
    .map((m) => {
      const open = state.openHistory.has(m.matchid);
      const portrait = heroPortraitUrl(m.heroName);
      return `
        <div class="history-item ${open ? "open" : ""}">
          <div class="history-head" data-toggle-history="${escapeHtml(m.matchid)}">
            ${portrait ? `<img class="hero-portrait" src="${portrait}" alt="" />` : `<div class="hero-portrait"></div>`}
            <div class="history-head-main">
              <div class="history-head-title">${escapeHtml(heroDisplayName(m.heroName))}</div>
              <div class="history-head-sub">${formatDate(m.date)}</div>
            </div>
            ${typeBadgeHtml(m)}
            <span class="history-chevron">▸</span>
          </div>
          <div class="history-body">
            <div class="history-quickstats">${escapeHtml(m.duration)} · ${m.totalDeaths} deaths · ${m.totalGoldLost}g lost · Rosh x${m.roshanDeaths}</div>
            ${comparisonBlockHtml(m)}
            <div>
              <p class="section-title">Key Items</p>
              ${itemGridHtml(m.keyItems)}
            </div>
            <div>
              <p class="section-title">Deaths</p>
              ${deathListHtml(m.deaths)}
            </div>
          </div>
        </div>
      `;
    })
    .join("");

  root.querySelectorAll("[data-toggle-history]").forEach((headEl) => {
    headEl.addEventListener("click", (e) => {
      if (e.target.closest("[data-type-badge]")) return;
      const id = headEl.dataset.toggleHistory;
      if (state.openHistory.has(id)) state.openHistory.delete(id);
      else state.openHistory.add(id);
      renderHistory();
    });
  });

  wireTypeBadges(root, (matchid, gameType) => {
    invoke("set_history_game_type", { matchid, gameType }).then((newHistory) => {
      state.history = newHistory;
      state.openTypeMenu = null;
      renderHistory();
      renderLeaderboard();
    });
  });
}

function typeBadgeHtml(m) {
  const label = gameTypeLabel(m.gameType);
  const menuOpen = state.openTypeMenu === m.matchid;
  return `
    <div class="type-badge-wrap">
      <button class="type-badge type-${m.gameType}" data-type-badge="${escapeHtml(m.matchid)}">
        ${label} <span class="caret">▾</span>
      </button>
      ${
        menuOpen
          ? `<div class="type-menu" data-type-menu="${escapeHtml(m.matchid)}">
              ${GAME_TYPES.map(
                (g) => `<button data-set-type="${g.id}" class="${g.id === m.gameType ? "current" : ""}">${g.label}</button>`
              ).join("")}
            </div>`
          : ""
      }
    </div>
  `;
}

function wireTypeBadges(root, onSetType) {
  root.querySelectorAll("[data-type-badge]").forEach((btn) => {
    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const id = btn.dataset.typeBadge;
      state.openTypeMenu = state.openTypeMenu === id ? null : id;
      if (root.id === "tab-history") renderHistory();
    });
  });
  root.querySelectorAll("[data-type-menu]").forEach((menu) => {
    const matchid = menu.dataset.typeMenu;
    menu.addEventListener("click", (e) => e.stopPropagation());
    menu.querySelectorAll("[data-set-type]").forEach((opt) => {
      opt.addEventListener("click", () => onSetType(matchid, opt.dataset.setType));
    });
  });
}

// ---------- Rendering: Leaderboard tab ----------

function lbMetricValue(m, key) {
  switch (key) {
    case "last_hits_25":
      return m.checkpoints["25"] ? m.checkpoints["25"].lastHits : null;
    case "fewest_deaths":
      return m.totalDeaths;
    case "least_gold_lost":
      return m.totalGoldLost;
    case "most_kills":
      return m.kills;
    default:
      return null;
  }
}

function lbMetricFmt(key, v) {
  const iv = Math.round(v);
  switch (key) {
    case "last_hits_25":
      return `${iv} LH`;
    case "fewest_deaths":
      return `${iv} deaths`;
    case "least_gold_lost":
      return `${iv}g lost`;
    case "most_kills":
      return `${iv} kills`;
    default:
      return String(iv);
  }
}

const MEDALS = ["\u{1F947}", "\u{1F948}", "\u{1F949}"];

function renderLeaderboard() {
  const root = document.getElementById("tab-leaderboard");
  const metric = LB_METRICS.find((m) => m.key === state.lbMetric);

  const scopeToggle = `
    <div class="scope-toggle">
      <button class="scope ${state.lbScope === "personal" ? "selected" : ""}" data-lb-scope="personal">Your Games</button>
      <button class="scope ${state.lbScope === "global" ? "selected" : ""}" data-lb-scope="global">Global</button>
    </div>
  `;
  const typeChips = `
    <div class="chip-row">
      <button class="chip ${state.lbType === "all" ? "selected" : ""}" data-lb-type="all">All Types</button>
      ${GAME_TYPES.map((g) => `<button class="chip ${state.lbType === g.id ? "selected" : ""}" data-lb-type="${g.id}">${g.label}</button>`).join("")}
    </div>
  `;
  const metricChips = `
    <div class="chip-row">
      ${LB_METRICS.map((m) => `<button class="chip small ${state.lbMetric === m.key ? "selected" : ""}" data-lb-metric="${m.key}">${m.label}</button>`).join("")}
    </div>
  `;

  const list = state.lbScope === "global" ? globalListHtml() : personalListHtml(metric);

  root.innerHTML =
    scopeToggle + typeChips + metricChips + `<div class="lb-list">${list}</div>`;

  root.querySelectorAll("[data-lb-scope]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.lbScope = btn.dataset.lbScope;
      renderLeaderboard();
      if (state.lbScope === "global") loadGlobalLeaderboard();
    });
  });
  root.querySelectorAll("[data-lb-type]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.lbType = btn.dataset.lbType;
      renderLeaderboard();
      if (state.lbScope === "global") loadGlobalLeaderboard();
    });
  });
  root.querySelectorAll("[data-lb-metric]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.lbMetric = btn.dataset.lbMetric;
      renderLeaderboard();
      if (state.lbScope === "global") loadGlobalLeaderboard();
    });
  });
  const retry = root.querySelector("[data-lb-retry]");
  if (retry) retry.addEventListener("click", loadGlobalLeaderboard);
}

function personalListHtml(metric) {
  let ranked = state.history
    .filter((m) => state.lbType === "all" || m.gameType === state.lbType)
    .map((m) => ({ m, v: lbMetricValue(m, state.lbMetric) }))
    .filter((r) => r.v !== null && r.v !== undefined);

  ranked.sort((a, b) => (metric.higherBetter ? b.v - a.v : a.v - b.v));
  ranked = ranked.slice(0, 10);

  if (!ranked.length) return `<div class="empty-state">No games with this stat yet.</div>`;
  return ranked
    .map(
      (r, i) => `
      <div class="lb-row">
        <span class="lb-medal">${MEDALS[i] || ""}</span>
        <span class="lb-rank${i < 3 ? " top" : ""}">#${i + 1}</span>
        <span class="lb-name">${escapeHtml(heroDisplayName(r.m.heroName))}</span>
        <span class="lb-value">${lbMetricFmt(state.lbMetric, r.v)}</span>
      </div>`
    )
    .join("");
}

function globalListHtml() {
  const g = state.lbGlobal;
  if (g.loading) return `<div class="empty-state">Loading global standings…</div>`;
  if (g.error) {
    return `
      <div class="empty-state">
        Couldn't reach the cloud.<br/>
        <span class="error-detail">${escapeHtml(g.error)}</span><br/>
        <button class="btn btn-secondary" style="margin-top:10px" data-lb-retry>Try again</button>
      </div>`;
  }
  if (!g.rows.length) {
    const hint = state.auth.signedIn
      ? "Yours will show up here once a match finishes."
      : "Sign in on the Account page to publish your own games here.";
    return `<div class="empty-state">Nobody has published a game with this stat yet.<br/>${hint}</div>`;
  }
  return g.rows
    .map((r, i) => {
      const isYou = state.auth.signedIn && r.userId === state.auth.userId;
      const who = r.username && r.username.length ? r.username : "Anonymous";
      return `
      <div class="lb-row ${isYou ? "is-you" : ""}">
        <span class="lb-medal">${MEDALS[i] || ""}</span>
        <span class="lb-rank${i < 3 ? " top" : ""}">#${i + 1}</span>
        <span class="lb-name">
          ${escapeHtml(who)}${isYou ? `<span class="you-tag">you</span>` : ""}
          <span class="lb-sub">${escapeHtml(heroDisplayName(r.heroName))} · ${gameTypeLabel(r.gameType)}</span>
        </span>
        <span class="lb-value">${lbMetricFmt(state.lbMetric, r.value)}</span>
      </div>`;
    })
    .join("");
}

async function loadGlobalLeaderboard() {
  state.lbGlobal = { rows: [], loading: true, error: null };
  renderLeaderboard();
  try {
    const rows = await invoke("global_leaderboard", {
      metric: state.lbMetric,
      gameType: state.lbType,
      limit: 10,
    });
    state.lbGlobal = { rows: rows || [], loading: false, error: null };
  } catch (e) {
    state.lbGlobal = { rows: [], loading: false, error: String(e) };
  }
  renderLeaderboard();
}

// ---------- Rendering: Account page ----------

/// The cloud-account section: sign in, or show who is signed in.
///
/// Written as a plain section rather than a card because the rest of the app
/// moved off stacked cards, and an account page made of three floating
/// panels reads as three unrelated things rather than one page.
function accountCardHtml() {
  const a = state.auth || {};
  if (a.signedIn) {
    return `
      <section class="home-section">
        <div class="home-head">
          <h2 class="home-title">Cloud account</h2>
          <div class="home-meta">signed in</div>
          <button class="link-btn" id="signOutBtn" type="button">Sign out</button>
        </div>
        <div class="acct-line">
          <span class="acct-key">Email</span>
          <span class="acct-val good">${escapeHtml(a.email || "—")}</span>
        </div>
        <p class="hint" style="margin-top:10px;max-width:70ch">
          Finished matches publish to the shared leaderboard under this
          account. Everything is written to this PC first either way.
        </p>
      </section>`;
  }
  return `
    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Cloud account</h2>
        <div class="home-meta">signed out</div>
      </div>
      <p class="hint" style="max-width:70ch">
        Tracking works fully signed out, and the shared leaderboard is
        readable either way. An account is only needed to <em>publish</em>
        your own matches to it.
      </p>
      <div class="auth-form">
        <input class="text-input" id="authEmail" type="email" placeholder="Email"
               value="${escapeHtml(state.authForm.email)}" autocomplete="off" />
        <input class="text-input" id="authPassword" type="password" placeholder="Password (8+ characters)"
               value="${escapeHtml(state.authForm.password)}" autocomplete="off" />
      </div>
      ${state.authForm.error ? `<div class="auth-error">${escapeHtml(state.authForm.error)}</div>` : ""}
      <div class="row" style="margin-top:12px">
        <button class="btn" id="signInBtn" type="button" ${state.authForm.busy ? "disabled" : ""}>
          ${state.authForm.busy ? "Working…" : "Sign in"}
        </button>
        <button class="btn btn-secondary" id="signUpBtn" type="button" ${state.authForm.busy ? "disabled" : ""}>
          Create account
        </button>
      </div>
    </section>`;
}

function wireAccountCard(root) {
  const emailEl = root.querySelector("#authEmail");
  const passEl = root.querySelector("#authPassword");
  if (emailEl) emailEl.addEventListener("input", (e) => (state.authForm.email = e.target.value));
  if (passEl) passEl.addEventListener("input", (e) => (state.authForm.password = e.target.value));

  const submit = async (flow) => {
    const email = state.authForm.email.trim();
    const password = state.authForm.password;
    if (!email || !password) {
      state.authForm.error = "Enter an email and password.";
      renderAccounts();
      return;
    }
    state.authForm.busy = true;
    state.authForm.error = null;
    renderAccounts();
    try {
      state.auth = await invoke("sign_in", { email, password, flow });
      state.authForm = { email: "", password: "", error: null, busy: false };
      // Publish anything that was waiting on an account.
      await invoke("sync_all");
    } catch (e) {
      state.authForm.busy = false;
      state.authForm.error = String(e);
    }
    renderAccounts();
    refreshSyncStatus();
  };

  const inBtn = root.querySelector("#signInBtn");
  const upBtn = root.querySelector("#signUpBtn");
  if (inBtn) inBtn.addEventListener("click", () => submit("signIn"));
  if (upBtn) upBtn.addEventListener("click", () => submit("signUp"));

  const outBtn = root.querySelector("#signOutBtn");
  if (outBtn)
    outBtn.addEventListener("click", async () => {
      state.auth = await invoke("sign_out");
      renderAccounts();
      refreshSyncStatus();
    });
}

/// Fills in the sync figures. Split out so the 700ms status poll can
/// refresh them without rebuilding the whole page — which would blow away
/// whatever is half-typed in the display-name field.
function renderCloudSection() {
  const statusText = document.getElementById("cloudStatusText");
  if (!statusText) return; // not on the Account page
  const s = state.sync || {};

  // Two parts, because these sit in a stat rail: a short value set in the
  // rail's large type, and the explanation underneath it. A whole sentence
  // in the value slot was set at 28px and read as a headline.
  let value = "Connecting";
  let detail = "";
  let tone = "";

  if (!state.auth.signedIn) {
    value = "Signed out";
    detail = "matches stay on this PC";
  } else if (s.pending > 0) {
    value = `Uploading ${s.pending}`;
    detail = "in progress";
  } else if (s.lastError) {
    value = "Offline";
    detail = s.lastError;
    tone = "loss";
  } else if (s.connected) {
    value = "Up to date";
    detail = s.lastSync ? `last push ${s.lastSync}` : "connected";
    tone = "win";
  } else {
    value = "Idle";
    detail = "nothing synced yet this session";
  }

  statusText.textContent = value;
  statusText.parentElement.className = "rail-value " + tone;
  const sub = document.getElementById("cloudStatusSub");
  if (sub) sub.textContent = detail;
  document.getElementById("cloudSyncedCount").textContent = s.synced ?? 0;
  document.getElementById("cloudDeviceId").textContent = state.deviceId || "—";
}

function renderSyncPill() {
  const pill = document.getElementById("syncPill");
  const s = state.sync;
  if (!s) return;

  pill.classList.remove("ok", "err", "warn");
  let label = "Cloud";
  let title = `Cloud sync — device ${state.deviceId || "?"}`;

  if (!state.auth.signedIn) {
    label = "Signed out";
    title = "Matches are saved locally. Sign in on the Account page to publish them to the global leaderboard.";
  } else if (s.pending > 0) {
    pill.classList.add("warn");
    label = `Syncing ${s.pending}`;
    title = `${s.pending} item(s) queued to upload`;
  } else if (s.lastError) {
    pill.classList.add("err");
    label = "Offline";
    title = `Cloud sync failed: ${s.lastError}\nYour matches are still saved locally.`;
  } else if (s.connected) {
    pill.classList.add("ok");
    label = "Synced";
    title = s.lastSync ? `Last synced at ${s.lastSync}` : "Connected to Convex";
  }

  document.getElementById("syncText").textContent = label;
  pill.title = title;
}


// ---------- Top-level wiring ----------

// Every view, its heading, and which game tab owns it.
const VIEWS = {
  dotaoverview: { game: "dota", title: "Overview", sub: "Your Dota 2 account at a glance" },
  live: { game: "dota", title: "Live", sub: "Real-time Dota 2 tracking via Valve's GSI feed" },
  dotamatches: { game: "dota", title: "Match History", sub: "Results, modes and scoreboards from OpenDota" },
  dotameta: { game: "dota", title: "Meta", sub: "Strongest heroes right now, and which way they are moving" },
  dotaheroes: { game: "dota", title: "Heroes", sub: "Per-hero performance across your recent games" },
  favorite: { game: "dota", title: "Favorite Hero", sub: "Deep dive and improvement trend for your pick" },
  builds: { game: "dota", title: "Builds", sub: "Your saved item builds, stored on this PC" },
  history: { game: "dota", title: "Tracked Sessions", sub: "Matches this app recorded live" },
  leaderboard: { game: "dota", title: "Leaderboard", sub: "Your personal bests and the shared board" },

  dloverview: { game: "deadlock", title: "Overview", sub: "Your Deadlock account at a glance" },
  dlmatches: { game: "deadlock", title: "Matches", sub: "Recent games for your linked account" },
  dlmeta: { game: "deadlock", title: "Meta", sub: "Hero and item win rates across ranked matches" },
  dlheroes: { game: "deadlock", title: "Heroes", sub: "Per-hero breakdown of your recent games" },
  dlfavorite: { game: "deadlock", title: "Favorite Hero", sub: "Deep dive on your most-played pick" },
  dlbuilds: { game: "deadlock", title: "Builds", sub: "Saved builds for Deadlock heroes" },

  overlaysettings: { game: null, title: "Overlay Settings", sub: "Position, opacity and what the overlay warns about" },
  accounts: { game: null, title: "Account", sub: "Display name, cloud sync and linked Steam accounts" },
  about: { game: null, title: "About & Updates", sub: "Version, update checks and data sources" },
};

/// Switches the top-level game tab. Views belonging to the other game are
/// hidden rather than removed, so state (scroll, filters, loaded matches)
/// survives flipping back and forth.
function setGame(game) {
  state.game = game;

  // Repaints the whole app in that game's colours. It sits on <html> so the
  // page background behind the scroll area changes too, not just the panels.
  document.documentElement.dataset.game = game;
  document.querySelectorAll(".game-tab").forEach((t) => t.classList.toggle("active", t.dataset.game === game));
  document.querySelectorAll("[data-game-group]").forEach((g) => {
    g.hidden = g.dataset.gameGroup !== game;
  });

  // Clicking a game tab should show that game. Anything not already one of
  // its views — including the global ones like Accounts — jumps to its home
  // view, otherwise the click appears to do nothing.
  const meta = VIEWS[state.view];
  if (!meta || meta.game !== game) {
    setView(game === "deadlock" ? "dloverview" : "dotaoverview");
  }
}

function setView(view) {
  if (!VIEWS[view]) view = "dotaoverview";
  state.view = view;

  const meta = VIEWS[view];
  if (meta.game && meta.game !== state.game) setGame(meta.game);

  document.querySelectorAll(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.view === view));
  const appBtn = document.getElementById("appMenuBtn");
  if (appBtn) appBtn.classList.toggle("current", meta.game === null);
  document.querySelectorAll(".appmenu-item").forEach((b) => b.classList.toggle("active", b.dataset.view === view));
  document.querySelectorAll(".view").forEach((p) => p.classList.toggle("active", p.id === `tab-${view}`));
  document.getElementById("viewTitle").textContent = meta.title;
  document.getElementById("viewSub").textContent = meta.sub;

  // A renderer that throws used to leave the page blank with no hint of
  // why, in a release build with no devtools. Now it says so on the page.
  try {
    renderView(view);
  } catch (err) {
    const pane = document.getElementById(`tab-${view}`);
    if (pane) {
      pane.innerHTML = `
        <section class="home-section" style="padding-top:0">
          <div class="home-head"><h2 class="home-title">This page failed to draw</h2></div>
          <pre class="boot-error">${escapeHtml(String((err && err.stack) || err))}</pre>
        </section>`;
    }
    console.error("render failed for", view, err);
  }
}

function renderView(view) {
  switch (view) {
    case "dotaoverview":
      loadDotaOverview();
      break;
    case "history":
      loadHistory().then(renderHistory);
      break;
    case "leaderboard":
      loadHistory().then(renderLeaderboard);
      break;
    case "dotamatches":
      dtRefreshLink().then(() => {
        dtRender();
        dtLoad();
      });
      break;
    case "dotaheroes":
      dtRefreshLink().then(() => {
        renderDotaHeroes();
        dtLoad().then(renderDotaHeroes);
      });
      break;
    case "favorite":
      dtRefreshLink().then(() => {
        renderFavoriteHero();
        dtLoad().then(renderFavoriteHero);
      });
      break;
    case "builds":
      renderBuilds();
      break;
    case "dotameta":
      loadDotaMeta();
      break;
    case "dlmeta":
      // Deliberately not routed through dlRender: the meta needs no linked
      // account, so it should work before anyone has connected Steam.
      loadDeadlockMeta();
      break;
    case "dloverview":
    case "dlmatches":
    case "dlheroes":
    case "dlfavorite":
    case "dlbuilds":
      state.dlTab = view;
      dlRefreshLink().then(() => {
        dlRender();
        dlLoad();
      });
      break;
    case "overlaysettings":
      renderOverlaySettings();
      break;
    case "accounts":
      renderAccounts();
      break;
    case "about":
      renderAbout();
      break;
    default:
      break;
  }
}

async function loadHistory() {
  state.history = await invoke("get_history");
  return state.history;
}

function refreshLive() {
  return invoke("get_live_state").then((live) => {
    const wasEnded = state.live && state.live.current && state.live.current.ended;
    state.live = live;
    renderLive();
    const liveNav = document.querySelector('.nav-item[data-view="live"]');
    if (liveNav) liveNav.classList.toggle("is-live", !!(live.current && !live.current.ended));

    const trackBtn = document.getElementById("trackingToggle");
    trackBtn.classList.toggle("on", live.trackingEnabled);
    trackBtn.classList.toggle("off", !live.trackingEnabled);
    trackBtn.querySelector(".label").textContent = live.trackingEnabled ? "Tracking" : "Paused";

    const errBanner = document.getElementById("serverError");
    if (live.serverError) {
      errBanner.hidden = false;
      errBanner.textContent = `⚠ ${live.serverError}`;
    } else {
      errBanner.hidden = true;
    }

    // A match just finished while we were on the Live tab — refresh cached
    // history so switching to History/Leaderboard shows it immediately.
    const isEndedNow = live.current && live.current.ended;
    if (isEndedNow && !wasEnded) {
      loadHistory();
    }
  });
}

/// The overlay is a separate always-on-top window. Opening it also drops
/// it into click-through mode so it can't steal input from the game; the
/// "Move" affordance temporarily hands input back so it can be dragged.
async function toggleOverlay() {
  try {
    if (state.overlay.visible) {
      await invoke("overlay_hide");
      state.overlay.visible = false;
    } else {
      await invoke("overlay_show");
      state.overlay.visible = true;
      state.overlay.clickThrough = true;
      await invoke("overlay_click_through", { clickThrough: true });
    }
    state.overlay.error = null;
  } catch (e) {
    // Swallowing this was the reason a failed overlay looked like a dead
    // button — there was nothing on screen to explain it.
    state.overlay.error = String(e);
  }
  renderOverlayToggle();
  if (state.view === "overlaysettings") renderOverlaySettings();
}

function renderOverlayToggle() {
  const btn = document.getElementById("overlayBtn");
  if (!btn) return;
  btn.classList.toggle("on", state.overlay.visible);
  btn.querySelector(".ghost-label").textContent = state.overlay.visible ? "Overlay on" : "Overlay";
}

function refreshSyncStatus() {
  return Promise.all([invoke("sync_status"), invoke("auth_status")])
    .then(([s, a]) => {
      const wasSignedIn = state.auth.signedIn;
      state.sync = s;
      state.auth = a;
      renderSyncPill();
      renderCloudSection();
      // A restored session (or an expired one) arrives asynchronously —
      // redraw the account card when that lands, unless the user is
      // mid-typing in it.
      if (wasSignedIn !== a.signedIn && state.view === "accounts" && !state.authForm.busy) {
        renderAccounts();
      }
    })
    .catch(() => {});
}

/// Fills every [data-icon] placeholder with its SVG. Done once at boot
/// rather than inlining the markup, so the icon set lives in one file.
function paintIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach((el) => {
    if (el.dataset.painted) return;
    el.innerHTML = icon(el.dataset.icon, 16);
    el.dataset.painted = "1";
  });
}

function wireShell() {
  document.getElementById("trackingToggle").addEventListener("click", () => {
    const nowOn = !document.getElementById("trackingToggle").classList.contains("on");
    invoke("set_tracking", { enabled: nowOn }).then(refreshLive);
  });

  document.getElementById("gameTabs").addEventListener("click", (e) => {
    const t = e.target.closest(".game-tab");
    if (t) setGame(t.dataset.game);
  });

  document.getElementById("nav").addEventListener("click", (e) => {
    const btn = e.target.closest(".nav-item");
    if (btn) setView(btn.dataset.view);
  });

  document.getElementById("overlayBtn").addEventListener("click", toggleOverlay);

  // App settings, which belong to neither game and so are not in the
  // sidebar. Opening a page from here closes the menu; so does clicking
  // anywhere else, which the document handler below takes care of.
  const appBtn = document.getElementById("appMenuBtn");
  const appMenu = document.getElementById("appMenu");
  if (appBtn && appMenu) {
    appBtn.addEventListener("click", (e) => {
      e.stopPropagation();
      const open = appMenu.hidden;
      appMenu.hidden = !open;
      appBtn.setAttribute("aria-expanded", String(open));
      appBtn.classList.toggle("on", open);
    });
    appMenu.querySelectorAll("[data-view]").forEach((el) =>
      el.addEventListener("click", () => {
        appMenu.hidden = true;
        appBtn.setAttribute("aria-expanded", "false");
        appBtn.classList.remove("on");
        setView(el.dataset.view);
      })
    );
  }

  document.addEventListener("click", () => {
    const menu = document.getElementById("appMenu");
    if (menu && !menu.hidden) {
      menu.hidden = true;
      const btn = document.getElementById("appMenuBtn");
      if (btn) {
        btn.setAttribute("aria-expanded", "false");
        btn.classList.remove("on");
      }
    }
    if (state.openTypeMenu !== null) {
      state.openTypeMenu = null;
      if (state.view === "history") renderHistory();
    }
  });
}

/// The chip in the top-right shows the signed-in account, falling back to
/// the local profile name so it isn't blank before anyone signs in.
function renderUserChip() {
  const nameEl = document.getElementById("userName");
  const rankEl = document.getElementById("userRank");
  const avatarEl = document.getElementById("userAvatar");
  if (!nameEl) return;

  const p = state.profile || {};
  nameEl.textContent = p.username || state.auth.email || "Signed out";
  rankEl.textContent = state.auth && state.auth.signedIn ? "Synced" : "Local only";

  const avatar = (DOTA.link && DOTA.link.avatar) || (DL.link && DL.link.avatar);
  avatarEl.style.backgroundImage = avatar ? `url("${avatar}")` : "none";
}

/// Accounts view: cloud sign-in plus the two linked game accounts, all in
/// one place rather than buried in each game's own tab.
function renderAccounts() {
  const root = document.getElementById("tab-accounts");
  if (!root) return;

  const linkRow = (game, link, powers, where) => {
    const linked = link && link.accountId;
    return `
      <div class="acct-row">
        ${
          linked && link.avatar
            ? `<img class="acct-avatar" src="${escapeHtml(link.avatar)}" alt="" />`
            : `<div class="acct-avatar"></div>`
        }
        <div class="acct-main">
          <div class="acct-name">${game}</div>
          <div class="acct-sub">
            ${
              linked
                ? `${escapeHtml(link.personaname || "Linked")} &middot; <span class="mono">${link.accountId}</span>`
                : `Not linked &mdash; ${where}`
            }
          </div>
        </div>
        <div class="acct-side">
          <span class="acct-note">${powers}</span>
          ${linked ? `<button class="btn btn-secondary" data-unlink="${game === "Dota 2" ? "dota" : "deadlock"}" type="button">Unlink</button>` : ""}
        </div>
      </div>`;
  };

  root.innerHTML = `
    <section class="home-section" style="padding-top:0">
      <div class="home-head"><h2 class="home-title">Display name</h2></div>
      <p class="hint" style="margin-bottom:10px;max-width:70ch">
        Shown in the app, and beside your entries on the shared leaderboard
        while you are signed in.
      </p>
      <div class="row">
        <input class="text-input grow" id="usernameInput" type="text" placeholder="Your name"
               value="${escapeHtml(state.profileDraft.username || "")}" style="max-width:340px" />
        <button class="btn" id="saveProfileBtn" type="button">Save</button>
        <span class="flash" id="saveFlash">Saved</span>
      </div>
    </section>

    ${accountCardHtml()}

    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Sync</h2>
        <button class="link-btn" id="syncAllBtn" type="button">Sync everything</button>
        <span class="flash" id="syncFlash">Queued</span>
      </div>
      ${railHtml([
        { label: "Status", value: '<span id="cloudStatusText">—</span>', sub: '<span id="cloudStatusSub"></span>' },
        { label: "Synced this session", value: '<span id="cloudSyncedCount">0</span>', sub: "matches pushed" },
        { label: "Device", value: '<span id="cloudDeviceId" class="acct-device">—</span>', sub: "this install's id" },
      ])}
      <p class="hint" style="margin-top:12px;max-width:72ch">
        Matches are written to this PC first and pushed up afterwards, so an
        unreachable server costs nothing &mdash; press <b>Sync everything</b>
        once you are back online.
      </p>
    </section>

    <section class="home-section">
      <div class="home-head">
        <h2 class="home-title">Linked game accounts</h2>
        <div class="home-meta">Steam</div>
      </div>
      ${linkRow("Dota 2", DOTA.link, "Match History, modes and scoreboards, via OpenDota", "open <b>Match History</b> to search for your profile")}
      ${linkRow("Deadlock", DL.link, "Every Deadlock view, via the community Deadlock API", "open <b>Deadlock &rsaquo; Overview</b> to search for your profile")}
    </section>`;

  // ---- wiring ----

  root.querySelector("#usernameInput").addEventListener("input", (e) => {
    state.profileDraft.username = e.target.value;
  });
  root.querySelector("#saveProfileBtn").addEventListener("click", () => {
    invoke("save_profile", { profile: state.profileDraft }).then(() => {
      state.profile = { ...state.profileDraft };
      renderUserChip();
      flash("saveFlash");
    });
  });

  wireAccountCard(root);

  root.querySelector("#syncAllBtn").addEventListener("click", () => {
    invoke("sync_all").then(() => flash("syncFlash"));
  });
  renderCloudSection();

  root.querySelectorAll("[data-unlink]").forEach((el) =>
    el.addEventListener("click", async () => {
      if (el.dataset.unlink === "dota") {
        DOTA.link = await invoke("dota_unlink");
        DOTA.matches = [];
        DOTA.summary = null;
        DOTA.loadedAt = 0;
      } else {
        DL.link = await invoke("deadlock_unlink");
        DL.matches = [];
        DL.summary = null;
        DL.rank = null;
        DL.loadedAt = 0;
      }
      renderAccounts();
      renderUserChip();
    })
  );
}

/// Briefly shows a confirmation next to a button that has no other visible
/// result. Without it "Save" and "Sync everything" look like they did
/// nothing at all.
function flash(id) {
  const el = document.getElementById(id);
  if (!el) return;
  el.classList.add("show");
  setTimeout(() => el.classList.remove("show"), 1600);
}

async function boot() {
  wireShell();
  paintIcons();
  state.prefs = (await invoke("get_prefs").catch(() => null)) || state.prefs;
  state.profile = await invoke("get_profile");
  state.profileDraft = { ...state.profile };
  state.deviceId = await invoke("device_identity").catch(() => null);
  state.auth = (await invoke("auth_status").catch(() => null)) || { signedIn: false, email: null };
  state.overlay.visible = await invoke("overlay_visible").catch(() => false);

  // Both links are local file reads; the match data behind them is only
  // fetched when the relevant view is actually opened.
  await dlRefreshLink();
  await dtRefreshLink();

  renderOverlayToggle();
  renderUserChip();
  renderAccounts();
  await refreshLive();
  await refreshSyncStatus();
  await loadHistory();
  renderHistory();
  renderLeaderboard();

  setView("dotaoverview");

  // Let the app settle before touching the network, then check quietly.
  setTimeout(() => checkForUpdate({ quiet: true }), 4000);
  setInterval(() => checkForUpdate({ quiet: true }), 6 * 60 * 60 * 1000);

  setInterval(refreshLive, 700);
  setInterval(refreshSyncStatus, 1500);

  // Live Deadlock presence, polled slowly — it hits a rate-limited
  // community API, unlike Dota's local feed.
  const pollDeadlockLive = async () => {
    if (!DL.link.accountId) return;
    try {
      DL.live = await invoke("deadlock_live");
      if (state.view === "dloverview") dlRender();
    } catch (_) {
      /* community API is allowed to be flaky */
    }
  };
  pollDeadlockLive();
  setInterval(pollDeadlockLive, 45000);
}

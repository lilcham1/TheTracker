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

const RANKS = [
  { id: "herald", label: "Herald", color: "#a0a0a0" },
  { id: "guardian", label: "Guardian", color: "#5fa85f" },
  { id: "crusader", label: "Crusader", color: "#4fa8c9" },
  { id: "archon", label: "Archon", color: "#4f8fd1" },
  { id: "legend", label: "Legend", color: "#8f6fd1" },
  { id: "ancient", label: "Ancient", color: "#d14f6f" },
  { id: "divine", label: "Divine", color: "#4fd1c9" },
  { id: "immortal", label: "Immortal", color: "#f0a020" },
];

const ROLES = [
  { id: "carry", label: "Carry", color: "#e05b5b" },
  { id: "mid", label: "Mid", color: "#e0c05b" },
  { id: "offlane", label: "Offlane", color: "#a05be0" },
  { id: "soft_support", label: "Soft Support", color: "#5be0a0" },
  { id: "hard_support", label: "Hard Support", color: "#5b9be0" },
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

// ---------- Tauri bridge (falls back to mock data when previewed as a
// plain webpage outside Tauri, e.g. in a browser, for layout sanity-checks
// during development) ----------

const invoke = window.__TAURI__ ? window.__TAURI__.core.invoke : mockInvoke;

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

/// CDN icons occasionally 404 (renamed/removed items, or no connection).
/// Swap those for a readable text tile instead of a broken-image glyph.
/// Wired programmatically rather than via an inline `onerror` attribute,
/// which the app's CSP (script-src 'self') would block.
function wireImageFallbacks(root) {
  root.querySelectorAll("img[data-item-img]").forEach((img) => {
    img.addEventListener(
      "error",
      () => {
        const name = img.dataset.itemImg;
        const div = document.createElement("div");
        div.className = "item-fallback";
        div.textContent = itemDisplayName(name)
          .split(" ")
          .map((w) => w[0])
          .join("")
          .slice(0, 3);
        img.replaceWith(div);
      },
      { once: true }
    );
  });
  root.querySelectorAll("img.hero-portrait").forEach((img) => {
    img.addEventListener("error", () => img.classList.add("portrait-missing"), { once: true });
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// ---------- Steam auto-detect ----------
//
// Reads only the signed-in Steam account's id and display name (see
// steam.rs for exactly what is and isn't touched — no credentials, no
// tokens). Shared by both link screens.

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
  overlay: { visible: false, clickThrough: true },
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
  wireImageFallbacks(root);

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
        <div class="history-item ${open ? "open" : ""}" data-matchid="${escapeHtml(m.matchid)}">
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

  wireImageFallbacks(root);

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
        <span class="lb-rank">#${i + 1}</span>
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
      : "Sign in on the Profile tab to publish your own games here.";
    return `<div class="empty-state">Nobody has published a game with this stat yet.<br/>${hint}</div>`;
  }
  return g.rows
    .map((r, i) => {
      const isYou = state.auth.signedIn && r.userId === state.auth.userId;
      const who = r.username && r.username.length ? r.username : "Anonymous";
      return `
      <div class="lb-row ${isYou ? "is-you" : ""}">
        <span class="lb-medal">${MEDALS[i] || ""}</span>
        <span class="lb-rank">#${i + 1}</span>
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

// ---------- Rendering: Profile tab ----------

function renderProfile() {
  const root = document.getElementById("tab-profile");
  const p = state.profileDraft;

  root.innerHTML = `
    <div>
      <label class="form-label">Username</label>
      <input class="text-input" id="usernameInput" type="text" value="${escapeHtml(p.username || "")}" placeholder="Your name" />
    </div>

    <div>
      <label class="form-label">Rank</label>
      <div class="swatch-grid" id="rankSwatches">
        ${RANKS.map(
          (r) => `<button class="swatch" data-rank="${r.id}" style="${p.rank === r.id ? `background:${r.color};border-color:${r.color};` : ""}">${r.label}</button>`
        ).join("")}
      </div>
    </div>

    <div>
      <label class="form-label">Main Role</label>
      <div class="swatch-grid" id="roleSwatches">
        ${ROLES.map(
          (r) => `<button class="swatch" data-role="${r.id}" style="${p.role === r.id ? `background:${r.color};border-color:${r.color};` : ""}">${r.label}</button>`
        ).join("")}
      </div>
    </div>

    <div style="display:flex;align-items:center;gap:14px;">
      <button class="save-btn" id="saveProfileBtn">Save Profile</button>
      <span class="save-flash" id="saveFlash">Saved!</span>
    </div>


  `;

  root.querySelectorAll("[data-rank]").forEach((btn) =>
    btn.addEventListener("click", () => {
      state.profileDraft.rank = btn.dataset.rank;
      renderProfile();
    })
  );
  root.querySelectorAll("[data-role]").forEach((btn) =>
    btn.addEventListener("click", () => {
      state.profileDraft.role = btn.dataset.role;
      renderProfile();
    })
  );
  root.querySelector("#usernameInput").addEventListener("input", (e) => {
    state.profileDraft.username = e.target.value;
  });
  root.querySelector("#saveProfileBtn").addEventListener("click", () => {
    invoke("save_profile", { profile: state.profileDraft }).then(() => {
      state.profile = { ...state.profileDraft };
      renderUserChip();
      const flash = document.getElementById("saveFlash");
      flash.classList.add("show");
      setTimeout(() => flash.classList.remove("show"), 1600);
    });
  });
}

function accountCardHtml() {
  const a = state.auth || {};
  if (a.signedIn) {
    return `
      <div class="card" style="margin-top:6px">
        <p class="section-title">Account</p>
        <div class="cloud-body">
          <div class="cloud-line">
            <span class="cloud-key">Signed in as</span>
            <span class="cloud-val good">${escapeHtml(a.email || "—")}</span>
          </div>
          <p class="cloud-note">
            Your matches publish to the global leaderboard under this account.
          </p>
          <button class="btn btn-secondary" id="signOutBtn">Sign out</button>
        </div>
      </div>`;
  }
  return `
    <div class="card" style="margin-top:6px">
      <p class="section-title">Account</p>
      <div class="cloud-body">
        <p class="cloud-note" style="margin-top:0">
          Tracking works fully signed out, and you can browse the global
          leaderboard either way — an account is only needed to
          <em>publish</em> your own matches to it.
        </p>
        <input class="text-input" id="authEmail" type="email" placeholder="Email" value="${escapeHtml(state.authForm.email)}" autocomplete="off" />
        <input class="text-input" id="authPassword" type="password" placeholder="Password (8+ characters)" value="${escapeHtml(state.authForm.password)}" autocomplete="off" />
        ${state.authForm.error ? `<div class="auth-error">${escapeHtml(state.authForm.error)}</div>` : ""}
        <div style="display:flex;gap:8px;align-items:center;">
          <button class="save-btn" id="signInBtn" ${state.authForm.busy ? "disabled" : ""}>
            ${state.authForm.busy ? "Working…" : "Sign in"}
          </button>
          <button class="btn btn-secondary" id="signUpBtn" ${state.authForm.busy ? "disabled" : ""}>Create account</button>
        </div>
      </div>
    </div>`;
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

/// Fills in the Cloud Sync card. Split out so the 700ms status poll can
/// refresh it without rebuilding the whole profile form (which would blow
/// away whatever the user is typing in the username field).
function renderCloudSection() {
  const statusText = document.getElementById("cloudStatusText");
  if (!statusText) return; // not on the Profile tab
  const s = state.sync || {};

  let text = "Connecting…";
  if (!state.auth.signedIn) text = "Signed out — matches stay on this PC";
  else if (s.pending > 0) text = `Uploading ${s.pending}…`;
  else if (s.lastError) text = `Offline — ${s.lastError}`;
  else if (s.connected) text = s.lastSync ? `Up to date (${s.lastSync})` : "Connected";
  else text = "Idle — nothing synced yet this session";

  statusText.textContent = text;
  statusText.className = "cloud-val " + (s.lastError ? "bad" : s.connected ? "good" : "");
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
    title = "Matches are saved locally. Sign in on the Profile tab to publish them to the global leaderboard.";
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
  home: { game: null, title: "Account Overview", sub: "Both games at a glance" },
  live: { game: "dota", title: "Live", sub: "Real-time Dota 2 tracking via Valve's GSI feed" },
  dotamatches: { game: "dota", title: "Match History", sub: "Results, modes and scoreboards from OpenDota" },
  dotaheroes: { game: "dota", title: "Heroes", sub: "Per-hero performance across your recent games" },
  favorite: { game: "dota", title: "Favorite Hero", sub: "Deep dive and improvement trend for your pick" },
  builds: { game: "dota", title: "Builds", sub: "Your saved item builds, stored on this PC" },
  history: { game: "dota", title: "Tracked Sessions", sub: "Matches this app recorded live" },
  leaderboard: { game: "dota", title: "Leaderboard", sub: "Your personal bests and the shared board" },

  dloverview: { game: "deadlock", title: "Overview", sub: "Post-match stats via the community Deadlock API" },
  dlmatches: { game: "deadlock", title: "Matches", sub: "Recent games for your linked account" },
  dlheroes: { game: "deadlock", title: "Heroes", sub: "Per-hero breakdown of your recent games" },
  dlfavorite: { game: "deadlock", title: "Favorite Hero", sub: "Deep dive on your most-played pick" },
  dlbuilds: { game: "deadlock", title: "Builds", sub: "Saved builds for Deadlock heroes" },

  overlaysettings: { game: null, title: "Overlay Settings", sub: "Position, opacity and what the overlay shows" },
  profile: { game: null, title: "Profile", sub: "Your display name, rank and role" },
  accounts: { game: null, title: "Accounts", sub: "Cloud sync and linked game accounts" },
};

/// Switches the top-level game tab. Views belonging to the other game are
/// hidden rather than removed, so state (scroll, filters, loaded matches)
/// survives flipping back and forth.
function setGame(game) {
  state.game = game;
  document.querySelectorAll(".game-tab").forEach((t) => t.classList.toggle("active", t.dataset.game === game));
  document.querySelectorAll("[data-game-group]").forEach((g) => {
    g.hidden = g.dataset.gameGroup !== game;
  });

  // Clicking a game tab should show that game. Anything not already one of
  // its views — including the global ones like Accounts — jumps to its home
  // view, otherwise the click appears to do nothing.
  const meta = VIEWS[state.view];
  if (!meta || meta.game !== game) {
    setView(game === "deadlock" ? "dloverview" : "live");
  }
}

function setView(view) {
  if (!VIEWS[view]) view = "home";
  state.view = view;
  state.tab = view; // older helpers still read state.tab

  const meta = VIEWS[view];
  if (meta.game && meta.game !== state.game) setGame(meta.game);

  document.querySelectorAll(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.view === view));
  document.querySelectorAll(".view").forEach((p) => p.classList.toggle("active", p.id === `tab-${view}`));
  document.getElementById("viewTitle").textContent = meta.title;
  document.getElementById("viewSub").textContent = meta.sub;

  switch (view) {
    case "home":
      loadHome();
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
    case "profile":
      renderProfile();
      break;
    case "accounts":
      renderAccounts();
      break;
    default:
      break;
  }
}

/// Deadlock views share one renderer, so it needs to know which is showing.
function setDeadlockTab(tab) {
  setView(tab);
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
  } catch (e) {
    console.error("overlay toggle failed", e);
  }
  renderOverlayToggle();
  if (state.view === "profile") renderProfile();
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

  document.addEventListener("click", () => {
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
  const rank = RANKS.find((r) => r.id === p.rank);
  const role = ROLES.find((r) => r.id === p.role);

  nameEl.textContent = p.username || state.auth.email || "Signed out";
  rankEl.textContent = [rank && rank.label, role && role.label].filter(Boolean).join(" · ");
  if (rank) rankEl.style.color = rank.color;

  const avatar = (DOTA.link && DOTA.link.avatar) || (DL.link && DL.link.avatar);
  avatarEl.style.backgroundImage = avatar ? `url("${avatar}")` : "none";
}

/// Accounts view: cloud sign-in plus the two linked game accounts, all in
/// one place rather than buried in each game's own tab.
function renderAccounts() {
  const root = document.getElementById("tab-accounts");
  if (!root) return;

  const dotaLinked = DOTA.link && DOTA.link.accountId;
  const dlLinked = DL.link && DL.link.accountId;

  root.innerHTML = `
    ${accountCardHtml()}
    <div class="card" style="margin-top:6px">
      <p class="section-title">Cloud Sync</p>
      <div class="cloud-body">
        <div class="cloud-line">
          <span class="cloud-key">Status</span>
          <span class="cloud-val" id="cloudStatusText">—</span>
        </div>
        <div class="cloud-line">
          <span class="cloud-key">Synced this session</span>
          <span class="cloud-val" id="cloudSyncedCount">0</span>
        </div>
        <div class="cloud-line">
          <span class="cloud-key">Device ID</span>
          <span class="cloud-val mono" id="cloudDeviceId">—</span>
        </div>
        <p class="cloud-note">
          Matches are always saved to this PC first, then pushed to the cloud.
          If the cloud is unreachable nothing is lost — just press Sync
          Everything once you're back online.
        </p>
        <div style="display:flex;align-items:center;gap:12px;">
          <button class="btn btn-secondary" id="syncAllBtn">Sync Everything</button>
          <span class="save-flash" id="syncFlash">Queued!</span>
        </div>
      </div>
    </div>


    <div class="card col">
      <div class="section-head"><h3 class="section-title">Dota 2 — Steam account</h3></div>
      ${
        dotaLinked
          ? `<div class="row">
               ${DOTA.link.avatar ? `<img src="${escapeHtml(DOTA.link.avatar)}" style="width:38px;height:38px;border-radius:50%" alt="" />` : ""}
               <div class="grow">
                 <div class="result-name">${escapeHtml(DOTA.link.personaname || "Linked")}</div>
                 <div class="result-id">Account ${DOTA.link.accountId}</div>
               </div>
               <button class="btn btn-secondary" id="dotaUnlinkBtn" type="button">Unlink</button>
             </div>
             <p class="hint">Powers Match History: real results, game modes and scoreboards from OpenDota.</p>`
          : `<p class="hint">Not linked. Open <b>Match History</b> to search for your Steam profile and link it.</p>`
      }
    </div>

    <div class="card col">
      <div class="section-head"><h3 class="section-title">Deadlock — Steam account</h3></div>
      ${
        dlLinked
          ? `<div class="row">
               ${DL.link.avatar ? `<img src="${escapeHtml(DL.link.avatar)}" style="width:38px;height:38px;border-radius:50%" alt="" />` : ""}
               <div class="grow">
                 <div class="result-name">${escapeHtml(DL.link.personaname || "Linked")}</div>
                 <div class="result-id">Account ${DL.link.accountId}</div>
               </div>
               <button class="btn btn-secondary" id="dlUnlinkBtn" type="button">Unlink</button>
             </div>
             <p class="hint">Powers the Deadlock views via the community Deadlock API.</p>`
          : `<p class="hint">Not linked. Open <b>Deadlock → Overview</b> to search for your Steam profile.</p>`
      }
    </div>`;

  wireAccountCard(root);
  const syncBtn = root.querySelector("#syncAllBtn");
  if (syncBtn)
    syncBtn.addEventListener("click", () => {
      invoke("sync_all").then(() => {
        const flash = document.getElementById("syncFlash");
        if (flash) {
          flash.classList.add("show");
          setTimeout(() => flash.classList.remove("show"), 1600);
        }
      });
    });
  renderCloudSection();

  const du = root.querySelector("#dotaUnlinkBtn");
  if (du)
    du.addEventListener("click", async () => {
      DOTA.link = await invoke("dota_unlink");
      DOTA.matches = [];
      DOTA.summary = null;
      DOTA.loadedAt = 0;
      renderAccounts();
      renderUserChip();
    });

  const dlu = root.querySelector("#dlUnlinkBtn");
  if (dlu)
    dlu.addEventListener("click", async () => {
      DL.link = await invoke("deadlock_unlink");
      DL.matches = [];
      DL.summary = null;
      DL.loadedAt = 0;
      renderAccounts();
      renderUserChip();
    });
}

async function boot() {
  wireShell();
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
  renderProfile();
  await refreshLive();
  await refreshSyncStatus();
  await loadHistory();
  renderHistory();
  renderLeaderboard();

  setView("home");

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

// ---------- Mock backend (only used outside Tauri, for visual preview) ----------


// ---- preview-only fixtures for the newer views ----

function mockDotaApi() {
  const heroes = [
    ["juggernaut", "Juggernaut"],
    ["queenofpain", "Queen of Pain"],
    ["rattletrap", "Clockwerk"],
    ["lina", "Lina"],
    ["pudge", "Pudge"],
  ];
  const now = Math.floor(Date.now() / 1000);
  const matches = Array.from({ length: 24 }, (_, i) => {
    const [slug, name] = heroes[i % heroes.length];
    const won = Math.random() > 0.45;
    const kills = 2 + Math.floor(Math.random() * 14);
    const deaths = 1 + Math.floor(Math.random() * 11);
    const assists = 3 + Math.floor(Math.random() * 18);
    return {
      matchId: 9000000 + i,
      heroId: i,
      heroName: name,
      heroSlug: slug,
      startTime: now - i * 7200,
      durationSeconds: 1800 + Math.floor(Math.random() * 1800),
      won,
      radiant: i % 2 === 0,
      kills,
      deaths,
      assists,
      kda: (kills + assists) / Math.max(1, deaths),
      lastHits: 80 + Math.floor(Math.random() * 320),
      denies: Math.floor(Math.random() * 25),
      goldPerMin: 380 + Math.floor(Math.random() * 320),
      xpPerMin: 400 + Math.floor(Math.random() * 400),
      heroDamage: 12000 + Math.floor(Math.random() * 40000),
      towerDamage: Math.floor(Math.random() * 8000),
      heroHealing: Math.floor(Math.random() * 3000),
      gameType: ["ranked", "all_pick", "turbo"][i % 3],
      modeName: "All Pick",
      lobbyName: i % 3 === 0 ? "Ranked" : "Unranked",
      partySize: 1 + (i % 3),
      abandoned: false,
    };
  });
  const wins = matches.filter((m) => m.won).length;
  const sum = (pick) => matches.reduce((t, m) => t + pick(m), 0);
  return {
    matches,
    summary: {
      matches: matches.length,
      wins,
      losses: matches.length - wins,
      winRate: (wins / matches.length) * 100,
      kills: sum((m) => m.kills),
      deaths: sum((m) => m.deaths),
      assists: sum((m) => m.assists),
      kda: (sum((m) => m.kills) + sum((m) => m.assists)) / Math.max(1, sum((m) => m.deaths)),
      avgGpm: Math.round(sum((m) => m.goldPerMin) / matches.length),
      avgXpm: Math.round(sum((m) => m.xpPerMin) / matches.length),
      avgLastHits: Math.round(sum((m) => m.lastHits) / matches.length),
    },
  };
}

function mockDeadlock() {
  const names = ["Yamato", "Infernus", "Seven", "Haze", "Abrams"];
  const now = Math.floor(Date.now() / 1000);
  const matches = Array.from({ length: 18 }, (_, i) => {
    const kills = 2 + Math.floor(Math.random() * 12);
    const deaths = 2 + Math.floor(Math.random() * 12);
    return {
      matchId: 103000000 + i,
      heroId: i,
      heroName: names[i % names.length],
      heroImage: null,
      startTime: now - i * 9000,
      durationSeconds: 1500 + Math.floor(Math.random() * 1500),
      kills,
      deaths,
      assists: 2 + Math.floor(Math.random() * 12),
      netWorth: 20000 + Math.floor(Math.random() * 30000),
      lastHits: 60 + Math.floor(Math.random() * 200),
      denies: Math.floor(Math.random() * 20),
      heroLevel: 20 + Math.floor(Math.random() * 20),
      outcome: Math.random() > 0.5 ? "win" : "loss",
      abandoned: false,
    };
  });
  const wins = matches.filter((m) => m.outcome === "win").length;
  return {
    matches,
    summary: {
      matches: matches.length,
      wins,
      losses: matches.length - wins,
      winRate: (wins / matches.length) * 100,
      kills: 0,
      deaths: 0,
      assists: 0,
      kda: 2.4,
      avgSouls: 31000,
      bestHero: "Yamato",
    },
    rank: { badge: 26, tier: 2, subrank: 6, tierName: "Seeker", label: "Seeker 6" },
  };
}

function mockInvoke(cmd, args) {
  const mock = (window.__mockState ||= {
    history: mockHistory(),
    profile: { username: "lilcham", rank: "archon", role: "mid" },
    current: mockCurrentMatch(),
  });
  switch (cmd) {
    case "dota_link_status":
      return Promise.resolve({ accountId: 850402858, personaname: "lilcham", avatar: null });
    case "deadlock_link_status":
      return Promise.resolve({ accountId: 850402858, personaname: "lilcham", avatar: null });
    case "dota_api_history":
      return Promise.resolve((window.__mockDota ||= mockDotaApi()));
    case "deadlock_overview":
      return Promise.resolve((window.__mockDl ||= mockDeadlock()));
    case "deadlock_live":
      return Promise.resolve(null);
    case "get_prefs":
      return Promise.resolve(
        (window.__mockPrefs ||= {
          favorites: { dota: "juggernaut", deadlock: "Yamato" },
          builds: [],
          overlay: {
            opacity: 0.85,
            scale: 1,
            corner: "top-left",
            clickThrough: true,
            dota: { stats: true, roshan: true, checkpoints: true, items: false, deaths: false },
            deadlock: { matchInfo: true, lineup: false, sessionRecord: true },
          },
        })
      );
    case "set_favorite_hero":
      window.__mockPrefs.favorites[args.game] = args.hero;
      return Promise.resolve(window.__mockPrefs);
    case "save_overlay_settings":
      window.__mockPrefs.overlay = { ...window.__mockPrefs.overlay, ...args.settings };
      return Promise.resolve(window.__mockPrefs);
    case "save_build":
      window.__mockPrefs.builds.push({ ...args.build, id: args.build.id || String(Date.now()) });
      return Promise.resolve(window.__mockPrefs);
    case "delete_build":
      window.__mockPrefs.builds = window.__mockPrefs.builds.filter((b) => b.id !== args.id);
      return Promise.resolve(window.__mockPrefs);
    case "dota_match_detail":
      return Promise.resolve({
        matchId: args.matchId,
        durationSeconds: 2400,
        startTime: 0,
        radiantWin: true,
        radiantScore: 34,
        direScore: 21,
        modeName: "All Pick",
        lobbyName: "Ranked",
        gameType: "ranked",
        players: Array.from({ length: 10 }, (_, i) => ({
          accountId: i,
          name: "Player " + (i + 1),
          heroName: "Juggernaut",
          heroSlug: "juggernaut",
          radiant: i < 5,
          kills: i,
          deaths: 10 - i,
          assists: i * 2,
          lastHits: 100 + i * 20,
          denies: i,
          goldPerMin: 400 + i * 30,
          xpPerMin: 450 + i * 25,
          heroDamage: 15000 + i * 2000,
          netWorth: 18000 + i * 1500,
          level: 20 + i,
          isMe: i === 2,
          items: [],
        })),
      });
    case "get_live_state":
      return Promise.resolve({ current: mock.current, trackingEnabled: true, serverError: null });
    case "get_history":
      return Promise.resolve(mock.history);
    case "get_profile":
      return Promise.resolve(mock.profile);
    case "save_profile":
      mock.profile = args.profile;
      return Promise.resolve();
    case "set_history_game_type": {
      const entry = mock.history.find((h) => h.matchid === args.matchid);
      if (entry) entry.gameType = args.gameType;
      return Promise.resolve(mock.history);
    }
    case "device_identity":
      return Promise.resolve("mock-device-0001");
    case "auth_status":
      return Promise.resolve(mock.auth ||= { signedIn: false, email: null, userId: null, lastError: null });
    case "sign_in":
      mock.auth = { signedIn: true, email: args.email, userId: "mock-user-1", lastError: null };
      return Promise.resolve(mock.auth);
    case "sign_out":
      mock.auth = { signedIn: false, email: null, userId: null, lastError: null };
      return Promise.resolve(mock.auth);
    case "sync_status":
      return Promise.resolve({ connected: true, pending: 0, synced: 3, lastError: null, lastSync: "12:34:56" });
    case "sync_all":
      return Promise.resolve(mock.history.length);
    case "overlay_visible":
      return Promise.resolve(false);
    case "overlay_show":
    case "overlay_hide":
    case "overlay_click_through":
      return Promise.resolve(null);
    case "deadlock_link_status":
      return Promise.resolve(mock.dlLink ||= { accountId: null, personaname: null, avatar: null });
    case "deadlock_search":
      return Promise.resolve([
        { accountId: 850402858, personaname: "恵lilcham", avatar: null, profileUrl: null },
        { accountId: 111111111, personaname: "lilcham alt", avatar: null, profileUrl: null },
      ]);
    case "deadlock_link":
      mock.dlLink = { accountId: args.accountId, personaname: args.personaname, avatar: args.avatar };
      return Promise.resolve(mock.dlLink);
    case "deadlock_unlink":
      mock.dlLink = { accountId: null, personaname: null, avatar: null };
      return Promise.resolve(mock.dlLink);
    case "deadlock_live":
      return Promise.resolve(null);
    case "deadlock_overview": {
      const matches = mockDeadlockMatches();
      return Promise.resolve({ matches, summary: mockDeadlockSummary(matches), rank: { badge: 26, tier: 2, subrank: 6, tierName: "Seeker", label: "Seeker 6" } });
    }
    case "global_leaderboard":
      return Promise.resolve([
        { userId: "someone-else", username: "Dendi", heroName: "npc_dota_hero_nevermore", gameType: "ranked", date: "", value: 214 },
        { userId: "mock-user-1", username: "lilcham", heroName: "npc_dota_hero_antimage", gameType: "ranked", date: "", value: 187 },
        { userId: "another", username: "Miracle", heroName: "npc_dota_hero_wisp", gameType: "turbo", date: "", value: 165 },
      ]);
    default:
      return Promise.resolve(null);
  }
}

function mockDeadlockMatches() {
  const heroes = ["Yamato", "Infernus", "Seven", "Lash", "Bebop"];
  const out = [];
  for (let i = 0; i < 14; i++) {
    const outcome = i % 3 === 0 ? "loss" : i % 7 === 5 ? "abandoned" : "win";
    out.push({
      matchId: 103485245 - i,
      heroId: 27,
      heroName: heroes[i % heroes.length],
      heroImage: null,
      startTime: Math.floor(Date.now() / 1000) - i * 7200,
      durationSeconds: 1800 + i * 60,
      kills: 4 + (i % 9),
      deaths: 3 + (i % 6),
      assists: 6 + (i % 8),
      netWorth: 28000 + i * 900,
      lastHits: 100 + i * 7,
      denies: i % 12,
      heroLevel: 24 + (i % 12),
      outcome,
      abandoned: outcome === "abandoned",
    });
  }
  return out;
}

function mockDeadlockSummary(matches) {
  const scored = matches.filter((m) => m.outcome === "win" || m.outcome === "loss");
  const wins = scored.filter((m) => m.outcome === "win").length;
  const k = matches.reduce((s, m) => s + m.kills, 0);
  const d = matches.reduce((s, m) => s + m.deaths, 0);
  const a = matches.reduce((s, m) => s + m.assists, 0);
  return {
    matches: matches.length,
    wins,
    losses: scored.length - wins,
    winRate: scored.length ? (wins / scored.length) * 100 : 0,
    kills: k,
    deaths: d,
    assists: a,
    kda: (k + a) / Math.max(d, 1),
    avgSouls: Math.round(matches.reduce((s, m) => s + m.netWorth, 0) / matches.length),
    bestHero: "Yamato",
  };
}

function mockCurrentMatch() {
  return {
    matchid: "7891234560",
    heroName: "npc_dota_hero_queenofpain",
    startedAt: new Date().toISOString(),
    wasAlive: true,
    ownedItemCounts: {},
    deaths: [
      { clock: "6:41", goldLost: 214 },
      { clock: "14:02", goldLost: 388 },
      { clock: "21:37", goldLost: 512 },
    ],
    keyItemLog: [
      { clock: "8:12", item: "power_treads" },
      { clock: "16:30", item: "black_king_bar" },
      { clock: "24:05", item: "aghanims_scepter" },
    ],
    checkpoints: { 5: { lastHits: 31, denies: 4 }, 10: { lastHits: 68, denies: 9 }, 15: { lastHits: 104, denies: 12 }, 20: null, 25: null },
    lastClockTime: 1123,
    lastHits: 137,
    denies: 15,
    kills: 8,
    prevGold: 2400,
    ended: false,
    summary: null,
    gameType: "ranked",
    roshan: { deaths: 1, lastDeathClock: 900, wasAlive: false },
  };
}

function mockHistory() {
  const mk = (matchid, hero, date, duration, kills, deaths, gold, type, lh25) => ({
    matchid,
    heroName: hero,
    date,
    duration,
    kills,
    totalDeaths: deaths,
    totalGoldLost: gold,
    deaths: [
      { clock: "9:14", goldLost: Math.round(gold / Math.max(deaths, 1)) },
      { clock: "18:52", goldLost: Math.round(gold / Math.max(deaths, 1)) },
    ],
    keyItems: [
      { clock: "9:40", item: "phase_boots" },
      { clock: "19:20", item: "blink" },
    ],
    checkpoints: { 5: { lastHits: 28, denies: 3 }, 10: { lastHits: 61, denies: 7 }, 15: { lastHits: 95, denies: 10 }, 20: { lastHits: 128, denies: 12 }, 25: { lastHits: lh25, denies: 14 } },
    roshanDeaths: 2,
    gameType: type,
    comparison: {
      deaths: { value: deaths, avg: 5.2, verdict: deaths < 5 ? "better" : "worse", isBest: deaths <= 2 },
      goldLost: { value: gold, avg: 1420, verdict: gold < 1400 ? "better" : "worse", isBest: false },
      checkpoints: { 25: { value: lh25, avg: 150.4, verdict: lh25 > 155 ? "better" : "similar", isBest: lh25 > 180 } },
    },
    gamesComparedAgainst: 7,
  });
  return [
    mk("7891234501", "npc_dota_hero_antimage", "2026-09-01T18:22:00Z", "38:14", 11, 3, 980, "ranked", 187),
    mk("7891234502", "npc_dota_hero_nevermore", "2026-09-02T20:05:00Z", "44:52", 7, 8, 2140, "turbo", 142),
    mk("7891234503", "npc_dota_hero_rattletrap", "2026-09-03T21:41:00Z", "31:08", 4, 5, 1310, "all_pick", 151),
  ];
}

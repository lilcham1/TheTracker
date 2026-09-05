// Overlay renderer. Runs in its own transparent window and polls the same
// Tauri commands the main window does.
//
// What it shows is deliberately the set every established Dota overlay
// shows: objective and rune timers derived from the game clock, plus your
// own scoreboard line. Every number here is either already on your screen
// or simple arithmetic on the clock you can see — nothing is read from the
// game process, and nothing reveals an opponent's state.

const invoke = window.__TAURI__ ? window.__TAURI__.core.invoke : null;

// Timing rules, current as of the 2026 patches:
//   Bounty  — 0:00, then every 4 minutes
//   Water   — 2:00 and 4:00 only, then never again
//   Power   — from 6:00, every 2 minutes
//   Wisdom  — 7:00, then every 7 minutes
//   Stacks  — neutral camps spawn on the minute, so the pull goes at :53
//   Day/night — flips every 5 minutes
const BOUNTY_EVERY = 240;
const POWER_FROM = 360;
const POWER_EVERY = 120;
const WISDOM_EVERY = 420;
const WATER_TIMES = [120, 240];

function fmtClock(seconds) {
  if (seconds === null || seconds === undefined) return "--:--";
  const neg = seconds < 0;
  const abs = Math.floor(Math.abs(seconds));
  return `${neg ? "-" : ""}${Math.floor(abs / 60)}:${String(abs % 60).padStart(2, "0")}`;
}

function countdown(seconds) {
  const s = Math.max(0, Math.ceil(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

function heroDisplayName(raw) {
  if (!raw) return "Unknown Hero";
  const clean = raw.startsWith("npc_dota_hero_") ? raw.slice(14) : raw;
  const overrides = {
    antimage: "Anti-Mage",
    nevermore: "Shadow Fiend",
    windrunner: "Windranger",
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
    vengefulspirit: "Vengeful Spirit",
    keeper_of_the_light: "Keeper of the Light",
  };
  return overrides[clean] || clean.split("_").map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w)).join(" ");
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function totalGoldLost(deaths) {
  return deaths.reduce((sum, d) => sum + (d.goldLost ?? 0), 0);
}

// ---------- Settings ----------

let OV_SETTINGS = {
  opacity: 0.85,
  scale: 1,
  dota: { stats: true, roshan: true, runes: true, stacks: true, daynight: true },
  deadlock: { matchInfo: true, sessionRecord: true, lineup: false },
};

async function refreshOverlaySettings() {
  if (!invoke) return;
  try {
    const p = await invoke("get_prefs");
    if (p && p.overlay) {
      OV_SETTINGS = {
        ...OV_SETTINGS,
        ...p.overlay,
        dota: { ...OV_SETTINGS.dota, ...(p.overlay.dota || {}) },
        deadlock: { ...OV_SETTINGS.deadlock, ...(p.overlay.deadlock || {}) },
      };
      const card = document.getElementById("overlay");
      if (card) {
        card.style.opacity = String(OV_SETTINGS.opacity);
        card.style.fontSize = (13 * (OV_SETTINGS.scale || 1)).toFixed(1) + "px";
      }
    }
  } catch (_) {
    // Keep the last known settings rather than snapping back to defaults.
  }
}

// ---------- Timer maths ----------

/// Seconds until the next multiple of `every`, counting from `from`.
function nextEvery(clock, every, from = 0) {
  if (clock < from) return from - clock;
  return every - ((clock - from) % every);
}

function roshanRow(roshan, clock) {
  const alive = `
    <div class="ov-row">
      <span class="ov-label">Roshan</span>
      <span class="ov-value good">Alive</span>
    </div>`;

  if (roshan.lastDeathClock === null || roshan.lastDeathClock === undefined) return alive;

  const min = roshan.lastDeathClock + 480;
  const max = roshan.lastDeathClock + 660;

  if (clock < min) {
    return `
      <div class="ov-row">
        <span class="ov-label">Roshan</span>
        <span class="ov-value danger">${countdown(min - clock)}</span>
      </div>`;
  }
  if (clock < max) {
    return `
      <div class="ov-row">
        <span class="ov-label">Roshan &middot; maybe up</span>
        <span class="ov-value warn">${countdown(max - clock)}</span>
      </div>`;
  }
  return alive;
}

function runeRows(clock) {
  const rows = [["Bounty", countdown(nextEvery(clock, BOUNTY_EVERY))]];

  // Water runes exist only at 2:00 and 4:00; afterwards the row would be a
  // permanent "never", so it disappears instead of lying.
  const nextWater = WATER_TIMES.find((t) => t > clock);
  if (nextWater !== undefined) rows.push(["Water", countdown(nextWater - clock)]);

  rows.push([
    "Power",
    clock < POWER_FROM ? countdown(POWER_FROM - clock) : countdown(nextEvery(clock, POWER_EVERY, POWER_FROM)),
  ]);
  rows.push(["Wisdom", countdown(nextEvery(clock, WISDOM_EVERY, WISDOM_EVERY))]);

  return rows
    .map(
      ([label, val]) => `
      <div class="ov-row">
        <span class="ov-label">${label} rune</span>
        <span class="ov-value">${val}</span>
      </div>`
    )
    .join("");
}

function stackRow(clock) {
  // Camps spawn on the minute, so the pull goes out at :53.
  const intoMinute = clock % 60;
  const until = intoMinute <= 53 ? 53 - intoMinute : 113 - intoMinute;
  return `
    <div class="ov-row">
      <span class="ov-label">Stack camps</span>
      <span class="ov-value ${until <= 5 ? "warn" : ""}">${countdown(until)}</span>
    </div>`;
}

function dayNightRow(clock, isDaytime) {
  // GSI reports daytime directly, so only the 5-minute flip needs computing.
  const until = 300 - (clock % 300);
  const now = isDaytime === false ? "Night" : "Day";
  return `
    <div class="ov-row">
      <span class="ov-label">${now} &rarr; ${now === "Day" ? "night" : "day"}</span>
      <span class="ov-value">${countdown(until)}</span>
    </div>`;
}

// ---------- Renders ----------

function renderDota(m) {
  document.getElementById("ovDot").classList.add("live");
  document.getElementById("ovTitle").textContent = heroDisplayName(m.heroName);
  document.getElementById("ovClock").textContent = fmtClock(m.lastClockTime);

  const d = OV_SETTINGS.dota || {};
  const clock = m.lastClockTime || 0;
  const parts = [];

  if (d.stats) {
    parts.push(`
      <div class="ov-row">
        <span class="ov-label">Kills / deaths</span>
        <span class="ov-value">${m.kills} / ${m.deaths.length}</span>
      </div>
      <div class="ov-row">
        <span class="ov-label">Last hits / denies</span>
        <span class="ov-value">${m.lastHits} / ${m.denies}</span>
      </div>
      <div class="ov-row">
        <span class="ov-label">Gold lost to deaths</span>
        <span class="ov-value danger">${totalGoldLost(m.deaths)}g</span>
      </div>`);
  }

  const timers = d.roshan || d.runes || d.stacks || d.daynight;
  if (d.stats && timers) parts.push(`<div class="ov-sep"></div>`);
  if (d.roshan) parts.push(roshanRow(m.roshan, clock));
  if (d.runes) parts.push(runeRows(clock));
  if (d.stacks) parts.push(stackRow(clock));
  if (d.daynight) parts.push(dayNightRow(clock, m.daytime));

  if (!parts.length) {
    parts.push(`<div class="ov-hint">All panels are hidden — enable some in Overlay Settings.</div>`);
  }

  document.getElementById("ovBody").innerHTML = parts.join("");
}

function renderDeadlock(live) {
  document.getElementById("ovDot").classList.add("live");
  document.getElementById("ovTitle").textContent = `${live.heroName} — Deadlock`;
  const elapsed = live.startTime ? Math.floor(Date.now() / 1000 - live.startTime) : null;
  document.getElementById("ovClock").textContent = elapsed !== null ? fmtClock(elapsed) : "";

  const dl = OV_SETTINGS.deadlock || {};
  const parts = [];

  if (dl.matchInfo) {
    parts.push(`
      <div class="ov-row">
        <span class="ov-label">Match</span>
        <span class="ov-value">#${live.matchId}</span>
      </div>`);
  }

  if (dl.sessionRecord && OV_SESSION) {
    parts.push(`
      <div class="ov-row">
        <span class="ov-label">Today</span>
        <span class="ov-value">${OV_SESSION.wins}&ndash;${OV_SESSION.losses}</span>
      </div>`);
  }

  // Off by default. The game already shows every hero in the match, so this
  // grants no advantage — but Valve has published no position on Deadlock
  // overlays, so it stays opt-in. Never shows names, ranks or stats.
  if (dl.lineup) {
    parts.push(`
      <div class="ov-sep"></div>
      <div>
        <div class="ov-label" style="margin-bottom:4px">Your team</div>
        <div class="ov-heroes">${live.allyHeroes.map((h) => `<span class="ov-hero-chip">${esc(h)}</span>`).join("")}</div>
      </div>
      <div>
        <div class="ov-label" style="margin-bottom:4px">Opponents</div>
        <div class="ov-heroes">${live.enemyHeroes.map((h) => `<span class="ov-hero-chip">${esc(h)}</span>`).join("")}</div>
      </div>`);
  }

  parts.push(`
    <div class="ov-hint">
      Deadlock has no live stats feed, so in-match numbers aren't available.
    </div>`);

  document.getElementById("ovBody").innerHTML = parts.join("");
}

function renderIdle(message) {
  document.getElementById("ovDot").classList.remove("live");
  document.getElementById("ovTitle").textContent = "No match in progress";
  document.getElementById("ovClock").textContent = "";
  document.getElementById("ovBody").innerHTML = `<div class="ov-hint">${esc(message)}</div>`;
}

// ---------- Polling ----------

// Win/loss so far today, for the Deadlock session panel. Deadlock gives no
// live feed, so this comes from already-synced match history.
let OV_SESSION = null;

async function refreshSession() {
  if (!invoke) return;
  try {
    const data = await invoke("deadlock_overview", { limit: 50 });
    const midnight = new Date();
    midnight.setHours(0, 0, 0, 0);
    const cutoff = midnight.getTime() / 1000;
    const today = (data.matches || []).filter((m) => m.startTime >= cutoff);
    OV_SESSION = {
      wins: today.filter((m) => m.outcome === "win").length,
      losses: today.filter((m) => m.outcome === "loss").length,
    };
  } catch (_) {
    // Leave the last known record rather than flashing zeros.
  }
}

// The Dota feed is local and free to poll every second. The Deadlock check
// goes out to a rate-limited community API, so it's polled far more slowly
// and its last answer is reused in between.
const DEADLOCK_POLL_MS = 30000;
let deadlockCache = { live: null, checkedAt: 0, inFlight: false };

async function deadlockLive() {
  const now = Date.now();
  if (deadlockCache.inFlight || now - deadlockCache.checkedAt < DEADLOCK_POLL_MS) {
    return deadlockCache.live;
  }
  deadlockCache.inFlight = true;
  try {
    deadlockCache.live = await invoke("deadlock_live");
  } catch (_) {
    // The community API is allowed to be flaky; keep the last answer.
  } finally {
    deadlockCache.checkedAt = Date.now();
    deadlockCache.inFlight = false;
  }
  return deadlockCache.live;
}

async function tick() {
  if (!invoke) {
    renderIdle("Overlay preview — not running inside the app.");
    return;
  }

  // Dota wins when both are somehow live: it's the one with real-time data.
  try {
    const live = await invoke("get_live_state");
    if (live && live.current && !live.current.ended) {
      renderDota(live.current);
      return;
    }
  } catch (_) {
    /* fall through */
  }

  const dl = await deadlockLive();
  if (dl) {
    renderDeadlock(dl);
    return;
  }

  renderIdle("Start a Dota 2 match (with GSI enabled) or a Deadlock match and this fills in automatically.");
}

refreshOverlaySettings();
tick();
setInterval(tick, 1000);
// Settings change rarely, so a slow refresh keeps the window in sync
// without hammering the backend.
setInterval(refreshOverlaySettings, 3000);
// Session record only changes between games; the slow cadence also keeps
// load off the community API.
refreshSession();
setInterval(refreshSession, 120000);

// Overlay renderer. Runs in its own transparent, always-on-top window and
// polls the same Tauri commands the main window does.
//
// Dota only. Deadlock publishes no live feed, so a Deadlock overlay could
// show nothing beyond "you are in a match" — not worth floating a window
// over the game for.
//
// Everything drawn here is either the player's own GSI state or arithmetic
// on the match clock they can already see. Nothing is read out of the game
// process and nothing reveals an opponent's state.

const invoke = window.__TAURI__ ? window.__TAURI__.core.invoke : null;

// Rune timings, checked against current patch behaviour rather than memory:
//   bounty — 0:00, then every 4 minutes
//   water  — 2:00 and 4:00 only, then never again
//   power  — from 6:00, then every 2 minutes
//   wisdom — 7:00, then every 7 minutes
const BOUNTY_EVERY = 240;
const WATER_TIMES = [120, 240];
const POWER_FROM = 360;
const POWER_EVERY = 120;
const WISDOM_EVERY = 420;

// Neutral camps spawn on the minute, so the stack pull goes out at :53.
const STACK_AT = 53;

// Day and night alternate every 5 minutes.
const DAY_CYCLE = 300;

function fmtClock(seconds) {
  if (seconds === null || seconds === undefined) return "--:--";
  const neg = seconds < 0;
  const abs = Math.floor(Math.abs(seconds));
  return `${neg ? "-" : ""}${Math.floor(abs / 60)}:${String(abs % 60).padStart(2, "0")}`;
}

function countdown(seconds) {
  const s = Math.max(0, Math.floor(seconds));
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
  return (
    overrides[clean] ||
    clean.split("_").map((w) => (w ? w[0].toUpperCase() + w.slice(1) : w)).join(" ")
  );
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

function row(label, value, tone = "") {
  return `
    <div class="ov-row">
      <span class="ov-label">${label}</span>
      <span class="ov-value ${tone}">${value}</span>
    </div>`;
}

function roshanRow(roshan, clock) {
  if (roshan.lastDeathClock === null || roshan.lastDeathClock === undefined) {
    return row("Roshan", "Alive", "good");
  }
  const min = roshan.lastDeathClock + 480;
  const max = roshan.lastDeathClock + 660;

  if (clock < min) return row("Roshan", countdown(min - clock), "danger");
  if (clock < max) return row("Roshan &middot; maybe up", countdown(max - clock), "warn");
  return row("Roshan", "Alive", "good");
}

function runeRows(clock) {
  const out = [row("Bounty rune", countdown(nextEvery(clock, BOUNTY_EVERY)))];

  // Water runes exist only at 2:00 and 4:00. Afterwards the row would be a
  // permanent "never", so it disappears rather than lying.
  const nextWater = WATER_TIMES.find((t) => t > clock);
  if (nextWater !== undefined) out.push(row("Water rune", countdown(nextWater - clock)));

  out.push(
    row(
      "Power rune",
      clock < POWER_FROM
        ? countdown(POWER_FROM - clock)
        : countdown(nextEvery(clock, POWER_EVERY, POWER_FROM))
    )
  );
  out.push(row("Wisdom rune", countdown(nextEvery(clock, WISDOM_EVERY, WISDOM_EVERY))));
  return out.join("");
}

function stackRow(clock) {
  const intoMinute = clock % 60;
  const until = intoMinute <= STACK_AT ? STACK_AT - intoMinute : 60 + STACK_AT - intoMinute;
  return row("Stack camps", countdown(until), until <= 5 ? "warn" : "");
}

function dayNightRow(clock, isDaytime) {
  // GSI reports daytime directly, so only the flip needs computing.
  const until = DAY_CYCLE - (clock % DAY_CYCLE);
  const now = isDaytime === false ? "Night" : "Day";
  return row(`${now} &rarr; ${now === "Day" ? "night" : "day"}`, countdown(until));
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
    parts.push(
      row("Kills / deaths", `${m.kills} / ${m.deaths.length}`) +
        row("Last hits / denies", `${m.lastHits} / ${m.denies}`) +
        row("Gold lost", `${totalGoldLost(m.deaths)}g`, "danger")
    );
  }

  const timers = d.roshan || d.runes || d.stacks || d.daynight;
  if (d.stats && timers) parts.push(`<div class="ov-sep"></div>`);

  if (d.roshan) parts.push(roshanRow(m.roshan, clock));
  if (d.runes) parts.push(runeRows(clock));
  if (d.stacks) parts.push(stackRow(clock));
  if (d.daynight) parts.push(dayNightRow(clock, m.daytime));

  if (!parts.length) {
    parts.push(`<div class="ov-hint">All panels are hidden — turn some on in Overlay Settings.</div>`);
  }

  document.getElementById("ovBody").innerHTML = parts.join("");
}

function renderIdle(message) {
  document.getElementById("ovDot").classList.remove("live");
  document.getElementById("ovTitle").textContent = "TheTracker";
  document.getElementById("ovClock").textContent = "";
  document.getElementById("ovBody").innerHTML = `<div class="ov-hint">${esc(message)}</div>`;
}

// ---------- Loop ----------
//
// The GSI feed is local, so polling it every second costs nothing.

async function tick() {
  if (!invoke) {
    renderIdle("Overlay preview — not running inside the app.");
    return;
  }

  try {
    const live = await invoke("get_live_state");
    if (live && live.current && !live.current.ended) {
      renderDota(live.current);
      return;
    }
  } catch (_) {
    // The listener may not be up yet; the idle message below is honest either way.
  }

  renderIdle("Waiting for a Dota 2 match. Start one with GSI enabled and this fills in by itself.");
}

refreshOverlaySettings();
tick();
setInterval(tick, 1000);
// Settings change rarely, so a slow refresh keeps the window in sync without churn.
setInterval(refreshOverlaySettings, 3000);

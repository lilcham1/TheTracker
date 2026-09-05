// Overlay renderer.
//
// Dota only — Deadlock publishes no live feed, so an overlay there could
// say nothing beyond "you are in a match".
//
// The guiding rule is that a timer earns its place only while it matters.
// A wall of countdowns that are all six minutes away is noise you learn to
// ignore; a single chip that appears twenty seconds before a rune is
// something you actually use. So each piece has a relevance window, and
// outside that window it simply isn't drawn.
//
// Everything shown is either the player's own GSI state or arithmetic on
// the match clock they can already see. Nothing is read from the game
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
const DAY_CYCLE = 300;

// How early each thing is worth showing. These are the numbers that decide
// whether the overlay is useful or just busy.
const LEAD = {
  rune: 45, // a rune is worth walking to about here
  stack: 12, // enough time to send a camp
  daynight: 30, // enough to reposition before vision changes
};

let SETTINGS = {
  opacity: 0.85,
  scale: 1,
  dota: { stats: true, roshan: true, runes: true, stacks: true, daynight: true },
};

function countdown(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function totalGoldLost(deaths) {
  return deaths.reduce((sum, d) => sum + (d.goldLost ?? 0), 0);
}

/// Seconds until the next multiple of `every`, counting from `from`.
function nextEvery(clock, every, from = 0) {
  if (clock < from) return from - clock;
  return every - ((clock - from) % every);
}

function chip(label, value, tone = "") {
  return `<div class="chip ${tone}"><span class="label">${label}</span><span class="value">${value}</span></div>`;
}

async function refreshSettings() {
  if (!invoke) return;
  try {
    const p = await invoke("get_prefs");
    if (p && p.overlay) {
      SETTINGS = { ...SETTINGS, ...p.overlay, dota: { ...SETTINGS.dota, ...(p.overlay.dota || {}) } };
      const root = document.getElementById("overlay");
      if (root) {
        root.style.opacity = String(SETTINGS.opacity);
        root.style.fontSize = (12.5 * (SETTINGS.scale || 1)).toFixed(1) + "px";
      }
    }
  } catch (_) {
    // Keep the last known settings rather than snapping back to defaults.
  }
}

// ---------- Contextual pieces ----------
//
// Each returns "" when it isn't currently worth screen space.

function roshanChip(roshan, clock) {
  // Nothing to say while Roshan is simply alive — that is the default state
  // and a chip repeating it all game is pure noise.
  if (roshan.lastDeathClock === null || roshan.lastDeathClock === undefined) return "";

  const min = roshan.lastDeathClock + 480;
  const max = roshan.lastDeathClock + 660;

  if (clock < min) return chip("Roshan", countdown(min - clock), "urgent");
  if (clock < max) return chip("Roshan maybe up", countdown(max - clock), "soon");

  // Past the window he is definitely up; worth saying once he could be
  // contested, but it stops being news after a while.
  if (clock < max + 120) return chip("Roshan", "up", "good");
  return "";
}

function runeChips(clock) {
  const out = [];

  const bounty = nextEvery(clock, BOUNTY_EVERY);
  if (bounty <= LEAD.rune) out.push(chip("Bounty", countdown(bounty), bounty <= 15 ? "urgent" : "soon"));

  const water = WATER_TIMES.find((t) => t > clock);
  if (water !== undefined && water - clock <= LEAD.rune) {
    const t = water - clock;
    out.push(chip("Water", countdown(t), t <= 15 ? "urgent" : "soon"));
  }

  if (clock >= POWER_FROM - LEAD.rune) {
    const power = clock < POWER_FROM ? POWER_FROM - clock : nextEvery(clock, POWER_EVERY, POWER_FROM);
    if (power <= LEAD.rune) out.push(chip("Power", countdown(power), power <= 15 ? "urgent" : "soon"));
  }

  const wisdom = nextEvery(clock, WISDOM_EVERY, WISDOM_EVERY);
  if (wisdom <= LEAD.rune) out.push(chip("Wisdom", countdown(wisdom), wisdom <= 15 ? "urgent" : "soon"));

  return out.join("");
}

function stackChip(clock) {
  const intoMinute = clock % 60;
  const until = intoMinute <= STACK_AT ? STACK_AT - intoMinute : 60 + STACK_AT - intoMinute;
  if (until > LEAD.stack) return "";
  return chip("Stack", countdown(until), until <= 5 ? "urgent" : "soon");
}

function dayNightChip(clock, isDaytime) {
  const until = DAY_CYCLE - (clock % DAY_CYCLE);
  if (until > LEAD.daynight) return "";
  const next = isDaytime === false ? "Day" : "Night";
  return chip(next, countdown(until), "soon");
}

// ---------- Render ----------

function renderMatch(m) {
  const d = SETTINGS.dota || {};
  const clock = m.lastClockTime || 0;
  const parts = [];

  // The player's own line stays up, because it is the one thing that is
  // always relevant while playing.
  if (d.stats) {
    parts.push(
      chip("KDA", `${m.kills} / ${m.deaths.length}`) +
        chip("LH", `${m.lastHits}`) +
        (totalGoldLost(m.deaths) > 0 ? chip("Lost", `${totalGoldLost(m.deaths)}g`, "urgent") : "")
    );
  }

  if (d.roshan) parts.push(roshanChip(m.roshan, clock));
  if (d.runes) parts.push(runeChips(clock));
  if (d.stacks) parts.push(stackChip(clock));
  if (d.daynight) parts.push(dayNightChip(clock, m.daytime));

  document.getElementById("overlay").innerHTML = parts.join("");
}

function renderIdle() {
  // With auto-show on, this is only reached if the overlay was opened by
  // hand outside a match. Say why it is empty rather than showing nothing.
  document.getElementById("overlay").innerHTML =
    `<div class="hint">TheTracker — waiting for a Dota 2 match. This hides itself when no game is running.</div>`;
}

async function tick() {
  if (!invoke) {
    document.getElementById("overlay").innerHTML =
      `<div class="hint">Overlay preview — not running inside the app.</div>`;
    return;
  }

  try {
    const live = await invoke("get_live_state");
    if (live && live.current && !live.current.ended) {
      renderMatch(live.current);
      return;
    }
  } catch (_) {
    // The GSI listener may not be up yet; idle is honest either way.
  }

  renderIdle();
}

refreshSettings();
tick();
setInterval(tick, 1000);
// Settings change rarely; a slow refresh keeps this in sync without churn.
setInterval(refreshSettings, 3000);

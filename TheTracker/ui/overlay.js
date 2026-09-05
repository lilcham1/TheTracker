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

// Lotus pools spawn their first lotuses at 3:00 and every 3 minutes after.
// Turbo halves that, which is worth honouring because the whole point of a
// lotus warning is being there when they pop.
const LOTUS_FROM = 180;
const LOTUS_EVERY = 180;

// How long before an event its chip appears. One number for everything: a
// chip shows for the last ten seconds and then goes, so each event is
// announced once rather than sitting on screen counting down for a minute.
const LEAD = 10;

let SETTINGS = {
  opacity: 0.85,
  scale: 1,
  dota: { stats: true, roshan: true, runes: true, lotus: true, stacks: true, daynight: true },
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

/// Bounty every 4 minutes from 0:00, water only at 2:00 and 4:00, power
/// every 2 minutes from 6:00, wisdom every 7 from 7:00. Each shows only in
/// the last ten seconds before it lands, so it is announced once rather
/// than sitting there counting down.
function runeChips(clock) {
  const out = [];
  const due = (label, secs) => {
    if (secs > 0 && secs <= LEAD) out.push(chip(label, countdown(secs), secs <= 5 ? "urgent" : "soon"));
  };

  due("Bounty", nextEvery(clock, BOUNTY_EVERY));

  const water = WATER_TIMES.find((t) => t > clock);
  if (water !== undefined) due("Water", water - clock);

  due("Power", clock < POWER_FROM ? POWER_FROM - clock : nextEvery(clock, POWER_EVERY, POWER_FROM));
  due("Wisdom", nextEvery(clock, WISDOM_EVERY, WISDOM_EVERY));

  return out.join("");
}

/// Healing lotuses: first at 3:00, then every 3 minutes. Turbo halves both,
/// and the tracked game type is used when it is known — being late is the
/// one thing a lotus warning must not be.
function lotusChip(clock, gameType) {
  const turbo = gameType === "turbo";
  const from = turbo ? LOTUS_FROM / 2 : LOTUS_FROM;
  const every = turbo ? LOTUS_EVERY / 2 : LOTUS_EVERY;

  const secs = clock < from ? from - clock : nextEvery(clock, every, from);
  if (secs <= 0 || secs > LEAD) return "";
  return chip("Lotus", countdown(secs), secs <= 5 ? "urgent" : "soon");
}

function stackChip(clock) {
  const intoMinute = clock % 60;
  const until = intoMinute <= STACK_AT ? STACK_AT - intoMinute : 60 + STACK_AT - intoMinute;
  if (until > LEAD) return "";
  return chip("Stack", countdown(until), until <= 5 ? "urgent" : "soon");
}

function dayNightChip(clock, isDaytime) {
  const until = DAY_CYCLE - (clock % DAY_CYCLE);
  if (until > LEAD) return "";
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

  // GSI reports a clock during hero selection and strategy time as well, so
  // every timer below would otherwise count down to events in a match that
  // has not started. Only draw them once the horn has actually gone.
  if (m.inProgress && clock > 0) {
    if (d.roshan) parts.push(roshanChip(m.roshan, clock));
    if (d.runes) parts.push(runeChips(clock));
    if (d.lotus) parts.push(lotusChip(clock, m.gameType));
    if (d.stacks) parts.push(stackChip(clock));
    if (d.daynight) parts.push(dayNightChip(clock, m.daytime));
  } else if (!m.inProgress) {
    parts.push(chip("Drafting", "timers start at the horn"));
  }

  document.getElementById("overlay").innerHTML = parts.join("");
}

/// Shown when the overlay is open outside a match.
///
/// This is only reached by opening the overlay by hand, since it hides
/// itself when a game ends. The obvious thing to put here is "waiting for a
/// match" — but that is a dead end: it tells you nothing you did not
/// already know, and gives you nothing to judge position, opacity or size
/// against, which is the only reason to open the overlay outside a game.
///
/// So it renders sample chips instead, drawn from the panels actually
/// enabled. Adjusting settings then shows a real result immediately. The
/// leading chip marks it as a preview so it is never mistaken for live data.
function renderPreview() {
  const d = SETTINGS.dota || {};
  const parts = [`<div class="chip good"><span class="value">Preview</span></div>`];

  if (d.stats) parts.push(chip("KDA", "7 / 2") + chip("LH", "148"));
  if (d.roshan) parts.push(chip("Roshan", "4:12", "urgent"));
  if (d.runes) parts.push(chip("Bounty", "0:22", "soon"));
  if (d.lotus) parts.push(chip("Lotus", "0:08", "soon"));
  if (d.stacks) parts.push(chip("Stack", "0:06", "urgent"));
  if (d.daynight) parts.push(chip("Night", "0:18", "soon"));

  parts.push(
    `<div class="hint">Sample values. Real timers appear when a Dota 2 match starts, and this closes itself when it ends.</div>`
  );

  document.getElementById("overlay").innerHTML = parts.join("");
}

async function tick() {
  if (!invoke) {
    renderPreview();
    return;
  }

  try {
    const live = await invoke("get_live_state");
    if (live && live.current && !live.current.ended) {
      renderMatch(live.current);
      return;
    }
  } catch (_) {
    // The GSI listener may not be up yet; the preview below is honest either way.
  }

  renderPreview();
}

refreshSettings();
tick();
setInterval(tick, 1000);
// Settings change rarely; a slow refresh keeps this in sync without churn.
setInterval(refreshSettings, 3000);

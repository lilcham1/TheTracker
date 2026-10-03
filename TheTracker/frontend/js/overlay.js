// The in-game overlay. Dota only: Deadlock publishes no live feed.
//
// It draws one kind of thing: a countdown in the seconds before an event the
// player chose to be reminded of. The rest of the time the window is empty.
// Everything shown is arithmetic on the match clock the player can already
// see; nothing is read from the game and nothing reveals an opponent's state.

// Rune timings:
//   bounty — 0:00, then every 4 minutes
//   water  — 2:00 and 4:00 only
//   power  — from 6:00, then every 2 minutes
//   wisdom — 7:00, then every 7 minutes
const BOUNTY_EVERY = 240;
const WATER_TIMES = [120, 240];
const POWER_FROM = 360;
const POWER_EVERY = 120;
const WISDOM_EVERY = 420;
// Lotuses: first at 3:00, then every 3 minutes. Turbo halves both.
const LOTUS_FROM = 180;
const LOTUS_EVERY = 180;
// The camp pull is timed at :52 of every minute, starting at 1:52 — camps
// first spawn at 1:00, so there is nothing to stack before that. Its
// countdown always starts 7 seconds ahead, whatever the lead-time setting:
// that is the time it takes to get to the camp and line the pull up, and a
// shorter warning would arrive too late to act on.
const STACK_FROM = 112;
const STACK_LEAD = 7;

// One look per kind of reminder: its own colour and its own icon, so which
// rune it is can be read from the corner of the eye without reading a word.
// The icons are drawn here rather than loaded, so the overlay never waits on
// the network mid-fight.
const KINDS = {
  bounty: {
    label: "Bounty runes", color: "#f4c34f",
    icon: '<circle cx="12" cy="12" r="8"/><path d="M12 7l3 5-3 5-3-5z" fill="currentColor" stroke="none"/>',
  },
  water: {
    label: "Water runes", color: "#56b8f5",
    icon: '<path d="M12 3c3 4 6 7 6 11a6 6 0 0 1-12 0c0-4 3-7 6-11z" fill="currentColor" fill-opacity="0.25"/><path d="M9 14a3 3 0 0 0 3 3"/>',
  },
  power: {
    label: "Power rune", color: "#ff8452",
    icon: '<path d="M14 2L5 14h6l-1 8 9-12h-6z" fill="currentColor" fill-opacity="0.25"/>',
  },
  wisdom: {
    label: "Wisdom runes", color: "#b48cff",
    icon: '<path d="M12 7c-2-2-5-2-8-1v12c3-1 6-1 8 1 2-2 5-2 8-1V6c-3-1-6-1-8 1z" fill="currentColor" fill-opacity="0.2"/><path d="M12 7v12"/>',
  },
  lotus: {
    label: "Lotus", color: "#ff8fb8",
    icon: '<path d="M12 4c2 2 3 4 3 7s-1 4-3 6c-2-2-3-3-3-6s1-5 3-7z" fill="currentColor" fill-opacity="0.3"/><path d="M4 11c3 0 6 2 8 6 2-4 5-6 8-6 0 5-3 8-8 8s-8-3-8-8z"/>',
  },
  stack: {
    label: "Pull the camp", color: "#84d46c",
    icon: '<path d="M12 3l9 5-9 5-9-5z" fill="currentColor" fill-opacity="0.25"/><path d="M3 12l9 5 9-5M3 16l9 5 9-5"/>',
  },
};

let settings = { opacity: 0.85, scale: 1, leadSeconds: 5, corner: "top-left", dota: { runes: true, lotus: true, stacks: true } };

async function call(cmd, args) {
  const resp = await fetch("/api/" + cmd, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(args || {}) });
  if (!resp.ok) throw new Error(cmd);
  return resp.json();
}

/// Seconds until the next multiple of `every` at or after `from`.
function untilNext(clock, every, from = 0) {
  if (clock < from) return from - clock;
  const into = (clock - from) % every;
  return into === 0 ? 0 : every - into;
}

/// The reminders due now: [{kind, secs, lead}], soonest first.
function upcoming(clock, gameType) {
  const d = settings.dota || {};
  const lead = settings.leadSeconds || 5;
  const events = [];
  if (d.runes) {
    events.push({ kind: "bounty", secs: untilNext(clock, BOUNTY_EVERY), lead });
    events.push({ kind: "power", secs: untilNext(clock, POWER_EVERY, POWER_FROM), lead });
    events.push({ kind: "wisdom", secs: untilNext(clock, WISDOM_EVERY, WISDOM_EVERY), lead });
    const water = WATER_TIMES.find((t) => t > clock);
    if (water !== undefined) events.push({ kind: "water", secs: water - clock, lead });
  }
  if (d.lotus) {
    const turbo = gameType === "turbo";
    events.push({ kind: "lotus", secs: untilNext(clock, turbo ? LOTUS_EVERY / 2 : LOTUS_EVERY, turbo ? LOTUS_FROM / 2 : LOTUS_FROM), lead });
  }
  if (d.stacks) {
    events.push({ kind: "stack", secs: untilNext(clock, 60, STACK_FROM), lead: STACK_LEAD });
  }
  return events.filter((e) => e.secs > 0 && e.secs <= e.lead).sort((a, b) => a.secs - b.secs);
}

function chipHtml(e) {
  const k = KINDS[e.kind];
  const left = Math.max(0, Math.min(100, (e.secs / e.lead) * 100));
  return `<div class="chip ${e.secs <= 3 ? "soon" : ""}" style="--c:${k.color}">
    <span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${k.icon}</svg></span>
    <span class="label">${k.label}</span>
    <span class="secs">${Math.ceil(e.secs)}</span>
    <span class="bar"><i style="width:${left.toFixed(0)}%"></i></span>
  </div>`;
}

let lastHtml = "";
function paint(html) {
  if (html === lastHtml) return;
  lastHtml = html;
  document.getElementById("chips").innerHTML = html;
}

function applySettings() {
  const chips = document.getElementById("chips");
  chips.style.opacity = settings.opacity;
  chips.style.transform = `scale(${settings.scale})`;
  document.body.classList.toggle("right", /right$/.test(settings.corner));
  document.body.classList.toggle("bottom", /^bottom/.test(settings.corner));
}

async function tick() {
  let live;
  try {
    live = await call("get_live");
  } catch (_) {
    return paint("");
  }
  const m = live.current;
  // The draft and strategy time report a clock too; countdowns there would
  // be to events in a game that has not started.
  if (!live.live || !m || !m.inProgress || m.lastClockTime <= 0) return paint("");
  paint(upcoming(Math.floor(m.lastClockTime), m.gameType).map(chipHtml).join(""));
}

async function loadSettings() {
  try {
    settings = (await call("get_prefs")).overlay;
    applySettings();
  } catch (_) {
    /* keep the last settings */
  }
}

if (typeof document !== "undefined") {
  applySettings();
  loadSettings();
  setInterval(loadSettings, 2500);
  setInterval(tick, 400);
}
if (typeof module !== "undefined") module.exports = { upcoming, untilNext, chipHtml, KINDS, setSettings: (s) => (settings = s) };

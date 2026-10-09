// The in-game overlay. Dota only: Deadlock publishes no live feed.
//
// It draws countdowns in the seconds before an event the player chose to be
// reminded of, a brief "Now" when it happens, and, if switched on, one quiet
// line naming the next two events. Everything shown is arithmetic on the
// match clock the player can already see; nothing is read from the game and
// nothing reveals an opponent's state.
//
// Timings checked against Valve's patch notes up to 7.41f:
//   bounty — 0:00, then every 4 minutes (4 since 7.38)
//   water  — 2:00 and 4:00 only
//   power  — from 6:00, then every 2 minutes
//   wisdom — Shrines of Wisdom (runes until 7.38): 7:00, then every 7
//            minutes. Since 7.41 a shrine's countdown runs backwards while an
//            enemy stands in it, so a contested shrine can fill late.
//   lotus  — 3:00, then every 3 minutes; a Great Lotus once tier 4 neutral
//            items are out (35:00), a Greater Lotus after tier 5 (60:00).
//            Contested pools slow down the same way since 7.41. Turbo is
//            taken as twice as fast; no patch note confirms that.
const BOUNTY_EVERY = 240;
const WATER_TIMES = [120, 240];
const POWER_FROM = 360;
const POWER_EVERY = 120;
const WISDOM_EVERY = 420;
const LOTUS_FROM = 180;
const LOTUS_EVERY = 180;
const GREAT_LOTUS_AT = 2100;
const GREATER_LOTUS_AT = 3600;
// The camp pull is timed at :52 of every minute, starting at 1:52 — camps
// first spawn at 1:00, so there is nothing to stack before that. Its
// countdown always starts 7 seconds ahead, whatever the lead-time setting:
// that is the time it takes to get to the camp and line the pull up, and a
// shorter warning would arrive too late to act on.
const STACK_FROM = 112;
const STACK_LEAD = 7;
// How long "Now" stays up after an event, in seconds of game time.
const NOW_FOR = 2;

// One look per kind of reminder: its own colour and its own icon, so which
// rune it is can be read from the corner of the eye without reading a word.
// The icons are drawn here rather than loaded, so the overlay never waits on
// the network mid-fight.
const KINDS = {
  bounty: {
    label: "Bounty", short: "Bounty", color: "#f4c34f",
    icon: '<circle cx="12" cy="12" r="8"/><path d="M12 7l3 5-3 5-3-5z" fill="currentColor" stroke="none"/>',
  },
  water: {
    label: "Water", short: "Water", color: "#56b8f5",
    icon: '<path d="M12 3c3 4 6 7 6 11a6 6 0 0 1-12 0c0-4 3-7 6-11z" fill="currentColor" fill-opacity="0.25"/><path d="M9 14a3 3 0 0 0 3 3"/>',
  },
  power: {
    label: "Power", short: "Power", color: "#ff8452",
    icon: '<path d="M14 2L5 14h6l-1 8 9-12h-6z" fill="currentColor" fill-opacity="0.25"/>',
  },
  wisdom: {
    label: "Shrines", short: "Shrines", color: "#b48cff",
    icon: '<path d="M12 7c-2-2-5-2-8-1v12c3-1 6-1 8 1 2-2 5-2 8-1V6c-3-1-6-1-8 1z" fill="currentColor" fill-opacity="0.2"/><path d="M12 7v12"/>',
  },
  lotus: {
    label: "Lotus", short: "Lotus", color: "#ff8fb8",
    icon: '<path d="M12 4c2 2 3 4 3 7s-1 4-3 6c-2-2-3-3-3-6s1-5 3-7z" fill="currentColor" fill-opacity="0.3"/><path d="M4 11c3 0 6 2 8 6 2-4 5-6 8-6 0 5-3 8-8 8s-8-3-8-8z"/>',
  },
  stack: {
    label: "Pull camp", short: "Pull", color: "#84d46c",
    icon: '<path d="M12 3l9 5-9 5-9-5z" fill="currentColor" fill-opacity="0.25"/><path d="M3 12l9 5 9-5M3 16l9 5 9-5"/>',
  },
};

let settings = { opacity: 0.85, scale: 1, leadSeconds: 5, corner: "top-left", compact: false, nextUp: false, dota: { runes: true, lotus: true, stacks: true } };

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

/// Seconds since the last multiple of `every` at or after `from`, or
/// Infinity before the first.
function sinceLast(clock, every, from = 0) {
  return clock < from ? Infinity : (clock - from) % every;
}

/// Every kind the player switched on, with how far the next one is and how
/// long ago the last one was: [{kind, secs, since, lead, at}].
function schedule(clock, gameType) {
  const d = settings.dota || {};
  const lead = settings.leadSeconds || 5;
  const turbo = gameType === "turbo";
  const scale = turbo ? 0.5 : 1;
  const until = d.until || {};
  // Whether a reminder for an event at time t is still worth showing: within
  // its window, or (lotus) back for the Great Lotus.
  const inWindow = (kind, t) => {
    const mins = until[kind];
    if (!mins || t <= mins * 60 * scale) return true;
    return kind === "lotus" && d.lotusLate !== false && t >= GREAT_LOTUS_AT * scale;
  };
  const out = [];
  const add = (kind, every, from, kLead = lead) => {
    const secs = untilNext(clock, every, from);
    const since = sinceLast(clock, every, from);
    const at = clock + secs;
    out.push({ kind, secs: inWindow(kind, at) ? secs : Infinity, since: Number.isFinite(since) && inWindow(kind, clock - since) ? since : Infinity, lead: kLead, at });
  };
  // A rune is on when its own switch is and the old all-runes switch is.
  const rune = (k) => d.runes !== false && d[k] !== false;
  if (rune("bounty")) add("bounty", BOUNTY_EVERY, 0);
  if (rune("power")) add("power", POWER_EVERY, POWER_FROM);
  if (rune("wisdom")) add("wisdom", WISDOM_EVERY, WISDOM_EVERY);
  if (rune("water")) {
    const next = WATER_TIMES.find((w) => w >= clock);
    const last = [...WATER_TIMES].reverse().find((w) => w <= clock);
    out.push({ kind: "water", secs: next === undefined ? Infinity : next - clock, since: last === undefined ? Infinity : clock - last, lead, at: next });
  }
  if (d.lotus) add("lotus", turbo ? LOTUS_EVERY / 2 : LOTUS_EVERY, turbo ? LOTUS_FROM / 2 : LOTUS_FROM);
  if (d.stacks) add("stack", 60, STACK_FROM, STACK_LEAD);
  return out;
}

/// The reminders to show now, soonest first: counting down (`secs` > 0) or
/// just happened (`now`).
function upcoming(clock, gameType) {
  return schedule(clock, gameType)
    .map((e) => ((e.secs === 0 && e.since === 0) || e.since < NOW_FOR ? { ...e, now: true, secs: 0 } : e))
    .filter((e) => e.now || (e.secs > 0 && e.secs <= e.lead))
    .sort((a, b) => a.secs - b.secs);
}

/// The next two events that are not already on screen, for the "Next up"
/// line.
function nextUp(clock, gameType, shown) {
  const on = new Set(shown.map((e) => e.kind));
  return schedule(clock, gameType)
    .filter((e) => !on.has(e.kind) && Number.isFinite(e.secs) && e.secs > 0)
    .sort((a, b) => a.secs - b.secs)
    .slice(0, 2);
}

/// What the event is called at the time it happens: the lotus grows later in
/// the game.
function labelFor(e) {
  if (e.kind === "lotus" && e.at >= GREATER_LOTUS_AT) return "Greater lotus";
  if (e.kind === "lotus" && e.at >= GREAT_LOTUS_AT) return "Great lotus";
  return KINDS[e.kind].label;
}

const mmss = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;

/// `frac` is how far into the current second the clock is, so the bar drains
/// smoothly instead of stepping once a second.
function chipHtml(e, frac = 0) {
  const k = KINDS[e.kind];
  const left = e.now ? 0 : Math.max(0, Math.min(100, ((e.secs - frac) / e.lead) * 100));
  const state = e.now ? "is-now" : e.secs <= 3 ? "soon" : "";
  const icon = `<span class="icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${k.icon}</svg></span>`;
  const time = e.now ? `<span class="secs now">Now</span>` : `<span class="secs">${Math.ceil(e.secs)}</span>`;
  const bar = `<span class="bar"><i style="width:${left.toFixed(1)}%"></i></span>`;
  if (settings.compact) {
    return `<div class="chip compact ${state}" style="--c:${k.color}" title="${labelFor(e)}">${icon}${time}${bar}</div>`;
  }
  return `<div class="chip ${state}" style="--c:${k.color}">${icon}<span class="label">${labelFor(e)}</span>${time}${bar}</div>`;
}

function nextUpHtml(list) {
  if (!list.length) return "";
  return `<div class="next">${list.map((e) => `<span style="--c:${KINDS[e.kind].color}"><i></i>${KINDS[e.kind].short} <b>${mmss(e.secs)}</b></span>`).join("")}</div>`;
}

/// Everything drawn for a clock reading (fractional seconds allowed).
function frame(clock, gameType) {
  const whole = Math.floor(clock);
  const shown = upcoming(whole, gameType);
  const chips = shown.map((e) => chipHtml(e, clock - whole)).join("");
  return chips + (settings.nextUp ? nextUpHtml(nextUp(whole, gameType, shown)) : "");
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

// The feed reports the clock a couple of times a second. Between reports the
// clock is carried forward locally, never more than a second ahead, so a
// paused game does not run on.
let lastClock = null;
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
  if (!live.live || !m || !m.inProgress || m.lastClockTime <= 0) {
    lastClock = null;
    return paint("");
  }
  if (!lastClock || lastClock.value !== m.lastClockTime) lastClock = { value: m.lastClockTime, at: performance.now() };
  const ahead = Math.min(1, (performance.now() - lastClock.at) / 1000);
  paint(frame(m.lastClockTime + ahead, m.gameType));
}

// ?preview draws a fixed sample instead of following the match: Settings
// shows it so changes to size, opacity and style can be seen at once.
const PREVIEW = typeof location !== "undefined" && new URLSearchParams(location.search).has("preview");

function paintPreview() {
  const sample = [
    { kind: "bounty", secs: 0, now: true, lead: 5, at: 480 },
    { kind: "power", secs: 2, lead: 5, at: 480 },
    { kind: "stack", secs: 5, lead: STACK_LEAD, at: 472 },
  ];
  const next = [{ kind: "lotus", secs: 83 }, { kind: "wisdom", secs: 362 }];
  paint(sample.map((e) => chipHtml(e, 0.4)).join("") + (settings.nextUp ? nextUpHtml(next) : ""));
}

async function loadSettings() {
  try {
    settings = (await call("get_prefs")).overlay;
    applySettings();
    if (PREVIEW) paintPreview();
  } catch (_) {
    /* keep the last settings */
  }
}

if (typeof document !== "undefined") {
  applySettings();
  loadSettings();
  if (PREVIEW) {
    document.body.classList.add("preview");
    paintPreview();
  } else {
    setInterval(loadSettings, 2500);
    setInterval(tick, 250);
  }
}
if (typeof module !== "undefined") module.exports = { upcoming, nextUp, schedule, untilNext, sinceLast, chipHtml, labelFor, frame, KINDS, setSettings: (s) => (settings = s) };

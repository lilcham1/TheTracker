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
// Neutral camps spawn on the minute, so the stack pull goes out at :53.
const STACK_AT = 53;
// Lotuses: first at 3:00, then every 3 minutes. Turbo halves both.
const LOTUS_FROM = 180;
const LOTUS_EVERY = 180;

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

function upcoming(clock, gameType) {
  const d = settings.dota || {};
  const events = [];
  if (d.runes) {
    events.push(["Bounty runes", untilNext(clock, BOUNTY_EVERY)]);
    events.push(["Power rune", untilNext(clock, POWER_EVERY, POWER_FROM)]);
    events.push(["Wisdom runes", untilNext(clock, WISDOM_EVERY, WISDOM_EVERY)]);
    const water = WATER_TIMES.find((t) => t > clock);
    if (water !== undefined) events.push(["Water runes", water - clock]);
  }
  if (d.lotus) {
    const turbo = gameType === "turbo";
    events.push(["Lotus", untilNext(clock, turbo ? LOTUS_EVERY / 2 : LOTUS_EVERY, turbo ? LOTUS_FROM / 2 : LOTUS_FROM)]);
  }
  if (d.stacks) {
    const into = clock % 60;
    events.push(["Stack", into <= STACK_AT ? STACK_AT - into : 60 + STACK_AT - into]);
  }
  const lead = settings.leadSeconds || 5;
  return events.filter(([, secs]) => secs > 0 && secs <= lead).sort((a, b) => a[1] - b[1]);
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
  const clock = Math.floor(m.lastClockTime);
  paint(
    upcoming(clock, m.gameType)
      .map(([label, secs]) => `<div class="chip ${secs <= 3 ? "soon" : ""}"><span class="label">${label}</span><span class="secs">${Math.ceil(secs)}</span></div>`)
      .join("")
  );
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
if (typeof module !== "undefined") module.exports = { upcoming, untilNext, setSettings: (s) => (settings = s) };

// Overlay renderer.
//
// Dota only — Deadlock publishes no live feed, so an overlay there could
// say nothing beyond "you are in a match".
//
// It draws exactly one kind of thing: a countdown in the last five seconds
// before an event. The rest of the time the window is empty. No idle panel,
// no sample values, no "waiting for a match" — anything that sits on screen
// permanently is something you stop seeing, and a panel of numbers that
// never move looks like a hung window rather than a tracker.
//
// Everything shown is arithmetic on the match clock the player can already
// see. Nothing is read from the game process and nothing reveals an
// opponent's state.

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

// Lotus pools spawn their first lotuses at 3:00 and every 3 minutes after.
// Turbo halves that, which is worth honouring because the whole point of a
// lotus warning is being there when they pop.
const LOTUS_FROM = 180;
const LOTUS_EVERY = 180;

// Roshan is deliberately absent. His respawn window can only be timed from
// a death the player happened to see, so the countdown would be missing
// whenever it mattered most and wrong whenever the death was mistimed —
// which is worse than no countdown at all. The History tab still records
// Roshan deaths.

// How long before an event its countdown appears. Five seconds: long
// enough to react, short enough that nothing is ever on screen that isn't
// about to happen.
const LEAD = 5;

let SETTINGS = {
  opacity: 0.85,
  scale: 1,
  clickThrough: true,
  dota: { runes: true, lotus: true, stacks: true },
};

function countdown(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  return `0:0${s}`.slice(-4);
}

function chip(label, secs) {
  const tone = secs <= 2 ? "urgent" : "soon";
  return `<div class="chip ${tone}"><span class="label">${label}</span><span class="value">${countdown(secs)}</span></div>`;
}

function paint(html) {
  document.getElementById("overlay").innerHTML = html;
}

/// Seconds until the next multiple of `every`, counting from `from`.
function nextEvery(clock, every, from = 0) {
  if (clock < from) return from - clock;
  return every - ((clock - from) % every);
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

// ---------- Events ----------
//
// Each returns [label, secondsAway] pairs. Whether a pair is close enough
// to draw is decided in one place below, so no event can quietly invent its
// own lead time.

function runeEvents(clock) {
  const out = [
    ["Bounty", nextEvery(clock, BOUNTY_EVERY)],
    ["Power", clock < POWER_FROM ? POWER_FROM - clock : nextEvery(clock, POWER_EVERY, POWER_FROM)],
    ["Wisdom", nextEvery(clock, WISDOM_EVERY, WISDOM_EVERY)],
  ];

  const water = WATER_TIMES.find((t) => t > clock);
  if (water !== undefined) out.push(["Water", water - clock]);

  return out;
}

/// Healing lotuses: first at 3:00, then every 3 minutes. Turbo halves both,
/// and the tracked game type is used when it is known — being late is the
/// one thing a lotus warning must not be.
function lotusEvents(clock, gameType) {
  const turbo = gameType === "turbo";
  const from = turbo ? LOTUS_FROM / 2 : LOTUS_FROM;
  const every = turbo ? LOTUS_EVERY / 2 : LOTUS_EVERY;
  return [["Lotus", clock < from ? from - clock : nextEvery(clock, every, from)]];
}

function stackEvents(clock) {
  const into = clock % 60;
  return [["Stack", into <= STACK_AT ? STACK_AT - into : 60 + STACK_AT - into]];
}

// ---------- Render ----------

function renderMatch(m) {
  const d = SETTINGS.dota || {};
  const clock = m.lastClockTime || 0;

  // GSI reports a clock during hero selection and strategy time as well, so
  // every timer below would otherwise count down to events in a match that
  // has not started.
  if (!m.inProgress || clock <= 0) {
    paint("");
    return;
  }

  const events = [];
  if (d.runes) events.push(...runeEvents(clock));
  if (d.lotus) events.push(...lotusEvents(clock, m.gameType));
  if (d.stacks) events.push(...stackEvents(clock));

  paint(
    events
      .filter(([, secs]) => secs > 0 && secs <= LEAD)
      .sort((a, b) => a[1] - b[1])
      .map(([label, secs]) => chip(label, secs))
      .join("")
  );
}

async function tick() {
  // Unlocked for positioning: a transparent empty window cannot be dragged
  // to a corner you can see, so one marker stands in until it is locked
  // again. This is the only thing that ever shows outside the last five
  // seconds before an event.
  if (SETTINGS.clickThrough === false) {
    paint(`<div class="chip good"><span class="value">Drag to place, then lock</span></div>`);
    return;
  }

  if (!invoke) {
    paint("");
    return;
  }

  let live;
  try {
    live = await invoke("get_live_state");
  } catch (_) {
    paint("");
    return;
  }

  if (live && live.current && !live.current.ended) {
    renderMatch(live.current);
  } else {
    paint("");
  }
}

refreshSettings();
tick();
setInterval(tick, 1000);
// Settings change rarely; a slow refresh keeps this in sync without churn.
setInterval(refreshSettings, 3000);

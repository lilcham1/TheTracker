// Overlay renderer. Runs in its own transparent window and polls the same
// Tauri commands the main window does. Deliberately shows only what the
// player already has access to: their own Dota GSI feed, and whether they
// are currently in a Deadlock match.

const invoke = window.__TAURI__ ? window.__TAURI__.core.invoke : null;
const CHECKPOINT_MINUTES = [5, 10, 15, 20, 25];

function fmtClock(seconds) {
  if (seconds === null || seconds === undefined) return "--:--";
  const neg = seconds < 0;
  const abs = Math.floor(Math.abs(seconds));
  return `${neg ? "-" : ""}${Math.floor(abs / 60)}:${String(abs % 60).padStart(2, "0")}`;
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

function roshanBlock(roshan, clockTime) {
  if (roshan.lastDeathClock === null || roshan.lastDeathClock === undefined) return "";
  const min = roshan.lastDeathClock + 480;
  const max = roshan.lastDeathClock + 660;
  const countdown = (t) => {
    const rem = Math.max(0, t - clockTime);
    return `${Math.floor(rem / 60)}:${String(Math.floor(rem % 60)).padStart(2, "0")}`;
  };
  let cls = "alive";
  let status = "Roshan up";
  let sub = "Respawn window passed";
  if (clockTime < min) {
    cls = "dead";
    status = "Roshan down";
    sub = `Earliest respawn in ${countdown(min)}`;
  } else if (clockTime < max) {
    cls = "maybe";
    status = "Roshan may be up";
    sub = `Guaranteed in ${countdown(max)}`;
  }
  return `
    <div class="ov-rosh">
      <div class="ov-rosh-status ${cls}">🐉 ${status}</div>
      <div class="ov-rosh-sub">${sub}</div>
    </div>`;
}

function renderDota(m) {
  document.getElementById("ovDot").classList.add("live");
  document.getElementById("ovTitle").textContent = heroDisplayName(m.heroName);
  document.getElementById("ovClock").textContent = fmtClock(m.lastClockTime);

  const cps = CHECKPOINT_MINUTES.map((min) => {
    const cp = m.checkpoints[min];
    return `<div class="ov-cp">
        <div class="ov-cp-min">${min}m</div>
        <div class="ov-cp-val ${cp ? "" : "pending"}">${cp ? cp.lastHits : "—"}</div>
      </div>`;
  }).join("");

  document.getElementById("ovBody").innerHTML = `
    <div class="ov-row">
      <span class="ov-label">Last hits / denies</span>
      <span class="ov-value">${m.lastHits} / ${m.denies}</span>
    </div>
    <div class="ov-row">
      <span class="ov-label">Deaths / gold lost</span>
      <span class="ov-value danger">${m.deaths.length} / ${totalGoldLost(m.deaths)}g</span>
    </div>
    <div class="ov-row">
      <span class="ov-label">Kills</span>
      <span class="ov-value">${m.kills}</span>
    </div>
    ${roshanBlock(m.roshan, m.lastClockTime)}
    <div class="ov-checkpoints">${cps}</div>
  `;
}

function renderDeadlock(live) {
  document.getElementById("ovDot").classList.add("live");
  document.getElementById("ovTitle").textContent = `${live.heroName} — Deadlock`;
  const elapsed = live.startTime ? Math.floor(Date.now() / 1000 - live.startTime) : null;
  document.getElementById("ovClock").textContent = elapsed !== null ? fmtClock(elapsed) : "";

  document.getElementById("ovBody").innerHTML = `
    <div class="ov-row">
      <span class="ov-label">Match</span>
      <span class="ov-value">#${live.matchId}</span>
    </div>
    <div>
      <div class="ov-label" style="margin-bottom:4px">Your team</div>
      <div class="ov-heroes">${live.allyHeroes.map((h) => `<span class="ov-hero-chip">${esc(h)}</span>`).join("")}</div>
    </div>
    <div>
      <div class="ov-label" style="margin-bottom:4px">Opponents</div>
      <div class="ov-heroes">${live.enemyHeroes.map((h) => `<span class="ov-hero-chip">${esc(h)}</span>`).join("")}</div>
    </div>
    <div class="ov-hint">
      Deadlock has no live stats feed, so in-match numbers aren't available —
      this match's stats appear in the app once it ends.
    </div>
  `;
}

function renderIdle(message) {
  document.getElementById("ovDot").classList.remove("live");
  document.getElementById("ovTitle").textContent = "No match in progress";
  document.getElementById("ovClock").textContent = "";
  document.getElementById("ovBody").innerHTML = `<div class="ov-hint">${esc(message)}</div>`;
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

tick();
setInterval(tick, 1000);

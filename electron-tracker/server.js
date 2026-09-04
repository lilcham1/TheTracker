// Dota 2 Match Tracker — Game State Integration server
// No dependencies needed to run this file directly. Run with: node server.js

const http = require('http');
const fs = require('fs');
const path = require('path');

const PORT = 3000;
const LOG_DIR = process.env.DOTA_TRACKER_LOG_DIR || path.join(__dirname, 'logs');
if (!fs.existsSync(LOG_DIR)) fs.mkdirSync(LOG_DIR, { recursive: true });
const HISTORY_FILE = path.join(LOG_DIR, 'history.json');
const PROFILE_FILE = path.join(LOG_DIR, 'profile.json');

const CHECKPOINT_MINUTES = [5, 10, 15, 20, 25];

const HERO_CDN = 'https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/';
const ITEM_CDN = 'https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/items/';

const GAME_TYPES = ['ranked', 'unranked', 'turbo', 'other'];
const GAME_TYPE_LABELS = { ranked: 'Ranked', unranked: 'Unranked', turbo: 'Turbo', other: 'Other', unspecified: 'Unspecified' };

const RANKS = [
  { id: 'herald',   label: 'Herald',   color: '#a0a0a0' },
  { id: 'guardian', label: 'Guardian', color: '#5fa85f' },
  { id: 'crusader', label: 'Crusader', color: '#4fa8c9' },
  { id: 'archon',   label: 'Archon',   color: '#4f8fd1' },
  { id: 'legend',   label: 'Legend',   color: '#8f6fd1' },
  { id: 'ancient',  label: 'Ancient',  color: '#d14f6f' },
  { id: 'divine',   label: 'Divine',   color: '#4fd1c9' },
  { id: 'immortal', label: 'Immortal', color: '#f0a020' },
];
const ROLES = [
  { id: 'carry',         label: 'Carry',         color: '#e05b5b' },
  { id: 'mid',           label: 'Mid',            color: '#e0c05b' },
  { id: 'offlane',       label: 'Offlane',        color: '#a05be0' },
  { id: 'soft_support',  label: 'Soft Support',   color: '#5be0a0' },
  { id: 'hard_support',  label: 'Hard Support',   color: '#5b9be0' },
];

const KEY_ITEMS = new Set([
  'boots', 'boots_of_elves', 'phase_boots', 'power_treads', 'arcane_boots',
  'tranquil_boots', 'travel_boots', 'travel_boots_2', 'guardian_greaves',
  'blink', 'overwhelming_blink', 'swift_blink', 'arcane_blink',
  'black_king_bar', 'aghanims_scepter', 'ultimate_scepter', 'aghanims_shard',
  'refresher', 'refresher_shard', 'heart', 'assault', 'shivas_guard',
  'satanic', 'skadi', 'manta', 'sange_and_yasha', 'yasha_and_kaya',
  'daedalus', 'butterfly', 'silver_edge', 'nullifier', 'abyssal_blade',
  'monkey_king_bar', 'linkens_sphere', 'sheepstick', 'rod_of_atos',
  'eye_of_skadi', 'octarine_core', 'bloodthorn', 'dagon', 'dagon_5',
]);

const HERO_NAME_OVERRIDES = {
  antimage: 'Anti-Mage', nevermore: 'Shadow Fiend', windrunner: 'Windranger',
  vengefulspirit: 'Vengeful Spirit', queenofpain: 'Queen of Pain',
  skeleton_king: 'Wraith King', doom_bringer: 'Doom', necrolyte: 'Necrophos',
  furion: "Nature's Prophet", life_stealer: 'Lifestealer', rattletrap: 'Clockwerk',
  obsidian_destroyer: 'Outworld Destroyer', treant: 'Treant Protector', wisp: 'Io',
  zuus: 'Zeus', shredder: 'Timbersaw', magnataur: 'Magnus',
  centaur: 'Centaur Warrunner', abyssal_underlord: 'Underlord',
  keeper_of_the_light: 'Keeper of the Light',
};

function heroCleanName(rawName) { return (rawName || '').replace('npc_dota_hero_', '') || null; }
function heroDisplayName(rawName) {
  const clean = heroCleanName(rawName);
  if (!clean) return 'Unknown Hero';
  if (HERO_NAME_OVERRIDES[clean]) return HERO_NAME_OVERRIDES[clean];
  return clean.split('_').map(w => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
}
function roshanDrops(deathCount) {
  if (deathCount <= 1) return 'Aegis of the Immortal';
  if (deathCount === 2) return 'Aegis + Cheese + (Refresher Shard or Aghanim\'s Blessing)';
  return 'Aegis + Cheese + Aghanim\'s Blessing + Refresher Shard';
}

let current = null;
let trackingEnabled = true;

function fmtClock(seconds) {
  if (seconds == null) return '??:??';
  const neg = seconds < 0;
  const abs = Math.abs(Math.floor(seconds));
  const m = Math.floor(abs / 60);
  const s = abs % 60;
  return `${neg ? '-' : ''}${m}:${s.toString().padStart(2, '0')}`;
}
function log(line) { console.log(`[${new Date().toLocaleTimeString()}] ${line}`); }

function loadHistory() {
  try { return JSON.parse(fs.readFileSync(HISTORY_FILE, 'utf8')); } catch (e) { return []; }
}
function saveHistory(history) { fs.writeFileSync(HISTORY_FILE, JSON.stringify(history, null, 2)); }

function loadProfile() {
  try { return JSON.parse(fs.readFileSync(PROFILE_FILE, 'utf8')); }
  catch (e) { return { username: '', rank: null, role: null }; }
}
function saveProfile(profile) { fs.writeFileSync(PROFILE_FILE, JSON.stringify(profile, null, 2)); }

function newMatch(matchid, heroNameRaw) {
  return {
    matchid, heroName: heroNameRaw || null, startedAt: new Date().toISOString(),
    wasAlive: true, ownedItemCounts: {}, deaths: [], keyItemLog: [],
    checkpoints: Object.fromEntries(CHECKPOINT_MINUTES.map(m => [m, null])),
    lastClockTime: 0, lastHits: 0, denies: 0, kills: 0, prevGold: null,
    ended: false, summary: null, gameType: 'unspecified',
    roshan: { deaths: 0, lastDeathClock: null, wasAlive: true },
  };
}
function markRoshanDeath(match, clockTime, source) {
  match.roshan.deaths += 1;
  match.roshan.lastDeathClock = clockTime;
  match.roshan.wasAlive = false;
  log(`🐉 Roshan death #${match.roshan.deaths} at ${fmtClock(clockTime)} (${source}) — drops: ${roshanDrops(match.roshan.deaths)}`);
}
function bestValue(history, fn, mode) {
  const vals = history.map(fn).filter(v => v != null);
  if (!vals.length) return null;
  return mode === 'min' ? Math.min(...vals) : Math.max(...vals);
}
function compareMetric(value, avg, higherIsBetter) {
  if (avg == null || value == null) return { value, avg: null, verdict: 'no_data', isBest: false };
  const diff = value - avg;
  const threshold = Math.max(0.5, Math.abs(avg) * 0.08);
  let verdict = 'similar';
  if (Math.abs(diff) > threshold) verdict = (higherIsBetter ? diff > 0 : diff < 0) ? 'better' : 'worse';
  return { value, avg: Math.round(avg * 10) / 10, verdict, isBest: false };
}
function buildSummary(match) {
  const totalGoldLost = match.deaths.reduce((sum, d) => sum + (d.goldLost || 0), 0);
  return {
    matchid: match.matchid, heroName: match.heroName, date: new Date().toISOString(),
    duration: fmtClock(match.lastClockTime), kills: match.kills,
    totalDeaths: match.deaths.length, totalGoldLost, deaths: match.deaths,
    keyItems: match.keyItemLog, checkpoints: match.checkpoints,
    roshanDeaths: match.roshan.deaths, gameType: match.gameType,
  };
}
function finalizeMatch(match) {
  const summary = buildSummary(match);
  const fullHistory = loadHistory();
  const peers = fullHistory.filter(m => (m.gameType || 'unspecified') === (summary.gameType || 'unspecified'));
  const avg = (fn) => {
    const vals = peers.map(fn).filter(v => v != null);
    return vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null;
  };
  const comparison = {
    deaths: compareMetric(summary.totalDeaths, avg(m => m.totalDeaths), false),
    goldLost: compareMetric(summary.totalGoldLost, avg(m => m.totalGoldLost), false),
    checkpoints: {},
  };
  comparison.deaths.isBest = (() => { const b = bestValue(peers, m => m.totalDeaths, 'min'); return b != null && summary.totalDeaths < b; })();
  comparison.goldLost.isBest = (() => { const b = bestValue(peers, m => m.totalGoldLost, 'min'); return b != null && summary.totalGoldLost < b; })();
  for (const min of CHECKPOINT_MINUTES) {
    const value = summary.checkpoints[min]?.lastHits ?? null;
    const avgVal = avg(m => m.checkpoints?.[min]?.lastHits ?? null);
    const comp = compareMetric(value, avgVal, true);
    const best = bestValue(peers, m => m.checkpoints?.[min]?.lastHits ?? null, 'max');
    comp.isBest = value != null && best != null && value > best;
    comparison.checkpoints[min] = comp;
  }
  summary.comparison = comparison;
  summary.gamesComparedAgainst = peers.length;
  fullHistory.push(summary);
  saveHistory(fullHistory);
  match.ended = true;
  match.summary = summary;
  log(`🏁 Match ended — ${summary.totalDeaths} deaths, ${summary.totalGoldLost}g lost, ${peers.length} past ${GAME_TYPE_LABELS[summary.gameType || 'unspecified']} games to compare against`);
}

function handleUpdate(body) {
  if (!trackingEnabled) return;
  const map = body.map || {}, player = body.player || {}, hero = body.hero || {}, items = body.items || {};
  if (player.activity && player.activity !== 'playing') return;
  const matchid = map.matchid;
  if (!matchid || matchid === '0') return;

  if (!current || current.matchid !== matchid) {
    current = newMatch(matchid, hero.name);
    log(`=== New match detected (${matchid}) ===`);
  }
  if (current.ended) return;

  const clockTime = typeof map.clock_time === 'number' ? map.clock_time : current.lastClockTime;
  current.lastClockTime = clockTime;
  if (hero.name) current.heroName = hero.name;
  if (typeof player.kills === 'number') current.kills = player.kills;
  const gold = typeof player.gold === 'number' ? player.gold : null;

  const alive = hero.alive;
  if (typeof alive === 'boolean') {
    if (current.wasAlive === true && alive === false) {
      const goldLost = gold != null && current.prevGold != null ? Math.max(0, current.prevGold - gold) : null;
      const entry = { clock: fmtClock(clockTime), goldLost };
      current.deaths.push(entry);
      log(`💀 Death at ${entry.clock} — lost ${goldLost != null ? goldLost + 'g' : '?g'}`);
    }
    current.wasAlive = alive;
  }
  if (gold != null) current.prevGold = gold;

  if (typeof player.last_hits === 'number') current.lastHits = player.last_hits;
  if (typeof player.denies === 'number') current.denies = player.denies;
  for (const minute of CHECKPOINT_MINUTES) {
    if (clockTime >= minute * 60 && current.checkpoints[minute] === null) {
      current.checkpoints[minute] = { lastHits: current.lastHits, denies: current.denies };
      log(`⏱  ${minute}min — ${current.lastHits} LH / ${current.denies} DN`);
    }
  }

  if (typeof map.roshan_state === 'string') {
    const state = map.roshan_state.toLowerCase();
    if (state.includes('dead') && current.roshan.wasAlive) markRoshanDeath(current, clockTime, 'auto');
    else if (state.includes('alive')) current.roshan.wasAlive = true;
  }

  const currentCounts = {};
  for (const [slot, itemData] of Object.entries(items)) {
    if (!slot.startsWith('slot') && !slot.startsWith('teleport') && !slot.startsWith('neutral')) continue;
    const rawName = itemData && itemData.name && itemData.name !== 'empty' ? itemData.name : null;
    if (!rawName) continue;
    const cleanName = rawName.replace('item_', '');
    currentCounts[cleanName] = (currentCounts[cleanName] || 0) + 1;
  }
  for (const [itemName, count] of Object.entries(currentCounts)) {
    if (!KEY_ITEMS.has(itemName)) continue;
    const prevCount = current.ownedItemCounts[itemName] || 0;
    if (count > prevCount) {
      for (let i = 0; i < count - prevCount; i++) {
        const entry = { clock: fmtClock(clockTime), item: itemName };
        current.keyItemLog.push(entry);
        log(`⭐ ${itemName} at ${entry.clock}`);
      }
    }
  }
  current.ownedItemCounts = currentCounts;

  if (map.game_state === 'DOTA_GAMERULES_STATE_POST_GAME' && !current.ended) finalizeMatch(current);
}

const DASHBOARD_HTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Dota Tracker</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Cinzel:wght@600;700&family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
  :root { color-scheme: dark; }
  * { box-sizing: border-box; }
  body {
    font-family: 'Inter', -apple-system, sans-serif;
    background: radial-gradient(ellipse at top, #1a1310 0%, #0c0a0d 55%, #08070a 100%);
    color: #e8e4de; margin: 0; display: flex; justify-content: center; min-height: 100vh;
  }
  .app { width: 100%; max-width: 660px; padding: 24px 20px 60px; }

  .brand { text-align: center; margin-bottom: 20px; }
  .brand h1 {
    font-family: 'Cinzel', serif; font-size: 22px; letter-spacing: 0.08em;
    margin: 0; background: linear-gradient(180deg, #f0c060, #c98a2e);
    -webkit-background-clip: text; background-clip: text; color: transparent;
  }
  .brand .sub { font-size: 11px; color: #6b6258; letter-spacing: 0.15em; text-transform: uppercase; margin-top: 2px; }

  .profile-chip { display: flex; align-items: center; justify-content: center; gap: 8px; margin-bottom: 18px; font-size: 12px; color: #9a9088; }
  .rank-pill { padding: 2px 10px; border-radius: 999px; font-weight: 600; font-size: 11px; }
  .role-pill { padding: 2px 10px; border-radius: 999px; font-weight: 600; font-size: 11px; }

  .top-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 14px; }
  .switch-wrap { display: flex; align-items: center; gap: 8px; font-size: 13px; color: #9a9088; }
  .switch { width: 40px; height: 22px; border-radius: 999px; background: #332b24; position: relative; cursor: pointer; transition: background 0.15s; }
  .switch.on { background: #3a6d3a; }
  .switch .knob { width: 18px; height: 18px; border-radius: 50%; background: #f0e6d8; position: absolute; top: 2px; left: 2px; transition: left 0.15s; }
  .switch.on .knob { left: 20px; }

  .tabs { display: flex; gap: 4px; background: #17120f; border: 1px solid #2a221b; border-radius: 10px; padding: 4px; margin-bottom: 22px; }
  .tab { flex: 1; text-align: center; padding: 10px; border-radius: 8px; cursor: pointer; font-size: 13px; color: #7d7268; user-select: none; font-weight: 500; }
  .tab.active { background: linear-gradient(180deg, #3a2f22, #241c14); color: #f0c060; }
  .hidden { display: none; }

  .hero-row { display: flex; align-items: center; gap: 12px; margin-bottom: 14px; }
  .hero-portrait { width: 48px; height: 48px; border-radius: 8px; object-fit: cover; background: #17120f; border: 1px solid #33291d; }
  .hero-info { flex: 1; }
  .hero-name { font-size: 20px; font-weight: 700; }
  .clock { font-size: 13px; color: #8a8074; font-variant-numeric: tabular-nums; }

  .gametype-row { display: flex; gap: 6px; margin-bottom: 18px; }
  .gametype-btn { flex: 1; text-align: center; padding: 7px 4px; border-radius: 8px; background: #17120f; color: #7d7268; font-size: 12px; cursor: pointer; border: 1px solid #241c14; }
  .gametype-btn.active { background: linear-gradient(180deg, #3a2f22, #241c14); color: #f0c060; border-color: #4a3b28; }

  .stat-row { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; margin-bottom: 20px; }
  .stat-box { background: #17120f; border: 1px solid #241c14; border-radius: 12px; padding: 16px; }
  .stat-box .label { font-size: 11px; color: #7d7268; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 6px; }
  .stat-box .value { font-size: 26px; font-weight: 700; color: #ede6da; }
  .stat-box .value.red { color: #e0685f; }

  .section { margin-bottom: 22px; }
  .section-title { font-size: 12px; color: #7d7268; text-transform: uppercase; letter-spacing: 0.06em; margin-bottom: 10px; font-weight: 600; }

  .checkpoints { display: flex; gap: 8px; }
  .checkpoint { flex: 1; background: #17120f; border: 1px solid #241c14; border-radius: 10px; padding: 10px 4px; text-align: center; }
  .checkpoint .min { font-size: 11px; color: #6b6258; }
  .checkpoint .val { font-size: 16px; font-weight: 700; margin-top: 4px; color: #ede6da; }
  .checkpoint.pending .val { color: #443c33; }

  .item-grid { display: flex; flex-wrap: wrap; gap: 10px; }
  .item-card { position: relative; width: 64px; height: 48px; border-radius: 6px; overflow: hidden; background: #17120f; border: 1px solid #33291d; }
  .item-card img { width: 100%; height: 100%; object-fit: cover; }
  .item-clock { position: absolute; bottom: 0; left: 0; right: 0; background: rgba(0,0,0,0.8); color: #f0c060; font-size: 10px; text-align: center; padding: 2px 0; font-variant-numeric: tabular-nums; }

  .list-item { display: flex; justify-content: space-between; padding: 10px 14px; background: #17120f; border: 1px solid #241c14; border-radius: 8px; margin-bottom: 6px; font-size: 14px; }
  .list-item .death-gold { color: #e0685f; }
  .empty-note { color: #544c43; font-style: italic; font-size: 13px; padding: 8px 0; }

  .rosh-card { background: #17120f; border: 1px solid #241c14; border-radius: 12px; padding: 16px; }
  .rosh-status { font-size: 16px; font-weight: 700; margin-bottom: 4px; }
  .rosh-status.dead { color: #e0685f; }
  .rosh-status.maybe { color: #e0a95f; }
  .rosh-status.alive { color: #6fd18a; }
  .rosh-sub { font-size: 12px; color: #8a8074; margin-bottom: 10px; }
  .rosh-btn { background: linear-gradient(180deg, #3a2f22, #241c14); color: #f0c060; border: 1px solid #4a3b28; border-radius: 8px; padding: 8px 14px; font-size: 13px; cursor: pointer; font-weight: 600; }

  .summary-card { background: linear-gradient(160deg, #241c14, #17120f); border-radius: 14px; padding: 20px; margin-bottom: 24px; border: 1px solid #3a2f22; }
  .summary-card h2 { margin: 0 0 4px; font-size: 18px; font-family: 'Cinzel', serif; color: #f0c060; }
  .summary-sub { color: #8a8074; font-size: 13px; margin-bottom: 16px; }
  .type-tag { display: inline-block; font-size: 10px; padding: 2px 8px; border-radius: 999px; background: #241c14; color: #b0a696; margin-left: 8px; text-transform: uppercase; letter-spacing: 0.03em; border: 1px solid #3a2f22; }

  .compare-row { display: flex; align-items: center; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #241c14; font-size: 14px; }
  .compare-row:last-child { border-bottom: none; }
  .compare-label { color: #d0c8ba; }
  .compare-value { display: flex; align-items: center; gap: 8px; font-variant-numeric: tabular-nums; }
  .badge { font-size: 11px; padding: 2px 8px; border-radius: 999px; font-weight: 600; }
  .badge.better { background: #1c3320; color: #6fd18a; }
  .badge.worse { background: #331c1c; color: #e0685f; }
  .badge.similar { background: #241c14; color: #9a9088; }
  .badge.no_data { background: #241c14; color: #6b6258; }

  .history-item { background: #17120f; border: 1px solid #241c14; border-radius: 10px; padding: 14px 16px; margin-bottom: 8px; cursor: pointer; }
  .history-item .top { display: flex; justify-content: space-between; align-items: center; margin-bottom: 4px; }
  .history-item .hname { font-weight: 700; }
  .history-item .date { color: #6b6258; font-size: 12px; }
  .history-item .metrics { color: #9a9088; font-size: 13px; }
  .history-detail { margin-top: 12px; padding-top: 12px; border-top: 1px solid #241c14; }

  .profile-form { background: #17120f; border: 1px solid #241c14; border-radius: 12px; padding: 20px; }
  .profile-form label { display: block; font-size: 12px; color: #7d7268; text-transform: uppercase; letter-spacing: 0.05em; margin-bottom: 8px; margin-top: 18px; font-weight: 600; }
  .profile-form label:first-child { margin-top: 0; }
  .profile-form input[type=text] {
    width: 100%; background: #0c0a0d; border: 1px solid #33291d; border-radius: 8px;
    padding: 10px 12px; color: #ede6da; font-size: 14px; font-family: 'Inter', sans-serif;
  }
  .pill-row { display: flex; flex-wrap: wrap; gap: 8px; }
  .pill-choice { padding: 8px 14px; border-radius: 999px; font-size: 13px; cursor: pointer; border: 1.5px solid #33291d; background: #0c0a0d; color: #9a9088; font-weight: 600; }
  .pill-choice.active { color: #0c0a0d; }
  .save-btn {
    margin-top: 22px; width: 100%; padding: 12px; border-radius: 8px; border: none;
    background: linear-gradient(180deg, #f0c060, #c98a2e); color: #1a1310; font-weight: 700;
    font-size: 14px; cursor: pointer;
  }
  .saved-note { text-align: center; color: #6fd18a; font-size: 12px; margin-top: 10px; height: 14px; }
</style>
</head>
<body>
<div class="app">
  <div class="brand"><h1>⚔ DOTA TRACKER</h1><div class="sub">Match Intelligence</div></div>
  <div class="profile-chip" id="profile-chip"></div>

  <div class="top-row">
    <div class="tabs" style="flex:1; margin-bottom:0;">
      <div class="tab active" data-tab="live">Live</div>
      <div class="tab" data-tab="history">History</div>
      <div class="tab" data-tab="leaderboard">Leaderboard</div>
      <div class="tab" data-tab="profile">Profile</div>
    </div>
  </div>
  <div class="switch-wrap" style="margin-bottom:20px; justify-content:center;">
    <div class="switch" id="tracking-switch"><div class="knob"></div></div>
    <span id="tracking-label">Tracking ON</span>
  </div>

  <div id="live-tab">
    <div id="live-content"><div class="empty-note">Waiting for a match to start...</div></div>
  </div>
  <div id="history-tab" class="hidden">
    <div id="history-content"><div class="empty-note">No finished matches yet.</div></div>
  </div>
  <div id="leaderboard-tab" class="hidden">
    <div class="gametype-row" id="lb-type-row"></div>
    <div class="gametype-row" id="lb-metric-row" style="margin-bottom:20px;"></div>
    <div id="leaderboard-content"><div class="empty-note">No finished matches yet.</div></div>
  </div>
  <div id="profile-tab" class="hidden">
    <div class="profile-form">
      <label>Username</label>
      <input type="text" id="profile-username" placeholder="Your name">
      <label>Rank</label>
      <div class="pill-row" id="profile-ranks"></div>
      <label>Main Role</label>
      <div class="pill-row" id="profile-roles"></div>
      <button class="save-btn" onclick="saveProfile()">Save Profile</button>
      <div class="saved-note" id="profile-saved"></div>
    </div>
  </div>
</div>

<script>
const HERO_CDN = '${HERO_CDN}';
const ITEM_CDN = '${ITEM_CDN}';
const GAME_TYPES = ${JSON.stringify(GAME_TYPES)};
const GAME_TYPE_LABELS = ${JSON.stringify(GAME_TYPE_LABELS)};
const RANKS = ${JSON.stringify(RANKS)};
const ROLES = ${JSON.stringify(ROLES)};

let profileState = { username: '', rank: null, role: null };

document.querySelectorAll('.tab').forEach(t => t.addEventListener('click', () => {
  document.querySelectorAll('.tab').forEach(x => x.classList.remove('active'));
  t.classList.add('active');
  ['live','history','leaderboard','profile'].forEach(tab => document.getElementById(tab+'-tab').classList.toggle('hidden', t.dataset.tab !== tab));
  if (t.dataset.tab === 'history') refreshHistory();
  if (t.dataset.tab === 'leaderboard') refreshLeaderboard();
}));

const trackingSwitch = document.getElementById('tracking-switch');
trackingSwitch.addEventListener('click', async () => {
  const res = await fetch('/toggle', { method: 'POST' });
  const d = await res.json();
  setSwitchState(d.trackingEnabled);
});
function setSwitchState(on) {
  trackingSwitch.classList.toggle('on', on);
  document.getElementById('tracking-label').textContent = 'Tracking ' + (on ? 'ON' : 'OFF');
}

async function markRoshanDeath() { await fetch('/roshan-death', { method: 'POST' }); refreshLive(); }
async function setGameType(type) {
  await fetch('/set-game-type', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ type }) });
  refreshLive();
}

function badge(verdict) {
  const labels = { better: 'Better', worse: 'Worse', similar: 'Average', no_data: 'New' };
  return '<span class="badge ' + verdict + '">' + labels[verdict] + '</span>';
}
function renderComparisonRow(label, comp) {
  const valStr = comp.value != null ? comp.value : '—';
  const avgStr = comp.avg != null ? ' (avg ' + comp.avg + ')' : '';
  const trophy = comp.isBest ? ' 🏆' : '';
  return '<div class="compare-row"><span class="compare-label">' + label + '</span>' +
    '<span class="compare-value">' + valStr + avgStr + ' ' + badge(comp.verdict) + trophy + '</span></div>';
}
function heroDisplay(rawName) {
  if (!rawName) return 'Unknown Hero';
  return rawName.replace('npc_dota_hero_', '').split('_').map(w => w.charAt(0).toUpperCase()+w.slice(1)).join(' ');
}
function heroCleanName(rawName) { return (rawName||'').replace('npc_dota_hero_',''); }

function renderComparisonBlock(s) {
  const typeLabel = GAME_TYPE_LABELS[s.gameType || 'unspecified'];
  let html = '<div class="summary-sub">vs your last ' + s.gamesComparedAgainst + ' ' + typeLabel + ' games</div>';
  html += renderComparisonRow('Deaths', s.comparison.deaths);
  html += renderComparisonRow('Gold Lost to Deaths', s.comparison.goldLost);
  for (const min of [5,10,15,20,25]) {
    if (s.comparison.checkpoints[min] && s.comparison.checkpoints[min].value != null) {
      html += renderComparisonRow(min + ' min Last Hits', s.comparison.checkpoints[min]);
    }
  }
  return html;
}
function renderSummary(s) {
  const typeLabel = GAME_TYPE_LABELS[s.gameType || 'unspecified'];
  return '<div class="summary-card"><h2>🏁 Match Summary<span class="type-tag">' + typeLabel + '</span></h2>' +
    '<div class="summary-sub" style="margin-bottom:16px;">' + heroDisplay(s.heroName) + ' — ' + s.duration + '</div>' +
    renderComparisonBlock(s) + '</div>';
}

function renderRoshan(d) {
  let statusClass = 'alive', statusText = 'Alive', sub = '';
  if (d.roshan.lastDeathClock != null) {
    const minRespawn = d.roshan.lastDeathClock + 480, maxRespawn = d.roshan.lastDeathClock + 660, t = d.clockTimeRaw;
    function countdown(target) { const rem = Math.max(0, target - t); return Math.floor(rem/60) + ':' + Math.floor(rem%60).toString().padStart(2,'0'); }
    if (t < minRespawn) { statusClass='dead'; statusText='Dead'; sub = 'Respawn window opens in ' + countdown(minRespawn); }
    else if (t < maxRespawn) { statusClass='maybe'; statusText='Maybe Alive'; sub = 'Guaranteed alive in ' + countdown(maxRespawn); }
    else { statusClass='alive'; statusText='Alive'; sub = 'Respawn window has passed'; }
    sub += ' · Death #' + d.roshan.deaths + ' — drops: ' + d.roshan.dropsText;
  } else { sub = 'No deaths recorded yet this game'; }
  return '<div class="rosh-card"><div class="rosh-status ' + statusClass + '">🐉 Roshan — ' + statusText + '</div>' +
    '<div class="rosh-sub">' + sub + '</div><button class="rosh-btn" onclick="markRoshanDeath()">Mark Roshan Death</button></div>';
}
function renderGameTypeRow(cur) {
  return '<div class="gametype-row">' + GAME_TYPES.map(t =>
    '<div class="gametype-btn ' + (cur === t ? 'active' : '') + '" onclick="setGameType(\\'' + t + '\\')">' + GAME_TYPE_LABELS[t] + '</div>'
  ).join('') + '</div>';
}
function renderItemGrid(list) {
  return list.length
    ? '<div class="item-grid">' + list.map(i =>
        '<div class="item-card" title="' + i.item.replace(/_/g,' ') + '">' +
        '<img src="' + ITEM_CDN + i.item + '.png" onerror="this.style.display=\\'none\\'">' +
        '<div class="item-clock">' + i.clock + '</div></div>'
      ).join('') + '</div>'
    : '<div class="empty-note">No key items yet</div>';
}
function renderDeathsList(list) {
  return list.length
    ? list.map(x => '<div class="list-item"><span>' + x.clock + '</span><span class="death-gold">-' + (x.goldLost != null ? x.goldLost + 'g' : '?') + '</span></div>').join('')
    : '<div class="empty-note">No deaths yet</div>';
}

async function refreshLive() {
  try {
    const statusRes = await fetch('/status');
    const status = await statusRes.json();
    setSwitchState(status.trackingEnabled);

    const res = await fetch('/stats');
    if (!res.ok) throw new Error('no match');
    const d = await res.json();

    let html = '';
    if (d.ended && d.summary) html += renderSummary(d.summary);

    const clean = heroCleanName(d.heroName);
    html += '<div class="hero-row">';
    html += '<img class="hero-portrait" src="' + HERO_CDN + clean + '.png" onerror="this.style.display=\\'none\\'">';
    html += '<div class="hero-info"><div class="hero-name">' + heroDisplay(d.heroName) + '</div><div class="clock">' + d.lastClock + '</div></div></div>';

    html += renderGameTypeRow(d.gameType);

    html += '<div class="stat-row">';
    html += '<div class="stat-box"><div class="label">Last Hits / Denies</div><div class="value">' + d.lastHits + ' / ' + d.denies + '</div></div>';
    html += '<div class="stat-box"><div class="label">Deaths / Gold Lost</div><div class="value red">' + d.deaths.length + ' / ' + d.totalGoldLost + 'g</div></div>';
    html += '</div>';

    html += '<div class="section">' + renderRoshan(d) + '</div>';

    html += '<div class="section"><div class="section-title">Last Hit Checkpoints</div><div class="checkpoints">';
    for (const min of [5,10,15,20,25]) {
      const c = d.checkpoints[min];
      html += '<div class="checkpoint ' + (c ? '' : 'pending') + '"><div class="min">' + min + 'm</div><div class="val">' + (c ? c.lastHits : '—') + '</div></div>';
    }
    html += '</div></div>';

    html += '<div class="section"><div class="section-title">Key Items</div>' + renderItemGrid(d.keyItemLog) + '</div>';
    html += '<div class="section"><div class="section-title">Deaths</div>' + renderDeathsList(d.deaths) + '</div>';

    document.getElementById('live-content').innerHTML = html;
  } catch (e) {
    document.getElementById('live-content').innerHTML = '<div class="empty-note">Waiting for a match to start...</div>';
  }
}

let historyCache = [];
async function refreshHistory() {
  try {
    const res = await fetch('/history');
    historyCache = await res.json();
    if (!historyCache.length) {
      document.getElementById('history-content').innerHTML = '<div class="empty-note">No finished matches yet.</div>';
      return;
    }
    document.getElementById('history-content').innerHTML = historyCache.slice().reverse().map((m, idx) => {
      const realIdx = historyCache.length - 1 - idx;
      const d = new Date(m.date);
      const typeLabel = GAME_TYPE_LABELS[m.gameType || 'unspecified'];
      return '<div class="history-item" onclick="toggleHistoryDetail(' + realIdx + ')">' +
        '<div class="top"><span class="hname">' + heroDisplay(m.heroName) + '<span class="type-tag">' + typeLabel + '</span></span>' +
        '<span class="date">' + d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], {hour:'2-digit',minute:'2-digit'}) + '</span></div>' +
        '<div class="metrics">' + m.duration + ' · ' + m.totalDeaths + ' deaths · ' + m.totalGoldLost + 'g lost · Rosh x' + (m.roshanDeaths||0) + ' · click for details</div>' +
        '<div class="history-detail hidden" id="detail-' + realIdx + '"></div></div>';
    }).join('');
  } catch (e) {}
}
function toggleHistoryDetail(idx) {
  const el = document.getElementById('detail-' + idx);
  if (!el) return;
  if (!el.classList.contains('hidden')) { el.classList.add('hidden'); return; }
  const m = historyCache[idx];
  let html = renderComparisonBlock(m);
  html += '<div class="section-title" style="margin-top:14px;">Key Items</div>' + renderItemGrid(m.keyItems || []);
  html += '<div class="section-title" style="margin-top:14px;">Deaths</div>' + renderDeathsList(m.deaths || []);
  el.innerHTML = html;
  el.classList.remove('hidden');
}

// --- Leaderboard: personal top games, ranked by a chosen stat ---
const LB_METRICS = {
  last_hits_25: { label: 'Most LH @25m', get: m => m.checkpoints?.[25]?.lastHits ?? null, higherBetter: true, fmt: v => v + ' LH' },
  fewest_deaths: { label: 'Fewest Deaths', get: m => m.totalDeaths, higherBetter: false, fmt: v => v + ' deaths' },
  least_gold_lost: { label: 'Least Gold Lost', get: m => m.totalGoldLost, higherBetter: false, fmt: v => v + 'g lost' },
  most_kills: { label: 'Most Kills', get: m => m.kills, higherBetter: true, fmt: v => v + ' kills' },
};
let lbType = 'all';
let lbMetric = 'last_hits_25';

function renderLbFilterRows() {
  document.getElementById('lb-type-row').innerHTML = ['all', ...GAME_TYPES].map(t =>
    '<div class="gametype-btn ' + (lbType === t ? 'active' : '') + '" onclick="setLbType(\\'' + t + '\\')">' +
    (t === 'all' ? 'All Types' : GAME_TYPE_LABELS[t]) + '</div>'
  ).join('');
  document.getElementById('lb-metric-row').innerHTML = Object.keys(LB_METRICS).map(k =>
    '<div class="gametype-btn ' + (lbMetric === k ? 'active' : '') + '" onclick="setLbMetric(\\'' + k + '\\')">' + LB_METRICS[k].label + '</div>'
  ).join('');
}
function setLbType(t) { lbType = t; renderLeaderboard(); }
function setLbMetric(m) { lbMetric = m; renderLeaderboard(); }

async function refreshLeaderboard() {
  renderLbFilterRows();
  if (!historyCache.length) {
    const res = await fetch('/history');
    historyCache = await res.json();
  }
  renderLeaderboard();
}
function renderLeaderboard() {
  renderLbFilterRows();
  const metric = LB_METRICS[lbMetric];
  let pool = lbType === 'all' ? historyCache : historyCache.filter(m => (m.gameType || 'unspecified') === lbType);
  const ranked = pool
    .map(m => ({ m, value: metric.get(m) }))
    .filter(r => r.value != null)
    .sort((a, b) => metric.higherBetter ? b.value - a.value : a.value - b.value)
    .slice(0, 10);

  const el = document.getElementById('leaderboard-content');
  if (!ranked.length) { el.innerHTML = '<div class="empty-note">No games with this stat yet.</div>'; return; }

  const medals = ['🥇', '🥈', '🥉'];
  const medalColors = ['#f0c060', '#c8c8d0', '#c98a2e'];
  el.innerHTML = ranked.map((r, i) => {
    const d = new Date(r.m.date);
    const typeLabel = GAME_TYPE_LABELS[r.m.gameType || 'unspecified'];
    if (i < 3) {
      return '<div class="history-item" style="border-color:' + medalColors[i] + '55;">' +
        '<div class="top"><span class="hname">' + medals[i] + ' ' + heroDisplay(r.m.heroName) + '<span class="type-tag">' + typeLabel + '</span></span>' +
        '<span class="date">' + d.toLocaleDateString() + '</span></div>' +
        '<div class="metrics" style="color:' + medalColors[i] + '; font-weight:700; font-size:15px;">' + metric.fmt(r.value) + '</div></div>';
    }
    return '<div class="list-item"><span>#' + (i+1) + ' ' + heroDisplay(r.m.heroName) + '</span><span>' + metric.fmt(r.value) + '</span></div>';
  }).join('');
}

function rankPillStyle(color, active) {
  return active ? ('background:' + color + ';border-color:' + color + ';') : ('color:' + color + ';border-color:' + color + '55;');
}
function renderProfileForm() {
  document.getElementById('profile-username').value = profileState.username || '';
  document.getElementById('profile-ranks').innerHTML = RANKS.map(r =>
    '<div class="pill-choice ' + (profileState.rank === r.id ? 'active' : '') + '" style="' + rankPillStyle(r.color, profileState.rank === r.id) + '" onclick="selectRank(\\'' + r.id + '\\')">' + r.label + '</div>'
  ).join('');
  document.getElementById('profile-roles').innerHTML = ROLES.map(r =>
    '<div class="pill-choice ' + (profileState.role === r.id ? 'active' : '') + '" style="' + rankPillStyle(r.color, profileState.role === r.id) + '" onclick="selectRole(\\'' + r.id + '\\')">' + r.label + '</div>'
  ).join('');
}
function selectRank(id) { profileState.rank = id; renderProfileForm(); }
function selectRole(id) { profileState.role = id; renderProfileForm(); }
async function saveProfile() {
  profileState.username = document.getElementById('profile-username').value;
  await fetch('/profile', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(profileState) });
  document.getElementById('profile-saved').textContent = 'Saved!';
  setTimeout(() => document.getElementById('profile-saved').textContent = '', 1500);
  renderProfileChip();
}
function renderProfileChip() {
  const chip = document.getElementById('profile-chip');
  if (!profileState.username && !profileState.rank && !profileState.role) { chip.innerHTML = ''; return; }
  const rank = RANKS.find(r => r.id === profileState.rank);
  const role = ROLES.find(r => r.id === profileState.role);
  let html = '';
  if (profileState.username) html += '<span>' + profileState.username + '</span>';
  if (rank) html += '<span class="rank-pill" style="background:' + rank.color + '22; color:' + rank.color + ';">' + rank.label + '</span>';
  if (role) html += '<span class="role-pill" style="background:' + role.color + '22; color:' + role.color + ';">' + role.label + '</span>';
  chip.innerHTML = html;
}

async function loadProfile() {
  const res = await fetch('/profile');
  profileState = await res.json();
  renderProfileForm();
  renderProfileChip();
}

setInterval(refreshLive, 2000);
loadProfile();
refreshLive();
</script>
</body>
</html>`;

const server = http.createServer((req, res) => {
  if (req.method === 'POST' && req.url === '/') {
    let body = ''; req.on('data', c => body += c);
    req.on('end', () => { try { handleUpdate(JSON.parse(body)); } catch (e) {} res.writeHead(200); res.end('ok'); });
    return;
  }
  if (req.method === 'POST' && req.url === '/toggle') {
    trackingEnabled = !trackingEnabled;
    log(`Tracking manually switched ${trackingEnabled ? 'ON' : 'OFF'}`);
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ trackingEnabled }));
    return;
  }
  if (req.method === 'POST' && req.url === '/roshan-death') {
    if (current && !current.ended) markRoshanDeath(current, current.lastClockTime, 'manual');
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ ok: true }));
    return;
  }
  if (req.method === 'POST' && req.url === '/set-game-type') {
    let body = ''; req.on('data', c => body += c);
    req.on('end', () => {
      try {
        const { type } = JSON.parse(body);
        if (current && !current.ended && GAME_TYPES.includes(type)) { current.gameType = type; log(`Game type set to ${GAME_TYPE_LABELS[type]}`); }
      } catch (e) {}
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ ok: true }));
    });
    return;
  }
  if (req.method === 'GET' && req.url === '/profile') {
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(loadProfile()));
    return;
  }
  if (req.method === 'POST' && req.url === '/profile') {
    let body = ''; req.on('data', c => body += c);
    req.on('end', () => {
      try {
        const p = JSON.parse(body);
        saveProfile({ username: (p.username||'').slice(0,40), rank: p.rank||null, role: p.role||null });
      } catch (e) {}
      res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ ok: true }));
    });
    return;
  }
  if (req.method === 'GET' && req.url === '/status') {
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify({ trackingEnabled }));
    return;
  }
  if (req.method === 'GET' && req.url === '/stats') {
    if (!current) { res.writeHead(404); res.end('no match'); return; }
    const totalGoldLost = current.deaths.reduce((s, d) => s + (d.goldLost || 0), 0);
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({
      matchid: current.matchid, heroName: current.heroName, lastClock: fmtClock(current.lastClockTime),
      clockTimeRaw: current.lastClockTime, lastHits: current.lastHits, denies: current.denies,
      deaths: current.deaths, totalGoldLost, keyItemLog: current.keyItemLog, checkpoints: current.checkpoints,
      gameType: current.gameType,
      roshan: { deaths: current.roshan.deaths, lastDeathClock: current.roshan.lastDeathClock, dropsText: roshanDrops(current.roshan.deaths) },
      ended: current.ended, summary: current.summary,
    }));
    return;
  }
  if (req.method === 'GET' && req.url === '/history') {
    res.writeHead(200, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(loadHistory()));
    return;
  }
  if (req.method === 'GET' && req.url === '/') {
    res.writeHead(200, { 'Content-Type': 'text/html' }); res.end(DASHBOARD_HTML);
    return;
  }
  res.writeHead(404); res.end('not found');
});

server.listen(PORT, () => {
  console.log(`Dota 2 Match Tracker running.`);
  console.log(`Dashboard: http://localhost:${PORT}`);
  console.log(`Waiting for Dota 2 to send match data...`);
});

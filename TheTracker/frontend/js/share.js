// Share cards: a 1200×630 image of a page's headline numbers, drawn on a
// canvas with no outside library and saved to Documents\TheTracker.

const CARD_COLORS = { dota: "#d9a441", deadlock: "#58c2ae", cs2: "#6aa5ee", overwatch: "#f68a3c", today: "#9aa7ff" };
const CARD_GAMES = { dota: "Dota 2", deadlock: "Deadlock", cs2: "Counter-Strike 2", overwatch: "Overwatch", today: "This week" };
const CARD_FONT = 'Bahnschrift, "Segoe UI", sans-serif';

/// Draws a card and returns its PNG as a data URL.
/// card: {game, title, subtitle, stats: [{label, value, tone}], form: [true|false|null] newest first, rows: [[left, right]]}
function drawCard(card) {
  const W = 1200, H = 630, P = 64;
  const c = document.createElement("canvas");
  c.width = W;
  c.height = H;
  const g = c.getContext("2d");
  const accent = CARD_COLORS[card.game] || "#d9a441";
  const tones = { win: "#7ec46a", loss: "#e0665c" };

  // Background: deep, with the game's colour glowing from one corner.
  g.fillStyle = "#0c0e13";
  g.fillRect(0, 0, W, H);
  const glow = g.createRadialGradient(W, 0, 0, W, 0, 900);
  glow.addColorStop(0, accent + "40");
  glow.addColorStop(1, "#0c0e1300");
  g.fillStyle = glow;
  g.fillRect(0, 0, W, H);
  g.fillStyle = accent;
  g.fillRect(0, 0, 10, H);

  // Header.
  g.textBaseline = "alphabetic";
  g.fillStyle = accent;
  g.font = `600 26px ${CARD_FONT}`;
  g.fillText(CARD_GAMES[card.game] || "", P, P + 10);
  g.fillStyle = "#e8ebf0";
  g.font = `700 64px ${CARD_FONT}`;
  fitText(g, card.title || "", P, P + 86, W - 2 * P);
  if (card.subtitle) {
    g.fillStyle = "#8891a0";
    g.font = `400 28px ${CARD_FONT}`;
    fitText(g, card.subtitle, P, P + 132, W - 2 * P);
  }

  // The numbers.
  const stats = (card.stats || []).slice(0, 4);
  const colW = (W - 2 * P) / Math.max(stats.length, 1);
  stats.forEach((s, i) => {
    const x = P + i * colW;
    if (i) {
      g.fillStyle = "#262c39";
      g.fillRect(x - 20, 250, 2, 140);
    }
    g.fillStyle = "#8891a0";
    g.font = `400 26px ${CARD_FONT}`;
    fitText(g, s.label, x, 290, colW - 40);
    g.fillStyle = tones[s.tone] || "#e8ebf0";
    g.font = `700 76px ${CARD_FONT}`;
    fitText(g, String(s.value), x, 372, colW - 40);
  });

  // Rows (a comparison), or the run of results.
  if (card.rows && card.rows.length) {
    // Under the numbers a short list; on its own (a head-to-head) the rows
    // are the card, set large.
    const big = !stats.length;
    const top = big ? 268 : 452, step = big ? 62 : 38, size = big ? 38 : 26;
    card.rows.slice(0, big ? 5 : 3).forEach(([l, r], i) => {
      const y = top + i * step;
      if (big && i) {
        g.fillStyle = "#1d222d";
        g.fillRect(P, y - step + 18, W - 2 * P, 1);
      }
      g.fillStyle = "#b9c0cc";
      g.font = `400 ${size}px ${CARD_FONT}`;
      fitText(g, l, P, y, (W - 2 * P) / 2);
      g.fillStyle = "#e8ebf0";
      g.font = `${big ? 700 : 400} ${size}px ${CARD_FONT}`;
      g.textAlign = "right";
      g.fillText(r, W - P, y);
      g.textAlign = "left";
    });
  } else if (card.form && card.form.length) {
    const cells = card.form.slice(0, 30).reverse();
    cells.forEach((w, i) => {
      const h = w === true ? 46 : w === false ? 26 : 12;
      g.fillStyle = w === true ? tones.win : w === false ? tones.loss : "#5b6373";
      roundRect(g, P + i * 26, 490 - h, 18, h, 4);
    });
    g.fillStyle = "#8891a0";
    g.font = `400 22px ${CARD_FONT}`;
    g.fillText("Recent results, oldest first", P, 528);
  }

  // Footer.
  g.fillStyle = "#5b6373";
  g.font = `600 24px ${CARD_FONT}`;
  g.fillText("TheTracker", P, H - 44);
  g.textAlign = "right";
  g.font = `400 22px ${CARD_FONT}`;
  g.fillText(new Date().toLocaleDateString(undefined, { day: "numeric", month: "long", year: "numeric" }), W - P, H - 44);
  g.textAlign = "left";
  return c.toDataURL("image/png");
}

function fitText(g, text, x, y, maxW) {
  let t = text;
  while (t.length > 1 && g.measureText(t).width > maxW) t = t.slice(0, -2) + "…";
  g.fillText(t, x, y);
}

function roundRect(g, x, y, w, h, r) {
  g.beginPath();
  g.moveTo(x + r, y);
  g.arcTo(x + w, y, x + w, y + h, r);
  g.arcTo(x + w, y + h, x, y + h, r);
  g.arcTo(x, y + h, x, y, r);
  g.arcTo(x, y, x + w, y, r);
  g.fill();
}

/// What each Share button puts on its card. Each returns null when the page
/// has nothing loaded to share yet.
const SHARE = {
  dota() {
    const rows = dHist.data && dHist.data.matches;
    if (!rows || !rows.length) return null;
    const s = dHist.data.summary, link = S.boot.dotaLink || {};
    return { game: "dota", title: link.personaname || "My Dota 2", subtitle: `Last ${s.matches} matches · ${s.wins} won, ${s.losses} lost`,
      stats: [{ label: "Win rate", value: pct(s.winRate), tone: toneOfRate(s.winRate) }, { label: "KDA", value: s.kda.toFixed(2) }, { label: "Gold per minute", value: fmtNum(s.avgGpm) }, { label: "Last hits", value: fmtNum(s.avgLastHits) }],
      form: rows.filter((m) => !m.abandoned).map((m) => m.won) };
  },
  deadlock() {
    const d = dlOverview.data;
    if (!d || !d.matches.length) return null;
    const s = d.summary, link = S.boot.deadlockLink || {};
    return { game: "deadlock", title: link.personaname || "My Deadlock", subtitle: `${d.rank ? d.rank.label + " · " : ""}Last ${s.matches} matches`,
      stats: [{ label: "Win rate", value: pct(s.winRate), tone: toneOfRate(s.winRate) }, { label: "KDA", value: s.kda.toFixed(2) }, { label: "Souls per match", value: fmtNum(s.avgSouls) }, { label: "Best hero", value: s.bestHero || "–" }],
      form: d.matches.map((m) => (m.outcome === "win" ? true : m.outcome === "loss" ? false : null)) };
  },
  overwatch() {
    const d = owOverview.data;
    if (!d || !d.general.gamesPlayed) return null;
    const g = d.general, top = d.heroes[0];
    return { game: "overwatch", title: d.name, subtitle: `${{ all: "All modes", competitive: "Competitive", quickplay: "Quick Play" }[OW.mode]} · ${fmtNum(g.gamesPlayed)} games`,
      stats: [{ label: "Win rate", value: pct(g.winRate), tone: toneOfRate(g.winRate) }, { label: "KDA", value: g.kda.toFixed(2) }, { label: "Damage / 10 min", value: fmtNum(Math.round(g.avgDamage)) }, { label: "Most played", value: top ? top.name : "–" }],
      rows: d.ranks.map((r) => [r.role, `${owCap(r.division)} ${r.tier}`]) };
  },
  cs(el) {
    const m = CS.history.find((x) => x.id === el.dataset.id);
    if (!m) return null;
    return { game: "cs2", title: `${csMapName(m.map)} ${m.myScore} : ${m.theirScore}`, subtitle: `${csResText(m)} · ${fmtDate(m.date)}`,
      stats: [{ label: "K / D / A", value: `${m.kills}/${m.deaths}/${m.assists}` }, { label: "Damage per round", value: dash(csAdr(m), (v) => v.toFixed(0)) }, { label: "Headshots", value: dash(csHs(m), (v) => pct(v)) }, { label: "MVPs", value: m.mvps }],
      form: (m.rounds || []).map((r) => r.won) };
  },
  today() {
    const games = enabledGames(), totals = games.map((g) => [g, todayTotals(g)]).filter(([, t]) => t);
    if (!totals.length) return null;
    const week = totals.reduce((a, [, t]) => ({ games: a.games + t.week.games, won: a.won + t.week.won, lost: a.lost + t.week.lost }), { games: 0, won: 0, lost: 0 });
    const rate = week.won + week.lost ? (week.won * 100) / (week.won + week.lost) : null;
    return { game: "today", title: "My week", subtitle: `The last 7 days across ${totals.length} game${totals.length === 1 ? "" : "s"}`,
      stats: [{ label: "Games", value: week.games }, { label: "Won", value: week.won, tone: "win" }, { label: "Lost", value: week.lost, tone: "loss" }, { label: "Win rate", value: pct(rate), tone: toneOfRate(rate) }],
      rows: totals.map(([g, t]) => [GAME_NAMES[g], `${t.week.won}W ${t.week.lost}L`]) };
  },
  compare(el) {
    const game = el.dataset.game, c = CMP[game].data;
    if (!c) return null;
    return { game, title: `${c.me.name || "Me"} vs ${c.them.name}`, subtitle: `Head to head · ${c.me.name || "me"} first`, stats: [],
      rows: [["Win rate", `${pct(c.me.winRate)}  vs  ${pct(c.them.winRate)}`], ["KDA", `${c.me.kda.toFixed(2)}  vs  ${c.them.kda.toFixed(2)}`],
        ...c.statLabels.slice(0, 3).map((l, i) => [l, `${fmtStat(c.me.stats[i])}  vs  ${fmtStat(c.them.stats[i])}`])] };
  },
};

act("share", async (el) => {
  const card = SHARE[el.dataset.kind] && SHARE[el.dataset.kind](el);
  if (!card) return toast("Nothing to share yet: let the page finish loading.", "err");
  const saved = await attempt(() => invoke("save_image", { name: card.title, dataUrl: drawCard(card) }));
  if (!saved) return;
  toast("Card saved to Documents\\TheTracker.");
  attempt(() => invoke("reveal", { path: saved.path }));
});

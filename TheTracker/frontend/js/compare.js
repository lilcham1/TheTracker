// Compare: a friend's public numbers next to yours, for Dota 2, Deadlock and
// Overwatch. Search by name, or paste a Steam profile link or account id.

const CMP = { dota: {}, deadlock: {}, overwatch: {}, friends: null };
const CMP_VIEW = { dota: "compare", deadlock: "dl-compare", overwatch: "ow-compare" };
const cmpLinked = (game) => (game === "dota" ? dotaLinked() : game === "deadlock" ? dlLinked() : owLinked());

async function cmpFriends() {
  try {
    CMP.friends = await invoke("friends");
  } catch (_) {
    CMP.friends = [];
  }
  rerender();
}

async function cmpRun(game, friend, force = false) {
  const st = CMP[game];
  st.friend = friend;
  st.error = null;
  st.loading = true;
  st.data = null;
  rerender();
  try {
    st.data = await invoke("compare", { friend, mode: game === "overwatch" ? OW.mode : "", force });
    cmpFriends();
  } catch (e) {
    st.error = e.message;
  }
  st.loading = false;
  rerender();
}

act("cmp-search", async (el) => {
  const game = el.dataset.game, st = CMP[game];
  const input = document.getElementById("cmpQuery");
  st.query = input ? input.value : "";
  st.searching = true;
  st.error = null;
  st.results = [];
  rerender();
  try {
    st.results = await invoke("friend_search", { game, query: st.query });
    if (!st.results.length) st.error = game === "overwatch" ? "No public profile matches that. Try the full BattleTag, like Name#1234." : "Nobody matches that. Try their exact Steam name, or paste their Steam profile link.";
    // An exact id or link finds one person: compare straight away.
    if (st.results.length === 1 && /^\d+$|steamcommunity\.com\/profiles\//.test(st.query.trim())) {
      st.searching = false;
      const only = st.results[0];
      st.results = [];
      return cmpRun(game, only);
    }
  } catch (e) {
    st.error = e.message;
  }
  st.searching = false;
  rerender();
});
act("cmp-pick", (el) => {
  const game = el.dataset.game;
  CMP[game].results = [];
  cmpRun(game, JSON.parse(el.dataset.friend));
});
act("cmp-forget", async (el) => {
  const f = JSON.parse(el.dataset.friend);
  CMP.friends = await attempt(() => invoke("friend_forget", f));
  rerender();
});
act("cmp-clear", (el) => {
  const st = CMP[el.dataset.game];
  st.data = st.friend = st.error = null;
  rerender();
});

const fmtStat = (v) => (v >= 1000 ? fmtNum(Math.round(v)) : v >= 100 ? Math.round(v) : v.toFixed(1));

/// One row of the head-to-head: your value, a split bar, theirs. The better
/// side is lit; for deaths, lower is better.
function cmpRow(label, mine, theirs, { fmt = fmtStat, lowerBetter = false } = {}) {
  const total = Math.abs(mine) + Math.abs(theirs) || 1;
  const left = (Math.abs(mine) / total) * 100;
  const better = mine === theirs ? 0 : (mine > theirs) !== lowerBetter ? -1 : 1;
  return `<div class="vs-row">
    <span class="vs-val ${better < 0 ? "lead" : ""}">${fmt(mine)}</span>
    <span class="vs-mid"><span class="vs-label">${label}</span><span class="vs-bar"><i style="width:${left.toFixed(1)}%"></i></span></span>
    <span class="vs-val them ${better > 0 ? "lead" : ""}">${fmt(theirs)}</span></div>`;
}

function cmpHeroImg(game, h) {
  return game === "dota" ? heroImgHtml(h.slug, "avatar small") : imgHtml(h.image, "avatar small");
}

function cmpResultHtml(game, c) {
  const me = c.me, them = c.them;
  const side = (s, who) => `<div class="vs-who ${who}">${imgHtml(s.avatar, "avatar large")}<div><h2 class="display">${esc(s.name || (who === "me" ? "You" : "Them"))}</h2>
    <p class="muted">${s.rank ? esc(s.rank) + " · " : ""}${fmtNum(s.games)} ${game === "overwatch" ? "games" : "recent matches"}</p>${formStrip(s.form, 20)}</div></div>`;
  const common = me.heroes.filter((h) => them.heroes.some((t) => t.name === h.name)).map((h) => ({ me: h, them: them.heroes.find((t) => t.name === h.name) }))
    .sort((a, b) => b.me.games + b.them.games - (a.me.games + a.them.games)).slice(0, 8);
  const top = (s) => `<div class="lines">${s.heroes.slice(0, 5).map((h) => `<div class="line static">${cmpHeroImg(game, h)}<span class="grow"><b>${esc(h.name)}</b><span class="muted"> ${h.games} games</span></span><span class="num ${toneOfRate(h.winRate)}">${pct(h.winRate)}</span></div>`).join("") || `<p class="muted">No heroes.</p>`}</div>`;

  return `<div class="vs-head">${side(me, "me")}<span class="vs-v display">vs</span>${side(them, "them")}</div>
    <div class="vs">
      ${cmpRow("Win rate", me.winRate, them.winRate, { fmt: (v) => pct(v) })}
      ${cmpRow("KDA", me.kda, them.kda, { fmt: (v) => v.toFixed(2) })}
      ${c.statLabels.map((l, i) => cmpRow(l, me.stats[i], them.stats[i], { lowerBetter: /death/i.test(l) })).join("")}
    </div>
    ${common.length ? `<div class="sec-head"><h3>Heroes you both play</h3></div>
      <div class="table-wrap"><table class="table"><thead><tr><th>Hero</th><th class="num">You</th><th class="num">Games</th><th class="num">Them</th><th class="num">Games</th></tr></thead>
      <tbody>${common.map((r) => `<tr><td><span class="cell">${cmpHeroImg(game, r.me)}<b>${esc(r.me.name)}</b></span></td>
        <td class="num ${toneOfRate(r.me.winRate)}">${pct(r.me.winRate)}</td><td class="num muted">${r.me.games}</td>
        <td class="num ${toneOfRate(r.them.winRate)}">${pct(r.them.winRate)}</td><td class="num muted">${r.them.games}</td></tr>`).join("")}</tbody></table></div>` : ""}
    <div class="cols even">
      <section><div class="sec-head"><h3>Your most played</h3></div>${top(me)}</section>
      <section><div class="sec-head"><h3>Their most played</h3></div>${top(them)}</section>
    </div>
    ${aboutData(game === "overwatch"
      ? `Both careers as Blizzard publishes them, for ${{ all: "all modes", competitive: "Competitive", quickplay: "Quick Play" }[OW.mode]}. Change the mode on the Overview page.`
      : `Your last 100 matches against theirs, from ${game === "dota" ? "OpenDota" : "the community Deadlock API"}. Only public match data can be compared.`)}`;
}

function compareHtml(game) {
  if (!cmpLinked(game)) {
    return emptyState("Connect your own account first", "Compare puts a friend's numbers next to yours, so it needs to know which account is yours.",
      `<button class="btn" data-act="go" data-view="${GAMES[game].home}" type="button">Connect</button>`);
  }
  const st = CMP[game];
  if (CMP.friends === null) cmpFriends();
  const recent = (CMP.friends || []).filter((f) => f.game === game);
  const fj = (f) => esc(JSON.stringify(f));
  const placeholder = game === "overwatch" ? "BattleTag, like Name#1234" : "Steam name, profile link or account id";

  const search = `<div class="cmp-bar">
      <div class="row grow"><input class="input grow" id="cmpQuery" type="text" placeholder="${placeholder}" value="${esc(st.query || "")}" data-enter="cmp-search" data-game="${game}" />
        <button class="btn" data-act="cmp-search" data-game="${game}" type="button" ${st.searching ? "disabled" : ""}>${st.searching ? "Searching…" : "Find"}</button></div>
      ${recent.length ? `<div class="chips">${recent.map((f) => `<span class="chip friend ${st.friend && st.friend.id === f.id ? "on" : ""}"><button data-act="cmp-pick" data-game="${game}" data-friend="${fj(f)}" type="button">${imgHtml(f.avatar, "avatar tiny")}${esc(f.name)}</button><button class="x" data-act="cmp-forget" data-friend="${fj(f)}" type="button" title="Remove from recent">×</button></span>`).join("")}</div>` : ""}
    </div>
    ${st.error ? `<div class="note err">${esc(st.error)}</div>` : ""}
    ${st.results && st.results.length ? `<div class="pick-list">${st.results.map((f) => `<button class="pick" data-act="cmp-pick" data-game="${game}" data-friend="${fj(f)}" type="button">
      <span class="row">${imgHtml(f.avatar, "avatar small")}<b>${esc(f.name)}</b></span><span class="muted">Compare</span></button>`).join("")}</div>` : ""}`;

  let body = "";
  if (st.loading) body = loadingState(`Reading ${esc(st.friend.name)}'s matches…`);
  else if (st.data) body = `<div class="row"><span class="grow"></span><button class="btn ghost small" data-act="share" data-kind="compare" data-game="${game}" type="button">Share</button><button class="link" data-act="cmp-clear" data-game="${game}" type="button">Clear</button></div>${cmpResultHtml(game, st.data)}`;
  else if (!st.results || !st.results.length) body = `<p class="muted">Find a friend to see your numbers side by side.</p>`;
  return search + body;
}

for (const [game, id] of Object.entries(CMP_VIEW)) {
  view(id, {
    game, nav: true, icon: "compare", title: "Compare",
    // Dota 2 and Deadlock also have a leaderboard tab here: other players.
    navTitle: game === "overwatch" ? "Compare" : "Players",
    sub: () => (CMP[game].data ? `You and ${CMP[game].friend.name}` : "Your numbers next to a friend's"),
    load(force) {
      cmpFriends();
      if (game === "overwatch" && owLinked()) owOverview.load();
      if (force && CMP[game].friend) cmpRun(game, CMP[game].friend, true);
    },
    render: () => compareHtml(game),
  });
}

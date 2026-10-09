// Startup. One call fetches everything the first frame needs, so the app
// never draws a page before it knows who is linked and what is set.

async function boot() {
  paintIcons();
  try {
    S.boot = await invoke("boot");
  } catch (e) {
    $("#page").innerHTML = `<div class="empty"><h3>TheTracker couldn't start</h3><p>${esc(e.message)}</p>
      <button class="btn" data-act="reload" type="button">Try again</button></div>`;
    return;
  }
  S.overlayVisible = S.boot.overlayVisible;

  // With several games on, the day across all of them is the front page.
  let start = enabledGames().length > 1 ? "today" : GAMES[enabledGames()[0]].home;
  try {
    const saved = localStorage.getItem("tt.view");
    if (saved && VIEWS[saved] && (VIEWS[saved].game || saved === "today")) start = saved;
  } catch (_) {
    /* storage is a convenience */
  }

  await Promise.all([pollLive(), loadHistory()]);
  // A match in progress is what the player opened the app to see.
  if (S.live && S.live.live && S.boot.prefs.games.dota) start = "overview";
  // First run: ask which games before showing any of them.
  if (!S.boot.prefs.games.chosen) start = "welcome";

  renderBanners();
  go(start);

  setInterval(pollLive, 1000);
  setInterval(pollSlow, 5000);
  pollSlow();
  // Not at once: startup should not wait on the network.
  setTimeout(() => checkForUpdate(true), 8000);
  setInterval(() => checkForUpdate(true), 6 * 3600 * 1000);
  if (S.boot.prefs.games.dota) ensureHeroList();
}

act("reload", () => location.reload());

boot();

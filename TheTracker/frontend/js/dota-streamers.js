// Dota 2 streamers: who is streaming each hero right now, in the Live group.
// The page itself is the shared streamer page in deadlock.js.

const dotaBoard = resource("dotaBoard", "dota_streamers", { ttl: 20000 });

/// The hero you're playing, from the live feed, as the board's hero id.
function myDotaHero() {
  const c = S.live && S.live.live && S.live.current;
  if (!c || !c.heroName) return null;
  const h = S.heroBySlug && S.heroBySlug[heroSlug(c.heroName)];
  return h ? { id: h.id, name: h.name } : null;
}

view("dota-streamers", {
  game: "dota", nav: true, tabOf: "live", icon: "live", title: "Streamers",
  sub: () => "Streamers playing each hero right now",
  load(force) {
    ensureHeroList();
    return dotaBoard.load(force);
  },
  render() {
    return streamerPageHtml({
      game: "dota", gameName: "Dota 2", board: dotaBoard, otherMode: "Unranked", listName: "ranked and unranked games in Dota's Watch list",
      mine: myDotaHero(),
      about: `Only ranked and unranked games in standard modes are shown, ranked first; Turbo, Ability Draft, custom and league games are left out. Games come from OpenDota's copy of Dota's Watch list, which has only the top hundred or so live games, so most players' games, and most streamers', are not in it. Streams come from Twitch. A stream appears under a hero when one Steam account it can be tied to is playing that hero in one of those games: through a link you made, the player's name in the game, OpenDota's list of pro players, or an exact Steam name search. Streamers on Valve's Dota leaderboard whose game isn't listed appear under Top-ranked streamers live.`,
    });
  },
});

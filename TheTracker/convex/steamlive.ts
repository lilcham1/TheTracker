import { action } from "./_generated/server";
import { v } from "convex/values";

/**
 * Which hero a Dota player is on right now, from Steam's official Web API,
 * for the app's Dota Streamers page. Steam needs an API key, which can never
 * ship inside a desktop app, so the call is made here with the key the
 * deployment's owner sets once:
 *
 *   npx convex env set --prod STEAM_WEB_API_KEY <key>
 *
 * Without it the action answers { ok: false, reason: "not_set_up" }.
 *
 * Two official calls: GetPlayerSummaries says who is in Dota (app 570) and on
 * which game server (public profiles only); GetRealtimeStats on that server
 * gives every player's hero in the live match.
 */

const STEAM64_BASE = 76561197960265728n;
const MAX_ACCOUNTS = 200;
const MAX_SERVERS = 60;

type InGame = { accountId: string; heroId: number; matchId: string; gameMode: number; lobbyType: number; gameTime: number };

const str = (x: unknown) => (x === undefined || x === null ? "" : String(x));
const num = (x: unknown) => (typeof x === "number" ? x : Number(x) || 0);

export const dotaLive = action({
  args: { accountIds: v.array(v.string()) },
  handler: async (_ctx, { accountIds }): Promise<{ ok: true; players: InGame[] } | { ok: false; reason: string }> => {
    const key = process.env.STEAM_WEB_API_KEY;
    if (!key) return { ok: false, reason: "not_set_up" };

    const ids = [...new Set(accountIds.filter((a) => /^\d{1,10}$/.test(a)))].slice(0, MAX_ACCOUNTS);
    const to64 = (a: string) => (BigInt(a) + STEAM64_BASE).toString();
    const to32 = (s: string) => (BigInt(s) - STEAM64_BASE).toString();

    // Who is in Dota, and on which server.
    const server: Record<string, string> = {}; // account -> server steam id
    for (let i = 0; i < ids.length; i += 100) {
      const q = new URLSearchParams({ key, steamids: ids.slice(i, i + 100).map(to64).join(",") });
      const r = await fetch(`https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?${q}`);
      if (r.status === 401 || r.status === 403) return { ok: false, reason: "bad_credentials" };
      if (!r.ok) return { ok: false, reason: "unreachable" };
      const j = (await r.json()) as { response?: { players?: Record<string, unknown>[] } };
      for (const p of j.response?.players ?? []) {
        if (str(p.gameid) === "570" && str(p.gameserversteamid) && str(p.gameserversteamid) !== "0") {
          server[to32(str(p.steamid))] = str(p.gameserversteamid);
        }
      }
    }

    // Each live match once: every player's hero.
    const wanted = new Set(Object.keys(server));
    const servers = [...new Set(Object.values(server))].slice(0, MAX_SERVERS);
    const players: InGame[] = [];
    await Promise.all(
      servers.map(async (sid) => {
        const q = new URLSearchParams({ key, server_steam_id: sid });
        const r = await fetch(`https://api.steampowered.com/IDOTA2MatchStats_570/GetRealtimeStats/v1/?${q}`);
        if (!r.ok) return;
        let j: Record<string, any>;
        try {
          j = (await r.json()) as Record<string, any>;
        } catch {
          return;
        }
        const match = (j.match ?? {}) as Record<string, unknown>;
        for (const team of (j.teams ?? []) as Record<string, any>[]) {
          for (const p of (team.players ?? []) as Record<string, unknown>[]) {
            const account = str(p.accountid);
            if (!wanted.has(account) || server[account] !== sid) continue;
            players.push({
              accountId: account, heroId: num(p.heroid), matchId: str(match.matchid),
              gameMode: num(match.game_mode), lobbyType: match.lobby_type === undefined ? -1 : num(match.lobby_type), gameTime: num(match.game_time),
            });
          }
        }
      }),
    );
    return { ok: true, players };
  },
});

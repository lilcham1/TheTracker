import { action } from "./_generated/server";
import { v } from "convex/values";

/**
 * Lifetime Counter-Strike 2 statistics for one Steam account, from Steam's
 * own Web API: the totals Steam keeps for every player (kills, deaths,
 * accuracy, per-weapon and per-map figures). The same public source other
 * stat sites read.
 *
 * Steam requires an API key for this endpoint. It lives here, in the
 * deployment's environment (STEAM_WEB_API_KEY), and never in the app, so it
 * is not shipped to anyone's PC. Without it this simply reports that the
 * feature is not set up.
 *
 * Returns a value rather than throwing: a production deployment hides thrown
 * messages, and the app needs to tell "not set up" from "profile is private".
 */
export const cs2Stats = action({
  args: { steamId: v.string() },
  handler: async (_ctx, { steamId }) => {
    if (!/^\d{17}$/.test(steamId)) return { ok: false, reason: "bad_id" };
    const key = process.env.STEAM_WEB_API_KEY;
    if (!key) return { ok: false, reason: "not_set_up" };

    const url =
      "https://api.steampowered.com/ISteamUserStats/GetUserStatsForGame/v2/" +
      `?appid=730&key=${encodeURIComponent(key)}&steamid=${steamId}`;
    let resp: Response;
    try {
      resp = await fetch(url);
    } catch {
      return { ok: false, reason: "steam_unreachable" };
    }
    // Steam answers 403 when the player's game details are private.
    if (resp.status === 403) return { ok: false, reason: "private" };
    if (resp.status === 401) return { ok: false, reason: "bad_key" };
    if (!resp.ok) return { ok: false, reason: "no_stats" };

    const body = (await resp.json()) as { playerstats?: { stats?: { name: string; value: number }[] } };
    const stats = body.playerstats?.stats ?? [];
    if (!stats.length) return { ok: false, reason: "no_stats" };
    return { ok: true, stats };
  },
});

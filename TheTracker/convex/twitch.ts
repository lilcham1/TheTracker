import { action, internalMutation, internalQuery } from "./_generated/server";
import { internal } from "./_generated/api";
import { v } from "convex/values";

/**
 * Live Deadlock streams from Twitch's official API, for the app's Deadlock
 * Live page. Twitch needs an app's client ID and secret, which can never
 * ship inside a desktop app, so the call is made here with the two values
 * the deployment's owner sets once:
 *
 *   npx convex env set TWITCH_CLIENT_ID <id>
 *   npx convex env set TWITCH_CLIENT_SECRET <secret>
 *
 * Without them the action answers { ok: false, reason: "not_set_up" }.
 */

export const getKv = internalQuery({
  args: { key: v.string() },
  handler: async (ctx, { key }) => {
    const row = await ctx.db.query("kv").withIndex("by_key", (q) => q.eq("key", key)).unique();
    return row ? { value: row.value, expires: row.expires } : null;
  },
});

export const setKv = internalMutation({
  args: { key: v.string(), value: v.string(), expires: v.number() },
  handler: async (ctx, { key, value, expires }) => {
    const row = await ctx.db.query("kv").withIndex("by_key", (q) => q.eq("key", key)).unique();
    if (row) await ctx.db.patch(row._id, { value, expires });
    else await ctx.db.insert("kv", { key, value, expires });
  },
});

type Stream = { login: string; name: string; title: string; viewers: number; startedAt: string; thumbnail: string; language: string };

export const deadlockStreams = action({
  args: {},
  handler: async (ctx): Promise<{ ok: true; streams: Stream[] } | { ok: false; reason: string }> => {
    const id = process.env.TWITCH_CLIENT_ID;
    const secret = process.env.TWITCH_CLIENT_SECRET;
    if (!id || !secret) return { ok: false, reason: "not_set_up" };

    // An app token lasts about two months; keep it rather than asking for a
    // new one on every call.
    const now = Date.now();
    let token = await ctx.runQuery(internal.twitch.getKv, { key: "twitch_token" });
    if (!token || token.expires < now + 60_000) {
      const r = await fetch("https://id.twitch.tv/oauth2/token", {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({ client_id: id, client_secret: secret, grant_type: "client_credentials" }),
      });
      if (!r.ok) return { ok: false, reason: r.status === 400 || r.status === 403 ? "bad_credentials" : "unreachable" };
      const j = (await r.json()) as { access_token: string; expires_in: number };
      token = { value: j.access_token, expires: now + j.expires_in * 1000 };
      await ctx.runMutation(internal.twitch.setKv, { key: "twitch_token", value: token.value, expires: token.expires });
    }
    const headers = { "Client-Id": id, Authorization: `Bearer ${token.value}` };

    let game = await ctx.runQuery(internal.twitch.getKv, { key: "twitch_deadlock_game" });
    if (!game || game.expires < now) {
      const r = await fetch("https://api.twitch.tv/helix/games?name=Deadlock", { headers });
      if (!r.ok) return { ok: false, reason: r.status === 401 ? "bad_credentials" : "unreachable" };
      const j = (await r.json()) as { data: { id: string }[] };
      if (!j.data.length) return { ok: false, reason: "unreachable" };
      game = { value: j.data[0].id, expires: now + 7 * 86_400_000 };
      await ctx.runMutation(internal.twitch.setKv, { key: "twitch_deadlock_game", value: game.value, expires: game.expires });
    }

    // Most watched first, as Twitch returns them; four pages is every
    // stream anyone is likely to look for.
    const streams: Stream[] = [];
    let after = "";
    for (let page = 0; page < 4; page++) {
      const q = new URLSearchParams({ game_id: game.value, first: "100", type: "live" });
      if (after) q.set("after", after);
      const r = await fetch(`https://api.twitch.tv/helix/streams?${q}`, { headers });
      if (!r.ok) break;
      const j = (await r.json()) as {
        data: { user_login: string; user_name: string; title: string; viewer_count: number; started_at: string; thumbnail_url: string; language: string }[];
        pagination: { cursor?: string };
      };
      for (const s of j.data) {
        streams.push({
          login: s.user_login, name: s.user_name, title: s.title, viewers: s.viewer_count, startedAt: s.started_at,
          thumbnail: s.thumbnail_url.replace("{width}", "320").replace("{height}", "180"), language: s.language,
        });
      }
      if (!j.pagination.cursor || !j.data.length) break;
      after = j.pagination.cursor;
    }
    return { ok: true, streams };
  },
});

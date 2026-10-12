"use node";

import Anthropic from "@anthropic-ai/sdk";
import { action } from "./_generated/server";
import { internal } from "./_generated/api";
import { v } from "convex/values";

/**
 * The AI coach: a short write-up of one reviewed match, written by Claude
 * from the facts TheTracker's own review established. The app never holds
 * the key; the deployment's owner sets it once:
 *
 *   npx convex env set --prod ANTHROPIC_API_KEY <key>
 *
 * Without it the action answers { ok: false, reason: "not_set_up" }. One
 * write-up per match is kept for 90 days, so a match is paid for once even
 * across reinstalls.
 */

const MODEL = "claude-opus-5-5";

const SYSTEM = `You are a coach for a solo-queue player of Dota 2 and Deadlock, reading one of their matches between games.
You get the facts an analyzer established from the public match data and from what the player's own client reported: the review points, each death with who killed them, where, the fight it was part of and how the game stood, the role goals, net worth curves, time dead, wards.
Rules:
- Use only those facts. Never invent events, positions, items or numbers that aren't in them. If the facts are thin, say less.
- Talk to the player directly, plainly, like a coach who respects them: specific, no fluff, no jargon they wouldn't know, no praise sandwiches.
- Find the thread: what one or two habits explain most of what went wrong, and what the cheapest fix is. Prefer the pattern across deaths over listing each death.
- Each death note says what to do differently next time in that spot, in at most 25 words. Skip a note when a death was fine (a good trade, a sacrifice that won the fight).
- Focus items: at most three, each a habit (title up to 6 words), why it mattered in this match (up to 30 words), and one concrete drill or rule to practise next game (up to 30 words).
- The verdict is two sentences at most. Praise is one sentence on what to keep doing, grounded in a fact; empty if nothing stood out.
Answer with the JSON only.`;

const SCHEMA = {
  type: "object",
  additionalProperties: false,
  properties: {
    verdict: { type: "string" },
    deaths: {
      type: "array",
      items: { type: "object", additionalProperties: false, properties: { clock: { type: "string" }, note: { type: "string" } }, required: ["clock", "note"] },
    },
    focus: {
      type: "array",
      items: {
        type: "object",
        additionalProperties: false,
        properties: { title: { type: "string" }, why: { type: "string" }, drill: { type: "string" } },
        required: ["title", "why", "drill"],
      },
    },
    praise: { type: "string" },
  },
  required: ["verdict", "deaths", "focus", "praise"],
};

type Note = { verdict: string; deaths: { clock: string; note: string }[]; focus: { title: string; why: string; drill: string }[]; praise: string; model?: string };
type Result = { ok: true; note: Note } | { ok: false; reason: string };

export const review = action({
  args: { game: v.string(), matchId: v.string(), facts: v.any(), force: v.optional(v.boolean()) },
  handler: async (ctx, { game, matchId, facts, force }): Promise<Result> => {
    const apiKey = process.env.ANTHROPIC_API_KEY;
    if (!apiKey) return { ok: false, reason: "not_set_up" };
    if (game !== "dota" && game !== "deadlock") return { ok: false, reason: "unknown_game" };
    const cacheKey = `coach:${game}:${matchId}`;
    if (!force) {
      const cached = await ctx.runQuery(internal.twitch.getKv, { key: cacheKey });
      if (cached && cached.expires > Date.now()) return { ok: true, note: JSON.parse(cached.value) as Note };
    }
    const client = new Anthropic({ apiKey });
    try {
      const response = await client.beta.messages.create({
        model: MODEL,
        max_tokens: 4000,
        betas: ["server-side-fallback-2026-07-01"],
        fallbacks: "default",
        system: [{ type: "text", text: SYSTEM, cache_control: { type: "ephemeral" } }],
        output_config: { effort: "medium", format: { type: "json_schema", schema: SCHEMA } },
        messages: [{ role: "user", content: `Game: ${game === "dota" ? "Dota 2" : "Deadlock"}\nFacts:\n${JSON.stringify(facts)}` }],
      });
      if (response.stop_reason === "refusal") return { ok: false, reason: "refused" };
      const text = response.content
        .filter((b): b is Anthropic.Beta.BetaTextBlock => b.type === "text")
        .map((b) => b.text)
        .join("");
      const note = JSON.parse(text) as Note;
      note.model = response.model;
      await ctx.runMutation(internal.twitch.setKv, { key: cacheKey, value: JSON.stringify(note), expires: Date.now() + 90 * 24 * 3600_000 });
      return { ok: true, note };
    } catch (error) {
      if (error instanceof Anthropic.AuthenticationError) return { ok: false, reason: "bad_key" };
      if (error instanceof Anthropic.RateLimitError) return { ok: false, reason: "rate_limited" };
      if (error instanceof Anthropic.APIError) return { ok: false, reason: `API error ${error.status}` };
      return { ok: false, reason: error instanceof Error ? error.message : String(error) };
    }
  },
});

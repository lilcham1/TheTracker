import { internalMutation } from "./_generated/server";
import { v } from "convex/values";

/**
 * One-off housekeeping, run by the deployment's owner from the command line:
 *
 *   npx convex run --prod maintenance:removeTestRows '{"dryRun": true}'
 *
 * It is an internal function: nothing in the app, and nobody on the
 * internet, can call it.
 *
 * The rows it targets are test matches published on 5 September 2026 while
 * the old simulator could still sync (four "matches" inside 13 minutes).
 * The app has not been able to publish a simulated match since.
 */
export const removeTestRows = internalMutation({
  args: { dryRun: v.boolean() },
  handler: async (ctx, args) => {
    const from = "2026-09-05T16:00:00.000Z";
    const to = "2026-09-05T16:30:00.000Z";
    const rows = (await ctx.db.query("matches").collect()).filter((r) => r.date >= from && r.date <= to);
    if (!args.dryRun) {
      for (const row of rows) await ctx.db.delete(row._id);
    }
    return {
      deleted: args.dryRun ? 0 : rows.length,
      matched: rows.map((r) => ({ date: r.date, username: r.username, heroName: r.heroName, kills: r.kills })),
    };
  },
});

import { convexAuth, createAccount, retrieveAccount } from "@convex-dev/auth/server";
import { ConvexCredentials } from "@convex-dev/auth/providers/ConvexCredentials";
import { Password } from "@convex-dev/auth/providers/Password";

// Sign in with Steam.
//
// The app opens Steam's own login page in the player's browser (OpenID 2.0).
// Steam sends the browser back to a listener on the player's PC with a signed
// statement of which account logged in, and the app forwards that statement
// here unverified. This function is the one that checks it, by asking Steam
// directly, so a client cannot simply claim to be someone: the account id is
// taken from what Steam confirms, never from what the app says.
//
// No Steam API key is involved and no password ever reaches the app or this
// server.

const STEAM_OPENID = "https://steamcommunity.com/openid/login";
const CLAIMED_ID = /^https:\/\/steamcommunity\.com\/openid\/id\/(\d{17})$/;
// The app's callback is always a loopback address. Steam signs return_to, so
// a statement obtained by some website sending a player through Steam's login
// (whose return_to is that website) can never be replayed here.
const RETURN_TO = /^http:\/\/127\.0\.0\.1:\d{1,5}\/steam\/callback(\?|$)/;
// A statement is only good for a few minutes after Steam issued it.
const MAX_AGE_MS = 5 * 60 * 1000;

async function verifiedSteamId(openid: Record<string, string>): Promise<string> {
  const claimed = CLAIMED_ID.exec(openid["openid.claimed_id"] ?? "");
  if (
    !claimed ||
    openid["openid.identity"] !== openid["openid.claimed_id"] ||
    openid["openid.op_endpoint"] !== STEAM_OPENID ||
    openid["openid.ns"] !== "http://specs.openid.net/auth/2.0" ||
    openid["openid.mode"] !== "id_res" ||
    !RETURN_TO.test(openid["openid.return_to"] ?? "")
  ) {
    throw new Error("That isn't a Steam sign-in this app started.");
  }

  // The nonce starts with the time Steam issued it, e.g. 2026-10-03T10:00:00Z…
  const issued = Date.parse((openid["openid.response_nonce"] ?? "").slice(0, 20));
  if (!Number.isFinite(issued) || Math.abs(Date.now() - issued) > MAX_AGE_MS) {
    throw new Error("That Steam sign-in has expired. Try again.");
  }

  // Everything Steam said goes back to Steam, which answers whether it
  // really said it.
  const body = new URLSearchParams();
  for (const [key, value] of Object.entries(openid)) {
    if (key.startsWith("openid.") && typeof value === "string") body.set(key, value);
  }
  body.set("openid.mode", "check_authentication");
  const resp = await fetch(STEAM_OPENID, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: body.toString(),
  });
  const text = await resp.text();
  if (!resp.ok || !/is_valid\s*:\s*true/.test(text)) {
    throw new Error("Steam did not confirm that sign-in.");
  }
  return claimed[1];
}

const Steam = ConvexCredentials({
  id: "steam",
  authorize: async (params, ctx) => {
    const openid = params.openid as unknown as Record<string, string> | undefined;
    if (!openid || typeof openid !== "object") {
      throw new Error("Missing Steam sign-in details.");
    }
    const steamId = await verifiedSteamId(openid);
    const name = typeof params.name === "string" ? params.name.slice(0, 40) : undefined;

    // retrieveAccount throws when the account does not exist yet.
    try {
      const { user } = await retrieveAccount(ctx, { provider: "steam", account: { id: steamId } });
      return { userId: user._id };
    } catch {
      const { user } = await createAccount(ctx, {
        provider: "steam",
        account: { id: steamId },
        profile: name ? { name } : {},
      });
      return { userId: user._id };
    }
  },
});

// Password stays so copies of the app from before 1.0, which sign in with an
// email and password, keep working. New versions only offer Steam.
export const { auth, signIn, signOut, store, isAuthenticated } = convexAuth({
  providers: [Steam, Password],
});

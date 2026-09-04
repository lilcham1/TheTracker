import { convexAuth } from "@convex-dev/auth/server";
import { Password } from "@convex-dev/auth/providers/Password";

// Email + password accounts. Password was chosen over OAuth because this is
// a desktop app: an OAuth provider would need an external browser round trip
// and a deep link back into the Tauri window, whereas this flow stays inside
// the app. The tracker's Rust backend drives it by calling the `auth:signIn`
// action directly (see convex_sync.rs) — there's no JS auth client bundled.
export const { auth, signIn, signOut, store, isAuthenticated } = convexAuth({
  providers: [Password],
});

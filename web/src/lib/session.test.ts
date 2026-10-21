import { describe, expect, it } from "vitest";
import { isExpired } from "./session";

// The payload is base64url JSON with no signature worth checking here: these tests are about the decision to
// reuse a stored token, which is the difference between the app working and every page answering 401 with no
// sign-in form to recover through.
function tokenWith(payload: Record<string, unknown>): string {
  const encode = (value: object) =>
    btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${encode({ alg: "HS256", typ: "JWT" })}.${encode(payload)}.signature`;
}

describe("isExpired", () => {
  it("accepts a token whose exp is still ahead", () => {
    const token = tokenWith({ exp: 2_000_000_000 });

    expect(isExpired(token, 1_000_000_000_000)).toBe(false);
  });

  it("rejects a token whose exp has passed", () => {
    const token = tokenWith({ exp: 1_000_000_000 });

    expect(isExpired(token, 1_000_000_000_000)).toBe(true);
  });

  it("treats a token with no exp as expired, because it is never the one we minted", () => {
    expect(isExpired(tokenWith({ user_id: 12 }), 0)).toBe(true);
  });

  it("treats anything that is not a JWT as expired rather than throwing", () => {
    expect(isExpired("", 0)).toBe(true);
    expect(isExpired("not-a-token", 0)).toBe(true);
    expect(isExpired("a.b.c", 0)).toBe(true);
  });
});

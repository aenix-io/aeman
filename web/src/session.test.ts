import { describe, expect, it, vi } from "vitest";

import { ApiError } from "./providers/api/apiProvider";
import { guardSignedOut, isSignedOut } from "./session";

describe("isSignedOut", () => {
  it("is a 401 from the API — the session is gone", () => {
    expect(isSignedOut(new ApiError("not authenticated: not signed in", 401))).toBe(true);
  });

  it("is not any other refusal or failure: 403 (signed in, not allowed) and 500 stay errors", () => {
    expect(isSignedOut(new ApiError("forbidden", 403))).toBe(false);
    expect(isSignedOut(new ApiError("boom", 500))).toBe(false);
    expect(isSignedOut(new ApiError("not found", 404))).toBe(false);
  });

  it("is not a plain Error, nor nothing at all", () => {
    expect(isSignedOut(new Error("not authenticated: not signed in"))).toBe(false);
    expect(isSignedOut(new TypeError("Failed to fetch"))).toBe(false);
    expect(isSignedOut(undefined)).toBe(false);
    expect(isSignedOut(null)).toBe(false);
  });

  it("only the API's own error counts — a look-alike with a 401 status is not one", () => {
    expect(isSignedOut({ status: 401, message: "not signed in" })).toBe(false);
  });
});

describe("guardSignedOut", () => {
  const signedOut = new ApiError("not authenticated: not signed in", 401);

  function fakeProvider() {
    return {
      fails: vi.fn(async () => {
        throw signedOut;
      }),
      forbidden: vi.fn(async () => {
        throw new ApiError("forbidden", 403);
      }),
      works: vi.fn(async (n: number) => n * 2),
      sync: vi.fn((s: string) => s.toUpperCase()),
      label: "not a function",
    };
  }

  it("tells the handler about a 401 and still rejects, so the caller's own handling runs too", async () => {
    const onSignedOut = vi.fn();
    const p = guardSignedOut(fakeProvider(), onSignedOut);
    await expect(p.fails()).rejects.toBe(signedOut);
    expect(onSignedOut).toHaveBeenCalledTimes(1);
    expect(onSignedOut).toHaveBeenCalledWith(signedOut);
  });

  it("stays quiet on any other rejection", async () => {
    const onSignedOut = vi.fn();
    const p = guardSignedOut(fakeProvider(), onSignedOut);
    await expect(p.forbidden()).rejects.toBeInstanceOf(ApiError);
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it("passes results, arguments and `this` through untouched", async () => {
    const onSignedOut = vi.fn();
    const target = fakeProvider();
    const p = guardSignedOut(target, onSignedOut);
    await expect(p.works(21)).resolves.toBe(42);
    expect(target.works).toHaveBeenCalledWith(21);
    expect(p.sync("ok")).toBe("OK");
    expect(p.label).toBe("not a function");
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it("runs the handler before the caller's catch — the caller's message can be held back", async () => {
    const order: string[] = [];
    const p = guardSignedOut(fakeProvider(), () => order.push("guard"));
    await p.fails().catch(() => order.push("caller"));
    expect(order).toEqual(["guard", "caller"]);
  });
});

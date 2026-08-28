// A session can end under an open tab without the tab knowing: the server
// restarted with memory-only sessions, the session TTL passed, the forge
// refused a token refresh. From then on every /api/v1 request answers 401.
// This module recognises that answer and lets one place act on it, whichever
// of the many calls on the board saw it first — so the person is offered the
// sign-in again instead of reading "not signed in" in a red banner.

import { ApiError } from "./providers/api/apiProvider";

/** isSignedOut is true for the API's own 401 — the session is gone. Any other
 *  status (403: signed in but not allowed, 404, 500) and any other kind of
 *  error is not a sign-out. */
export function isSignedOut(err: unknown): err is ApiError {
  return err instanceof ApiError && err.status === 401;
}

/** guardSignedOut wraps every method of `target` so a rejection that is a
 *  sign-out reaches `onSignedOut` before the caller's own catch does; the call
 *  still rejects exactly as before, so nothing about the callers changes.
 *  Non-function members and synchronous results pass through untouched. */
export function guardSignedOut<T extends object>(
  target: T,
  onSignedOut: (err: ApiError) => void,
): T {
  return new Proxy(target, {
    get(t, prop, receiver) {
      const value: unknown = Reflect.get(t, prop, receiver);
      if (typeof value !== "function") {
        return value;
      }
      const method = value as (this: T, ...args: unknown[]) => unknown;
      return (...args: unknown[]) => {
        const result = method.apply(t, args);
        if (!(result instanceof Promise)) {
          return result;
        }
        return result.then(undefined, (err: unknown) => {
          if (isSignedOut(err)) {
            onSignedOut(err);
          }
          throw err;
        });
      };
    },
  });
}

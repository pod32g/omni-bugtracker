// Omni Access, the gateway in front of the tracker, answers a background
// request with 401 {"error":"authentication_required"} when it needs a
// top-level navigation to sign the user in again or re-check their device —
// something a fetch cannot do (the device check goes through a local agent or
// an app link). That is not the tracker's own session ending: the silent
// refresh would be blocked the same way, and treating it as a logout strands
// the user. A page reload is a navigation, so the gateway runs its check and
// lands the browser back on the same view.
//
// The decision is kept pure here so it can be reasoned about (and tested)
// without a browser; api.ts supplies the browser state.

/** The error code Omni Access puts in the body of such a 401. */
export const GATEWAY_RECHECK_ERROR = "authentication_required";

/** Shown when the tracker does not reload by itself. */
export const GATEWAY_RECHECK_MESSAGE =
  "Omni Access needs to re-check this device before the tracker can continue. " +
  "Nothing was sent — copy any unsaved text, then reload the page.";

/**
 * A reload within this long of the previous one is not attempted again: the
 * gateway should have been satisfied by the first, so a second request for a
 * re-check means reloading is not helping, and looping would lock the user out
 * of the page.
 */
export const RELOAD_LOOP_GUARD_MS = 30_000;

/** True when a response is Omni Access asking for a top-level navigation. */
export function isGatewayRecheck(status: number, body: unknown): boolean {
  return (
    status === 401 &&
    typeof body === "object" &&
    body !== null &&
    (body as { error?: unknown }).error === GATEWAY_RECHECK_ERROR
  );
}

export type RecheckAction = "reload" | "report";

export interface RecheckState {
  /** HTTP method of the request the gateway refused. */
  method: string;
  /**
   * Whether the page may hold text the user has not saved: a field has focus,
   * or they typed since the last successful write. A reload would lose it.
   */
  unsavedInput: boolean;
  /** When this tab last reloaded for a re-check (ms since epoch; 0 = never). */
  lastReloadAt: number;
  now: number;
}

/**
 * Whether to reload the page so the gateway can re-check, or to report the
 * problem and leave the page as it is.
 *
 * - A write (anything but GET/HEAD) never reached the tracker, and whatever
 *   the user typed to make it lives only in the page: report, don't reload.
 * - A page with unsaved input would lose it to a reload triggered by a
 *   background read (the inbox poll fires every 30s): report.
 * - A reload that just happened did not satisfy the gateway: report instead of
 *   looping.
 */
export function recheckAction(s: RecheckState): RecheckAction {
  const method = s.method.toUpperCase();
  if (method !== "GET" && method !== "HEAD") return "report";
  if (s.unsavedInput) return "report";
  if (s.lastReloadAt > 0 && s.now - s.lastReloadAt < RELOAD_LOOP_GUARD_MS) return "report";
  return "reload";
}

const TEXT_INPUT_EXCLUDED = new Set([
  "button",
  "submit",
  "reset",
  "checkbox",
  "radio",
  "range",
  "color",
  "file",
  "image",
  "hidden",
]);

/** Whether a focused element is one the user types into. */
export function isEditableElement(el: { tagName: string; isContentEditable?: boolean; type?: string } | null): boolean {
  if (!el) return false;
  if (el.isContentEditable) return true;
  switch (el.tagName.toUpperCase()) {
    case "TEXTAREA":
    case "SELECT":
      return true;
    case "INPUT":
      return !TEXT_INPUT_EXCLUDED.has((el.type ?? "text").toLowerCase());
    default:
      return false;
  }
}

import { useEffect, useRef } from "react";

/**
 * Keyboard shortcuts.
 *
 * One place decides when a keystroke is a shortcut, because the bug every
 * implementation of this ships with first is `c` opening a dialog while somebody is
 * halfway through typing a comment.
 */

/** True when the keystroke belongs to whatever the user is typing into. */
export function isTypingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null;
  if (!el || !el.tagName) return false;
  return (
    el.tagName === "INPUT" ||
    el.tagName === "TEXTAREA" ||
    el.tagName === "SELECT" ||
    el.isContentEditable
  );
}

/**
 * useShortcut runs handler for plain keystrokes only: never while a field has focus, and
 * never with a modifier held, so browser and OS bindings (⌘K, ⌘R, ⌥←) keep working.
 *
 * The handler is held in a ref so a component can close over fresh state without
 * re-binding the listener on every render.
 */
export function useShortcut(handler: (e: KeyboardEvent) => void, enabled = true): void {
  const ref = useRef(handler);
  ref.current = handler;

  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTypingTarget(e.target)) return;
      ref.current(e);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [enabled]);
}

/** How long a `g` stays armed waiting for its second key. */
const CHORD_MS = 1200;

/**
 * useChord implements "press g, then i" navigation. The prefix expires so a stray g
 * cannot silently swallow the next keystroke minutes later.
 */
export function useChord(prefix: string, routes: Record<string, string>, go: (to: string) => void): void {
  const armed = useRef<number | null>(null);

  useShortcut((e) => {
    if (armed.current !== null && Date.now() - armed.current < CHORD_MS) {
      const to = routes[e.key.toLowerCase()];
      armed.current = null;
      if (to) {
        e.preventDefault();
        go(to);
      }
      return;
    }
    if (e.key.toLowerCase() === prefix) {
      e.preventDefault();
      armed.current = Date.now();
    }
  });
}

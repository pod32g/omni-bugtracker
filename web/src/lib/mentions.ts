/**
 * Mention support for the comment composer and the markdown renderer.
 *
 * The handle grammar mirrors internal/prose/handles.go — if the two drift, the composer
 * suggests names the server will not resolve, which reads as the mention silently
 * failing.
 */

import type { User } from "./api";

/** A handle written in prose. The leading boundary keeps email addresses out. */
const MENTION = /(^|[^\w@.-])@([A-Za-z0-9][A-Za-z0-9._-]{0,63})/g;

/** What a user can be addressed by: their display name or their email local-part. */
export function handlesFor(u: User): string[] {
  const out = [u.display_name, u.email.split("@")[0]];
  return out.filter(Boolean).map((h) => h.toLowerCase());
}

/** The handle the composer should insert for a user — display name where it is usable. */
export function preferredHandle(u: User): string {
  const name = u.display_name?.trim() ?? "";
  // A display name with spaces cannot round-trip through the grammar above.
  if (name && !/\s/.test(name)) return name;
  return u.email.split("@")[0];
}

interface MdNode {
  type: string;
  value?: string;
  children?: MdNode[];
  data?: { hName?: string; hProperties?: Record<string, unknown> };
}

/**
 * remarkMentions paints @handles so a mention is visible as one. Uses data.hName rather
 * than a link node because there is no user page to link to, and a dead anchor is worse
 * than a highlighted span.
 *
 * Like remarkIssueKeys it walks the mdast, so code spans and blocks — leaf nodes with no
 * children — are structurally out of reach.
 */
export function remarkMentions() {
  return (tree: MdNode) => paint(tree);
}

function paint(node: MdNode): void {
  if (!node.children) return;
  if (node.type === "link" || node.type === "linkReference" || node.type === "definition") return;

  let changed = false;
  const next: MdNode[] = [];
  for (const child of node.children) {
    if (child.type === "text" && child.value) {
      const split = splitMentions(child.value);
      if (split) {
        next.push(...split);
        changed = true;
        continue;
      }
    }
    paint(child);
    next.push(child);
  }
  if (changed) node.children = next;
}

function splitMentions(value: string): MdNode[] | null {
  MENTION.lastIndex = 0;
  if (!MENTION.test(value)) return null;
  MENTION.lastIndex = 0;

  const out: MdNode[] = [];
  let last = 0;
  for (let m = MENTION.exec(value); m !== null; m = MENTION.exec(value)) {
    const at = m.index + m[1].length; // skip the boundary character we matched
    if (at > last) out.push({ type: "text", value: value.slice(last, at) });
    out.push({
      type: "mention",
      data: { hName: "span", hProperties: { className: "mention" } },
      children: [{ type: "text", value: `@${m[2]}` }],
    });
    last = at + m[2].length + 1;
  }
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

/**
 * mentionQuery reports the handle fragment being typed immediately before the caret, or
 * null when the caret is not in a mention. Used to decide whether to show the picker.
 */
export function mentionQuery(text: string, caret: number): { query: string; start: number } | null {
  const upto = text.slice(0, caret);
  const at = upto.lastIndexOf("@");
  if (at < 0) return null;

  // Same boundary rule as the grammar: an @ glued to a word is part of an address.
  const before = at === 0 ? "" : upto[at - 1];
  if (before && /[\w@.-]/.test(before)) return null;

  const fragment = upto.slice(at + 1);
  // A space ends the mention; so does anything the grammar cannot contain.
  if (fragment && !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(fragment)) return null;
  return { query: fragment.toLowerCase(), start: at };
}

/** Ranks candidates for the picker: prefix matches first, then substring, then by name. */
export function matchUsers(users: User[], query: string, limit = 6): User[] {
  const scored = users
    .map((u) => {
      const hs = handlesFor(u);
      if (!query) return { u, score: 2 };
      if (hs.some((h) => h.startsWith(query))) return { u, score: 0 };
      if (hs.some((h) => h.includes(query))) return { u, score: 1 };
      return { u, score: -1 };
    })
    .filter((s) => s.score >= 0);

  scored.sort((a, b) => a.score - b.score || a.u.display_name.localeCompare(b.u.display_name));
  return scored.slice(0, limit).map((s) => s.u);
}

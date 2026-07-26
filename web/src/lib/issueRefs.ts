/**
 * remarkIssueKeys turns a bare issue key written in prose — "same root cause as BUG-33" —
 * into a link to that issue.
 *
 * Runs on the mdast, not on the raw string, which is what keeps it honest: `code` and
 * `inlineCode` are leaf nodes with no children, so a key inside a pasted log or a snippet
 * is never touched, and existing links are skipped rather than nested (nested anchors are
 * invalid HTML and the browser silently rearranges them).
 *
 * Uppercase-only, mirroring the server-side parser in internal/git/mentions.go. A key that
 * names no issue still renders as a link; the issue page 404s, which is a better outcome
 * than resolving every key in every body just to decide how to paint it.
 */

const ISSUE_KEY = /\b([A-Z][A-Z0-9]{1,9})-(\d+)\b/g;

interface MdNode {
  type: string;
  value?: string;
  url?: string;
  children?: MdNode[];
}

export function remarkIssueKeys() {
  return (tree: MdNode) => linkify(tree);
}

function linkify(node: MdNode): void {
  if (!node.children) return;
  // Anything already a link keeps its own text.
  if (node.type === "link" || node.type === "linkReference" || node.type === "definition") return;

  let changed = false;
  const next: MdNode[] = [];
  for (const child of node.children) {
    if (child.type === "text" && child.value) {
      const split = splitKeys(child.value);
      if (split) {
        next.push(...split);
        changed = true;
        continue;
      }
    }
    linkify(child);
    next.push(child);
  }
  if (changed) node.children = next;
}

/** Splits a text value into alternating text and link nodes, or null when it holds no key. */
function splitKeys(value: string): MdNode[] | null {
  ISSUE_KEY.lastIndex = 0;
  if (!ISSUE_KEY.test(value)) return null;
  ISSUE_KEY.lastIndex = 0;

  const out: MdNode[] = [];
  let last = 0;
  for (let m = ISSUE_KEY.exec(value); m !== null; m = ISSUE_KEY.exec(value)) {
    if (m.index > last) out.push({ type: "text", value: value.slice(last, m.index) });
    out.push({
      type: "link",
      url: `/issues/${m[0]}`,
      children: [{ type: "text", value: m[0] }],
    });
    last = m.index + m[0].length;
  }
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

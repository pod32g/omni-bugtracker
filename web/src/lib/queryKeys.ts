/**
 * Query keys for the issue family.
 *
 * Three screens fetch issue lists and two of them page, so the same logical query
 * has two incompatible cache shapes: `useQuery` stores `{items,total}` while
 * `useInfiniteQuery` stores `{pages,pageParams}`. TanStack Query keys a cache entry
 * by the hash of the key alone — it does not distinguish infinite from non-infinite
 * — so any two of these that hash equal share one entry and one of them reads a
 * value it cannot parse.
 *
 * That was not hypothetical: the dashboard gadget used `["issues", key, filter, ""]`
 * and the issue list used `["issues", key, filter, sort]` with `sort` initialised to
 * `""`. The gadget's own "View all" link passes its filter through `?filter=`, so
 * clicking it produced an exact key collision, and the list's `getNextPageParam` ran
 * against `{items,total}` and threw during render.
 *
 * The fix is the second element: a discriminant that names the *consumer*. Everything
 * still hangs off the `["issues"]` prefix, so the fifteen existing
 * `invalidateQueries({queryKey: issueKeys.all})` calls keep invalidating all three.
 */
export const issueKeys = {
  /** Prefix for every issue list — invalidating this refreshes all of them. */
  all: ["issues"] as const,

  /** The paged issue list (useInfiniteQuery). */
  list: (projectKey: string, filter: string, sort: string) =>
    ["issues", "list", projectKey, filter, sort] as const,

  /** A fixed-size dashboard gadget (useQuery). */
  gadget: (projectKey: string, filter: string, limit: number) =>
    ["issues", "gadget", projectKey, filter, limit] as const,

  /** The board, which pages until the whole project is loaded (useInfiniteQuery). */
  board: (projectKey: string, filter: string) =>
    ["issues", "board", projectKey, filter] as const,
} as const;

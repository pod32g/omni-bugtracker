import { Link, useRouteError } from "react-router-dom";

/**
 * The router's `errorElement`. Without one, React Router rethrows and the whole app
 * unmounts to a blank page — the user loses their place, their filter, and any
 * half-written issue, with nothing on screen to explain it or to click.
 *
 * This is deliberately a route-level boundary rather than a single boundary around
 * the app: it resets when the user navigates, so one broken screen leaves the rest of
 * the tracker usable.
 */
export function RouteError() {
  const error = useRouteError();
  const message =
    error instanceof Error ? error.message : typeof error === "string" ? error : "Unknown error";

  return (
    <div className="mx-auto flex max-w-lg flex-col items-start gap-4 px-4 py-16 md:px-9">
      <h1 className="font-mono text-sm font-semibold uppercase tracking-caps text-critical">
        This page hit an error
      </h1>
      <p className="text-sm text-graphite">
        The rest of the tracker is still working — go back, or reload to try this page again.
      </p>
      <pre className="w-full overflow-x-auto rounded border border-hairline bg-paper-sunk p-3 font-mono text-xs text-graphite">
        {message}
      </pre>
      <div className="flex gap-2">
        <Link
          to="/"
          className="rounded border border-hairline px-3 py-1.5 text-sm font-medium hover:bg-paper-sunk"
        >
          Go to dashboard
        </Link>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="rounded border border-hairline px-3 py-1.5 text-sm font-medium hover:bg-paper-sunk"
        >
          Reload
        </button>
      </div>
    </div>
  );
}

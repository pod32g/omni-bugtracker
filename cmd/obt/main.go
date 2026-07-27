// Command obt is the terminal client for Omni-BugTracker.
//
// The README calls this project "developer-first, git-native, API-first" and then
// offers a browser SPA and raw curl. This is the client that description promises:
// git-aware defaults so the common commands need no arguments, --json on everything
// for piping into jq, and exit codes CI can gate on.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"

	"github.com/omni/bugtracker/internal/domain"
)

// Exit codes. Distinct rather than a blanket 1, because the whole point of a CLI in
// CI is being able to tell "not found" from "the server is down" without parsing text.
const (
	exitOK         = 0
	exitUsage      = 2
	exitNotFound   = 4
	exitForbidden  = 5
	exitValidation = 6
	exitServer     = 7
)

var (
	flagServer  = flag.String("server", "", "server base URL (default: config, $OBT_SERVER)")
	flagToken   = flag.String("token", "", "API token (default: config, $OBT_TOKEN)")
	flagHost    = flag.String("host", "", "config host section to use")
	flagProject = flag.String("p", "", "project key (default: config, $OBT_PROJECT, git remote)")
	flagJSON    = flag.Bool("json", false, "emit raw JSON")
)

func main() {
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(exitUsage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, args[0], args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "obt: "+err.Error())
		os.Exit(exitFor(err))
	}
}

// exitFor maps an error to an exit code a CI job can branch on.
func exitFor(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Status == 404:
			return exitNotFound
		case apiErr.Status == 401 || apiErr.Status == 403:
			return exitForbidden
		case apiErr.Status == 422 || apiErr.Status == 400 || apiErr.Status == 409:
			return exitValidation
		default:
			return exitServer
		}
	}
	if errors.Is(err, errUsage) {
		return exitUsage
	}
	return exitServer
}

var errUsage = errors.New("usage")

func usage() {
	fmt.Fprint(os.Stderr, `obt — Omni-BugTracker from the terminal

Usage: obt [flags] <command> [args]

Commands:
  ls [filter]            list issues            obt ls "is:open assignee:@me"
  show [KEY]             show one issue         obt show BUG-42
  new -t bug -T "title"  file an issue ($EDITOR for the body)
  comment [KEY] [text]   add a comment ($EDITOR when text is omitted)
  mv [KEY] <status>      transition             obt mv in_progress
  assign [KEY] <email>   assign ("@me", "-" to clear)
  watch [KEY]            watch; --off to unwatch
  spend [KEY] <dur>      log time               obt spend 90m -m "retry loop"
  projects               list projects
  whoami                 show the authenticated user

KEY defaults to the issue named by the current branch (bug-42-fix → BUG-42).

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nConfig: %s\n", configPath())
	fmt.Fprint(os.Stderr, `
  default = "prod"
  [hosts.prod]
  server = "http://tracker.example:8092"
  token  = "obt_…"
  project = "BUG"

Exit codes: 0 ok, 2 usage, 4 not found, 5 forbidden, 6 rejected, 7 server/transport.
`)
}

func run(ctx context.Context, command string, args []string) error {
	cfg, err := LoadConfig(*flagHost)
	if err != nil {
		return err
	}
	if *flagServer != "" {
		cfg.Server = strings.TrimRight(*flagServer, "/")
	}
	if *flagToken != "" {
		cfg.Token = *flagToken
	}
	client := NewClient(cfg)

	switch command {
	case "ls", "list":
		return cmdList(ctx, client, cfg, args)
	case "show", "view":
		return cmdShow(ctx, client, args)
	case "new", "create":
		return cmdNew(ctx, client, cfg, args)
	case "comment":
		return cmdComment(ctx, client, args)
	case "mv", "move", "transition":
		return cmdTransition(ctx, client, args)
	case "assign":
		return cmdAssign(ctx, client, args)
	case "watch":
		return cmdWatch(ctx, client, args)
	case "spend":
		return cmdSpend(ctx, client, args)
	case "projects":
		return cmdProjects(ctx, client)
	case "whoami":
		return cmdWhoami(ctx, client)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("%w: unknown command %q", errUsage, command)
	}
}

// resolveProject picks the project: -p, then config/env, then the git remote.
func resolveProject(cfg Config) string {
	if *flagProject != "" {
		return strings.ToUpper(*flagProject)
	}
	if cfg.Project != "" {
		return strings.ToUpper(cfg.Project)
	}
	return ProjectFromRemote()
}

// resolveKey takes an explicit key, or infers one from the branch. Returns the
// remaining arguments so callers can treat the key as optional in the middle of a
// command line — `obt mv in_progress` and `obt mv BUG-42 in_progress` both work.
func resolveKey(args []string) (string, []string, error) {
	if len(args) > 0 && looksLikeKey(args[0]) {
		return strings.ToUpper(args[0]), args[1:], nil
	}
	if branch := CurrentBranch(); branch != "" {
		if key := IssueKeyFromBranch(branch); key != "" {
			// Announced, not assumed. A branch called `release-2026` is a valid key
			// shape (RELEASE-2026), so the only safe version of this convenience is
			// one that always says which issue it decided to act on.
			fmt.Fprintf(os.Stderr, "obt: using %s (from branch %s)\n", key, branch)
			return key, args, nil
		}
	}
	return "", args, fmt.Errorf("%w: no issue key given, and the branch %q does not name one",
		errUsage, CurrentBranch())
}

// parseInterspersed lets flags appear after positional arguments.
//
// Go's flag package stops at the first non-flag, so `obt ls "is:open" -n 5` silently
// folded "-n 5" into the filter and reported no matches — a wrong answer that looks
// like a real one. This parses, sets the positionals aside, and parses again until
// nothing flag-shaped is left.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, errUsage
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		// Everything up to the next flag is positional; resume parsing from the flag.
		next := len(rest)
		for i, a := range rest {
			if strings.HasPrefix(a, "-") && a != "-" {
				next = i
				break
			}
		}
		positional = append(positional, rest[:next]...)
		if next == len(rest) {
			return positional, nil
		}
		args = rest[next:]
	}
}

func looksLikeKey(s string) bool {
	key, num, ok := strings.Cut(s, "-")
	if !ok || key == "" || num == "" {
		return false
	}
	for _, r := range num {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ── commands ──

func cmdList(ctx context.Context, c *Client, cfg Config, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	sort := fs.String("sort", "", "sort order (priority, severity, due, …)")
	limit := fs.Int("n", 30, "maximum issues to list")
	all := fs.Bool("A", false, "every project, not just this one")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	filter := strings.Join(positional, " ")
	if filter == "" {
		filter = "is:open"
	}
	project := resolveProject(cfg)
	if *all {
		project = ""
	}

	out, err := c.ListIssues(ctx, project, filter, *sort, *limit)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(out)
	}
	if len(out.Items) == 0 {
		fmt.Printf("no issues match %q\n", filter)
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, i := range out.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			i.Key, i.Status, i.Priority, assigneeName(i.Assignee), truncate(i.Title, 60))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if out.Total > len(out.Items) {
		fmt.Printf("\n%d of %d — pass -n to see more\n", len(out.Items), out.Total)
	}
	return nil
}

func cmdShow(ctx context.Context, c *Client, args []string) error {
	key, _, err := resolveKey(args)
	if err != nil {
		return err
	}
	issue, err := c.GetIssue(ctx, key)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(issue)
	}

	fmt.Printf("%s  %s\n", issue.Key, issue.Title)
	fmt.Printf("%s · %s · %s", issue.Status, issue.Priority, issue.Type)
	if issue.Severity != nil {
		fmt.Printf(" · %s", *issue.Severity)
	}
	fmt.Printf("\nreporter %s · assignee %s\n", assigneeName(issue.Reporter), assigneeName(issue.Assignee))
	if len(issue.Labels) > 0 {
		fmt.Printf("labels   %s\n", strings.Join(issue.Labels, ", "))
	}
	if issue.DueAt != nil {
		fmt.Printf("due      %s\n", issue.DueAt.Format("2006-01-02 15:04"))
	}
	if issue.SLA != nil && issue.SLA.State != domain.SLAOK && issue.SLA.State != domain.SLAMet {
		fmt.Printf("sla      %s\n", issue.SLA.State)
	}
	if issue.Checklist != nil {
		fmt.Printf("tasks    %d/%d\n", issue.Checklist.Done, issue.Checklist.Total)
	}
	if issue.DescriptionMD != "" {
		fmt.Printf("\n%s\n", issue.DescriptionMD)
	}

	comments, err := c.ListComments(ctx, key)
	if err != nil {
		// The issue is already printed; failing the whole command over the comment
		// list would throw away what the user asked for.
		fmt.Fprintln(os.Stderr, "obt: could not load comments: "+err.Error())
		return nil
	}
	for _, cm := range comments {
		fmt.Printf("\n--- %s · %s\n%s\n",
			assigneeName(cm.Author), cm.CreatedAt.Format("2006-01-02 15:04"), cm.BodyMD)
	}
	return nil
}

func cmdNew(ctx context.Context, c *Client, cfg Config, args []string) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	issueType := fs.String("t", "bug", "type: bug, task, feature, improvement")
	title := fs.String("T", "", "title (required)")
	severity := fs.String("s", "", "severity")
	priority := fs.String("P", "", "priority")
	labels := fs.String("l", "", "comma-separated labels")
	body := fs.String("m", "", "description; omit to open $EDITOR")
	assignee := fs.String("a", "", "assignee email, or @me")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return fmt.Errorf("%w: a title is required (-T)", errUsage)
	}
	project := resolveProject(cfg)
	if project == "" {
		return fmt.Errorf("%w: no project — pass -p, set it in the config, or run inside the repo", errUsage)
	}

	description := *body
	if description == "" {
		edited, err := EditorBody("\n#! Describe the issue. Lines starting with #! are ignored.\n")
		if err != nil {
			return err
		}
		if strings.TrimSpace(edited) == "" {
			return fmt.Errorf("aborting: empty description")
		}
		description = edited
	}

	payload := map[string]any{"type": *issueType, "title": *title, "description_md": description}
	if *severity != "" {
		payload["severity"] = *severity
	}
	if *priority != "" {
		payload["priority"] = *priority
	}
	if *labels != "" {
		payload["labels"] = splitCSV(*labels)
	}
	if *assignee != "" {
		id, err := resolveUser(ctx, c, *assignee)
		if err != nil {
			return err
		}
		payload["assignee_id"] = id
	}

	issue, err := c.CreateIssue(ctx, project, payload)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(issue)
	}
	fmt.Printf("%s created\n", issue.Key)
	return nil
}

func cmdComment(ctx context.Context, c *Client, args []string) error {
	key, rest, err := resolveKey(args)
	if err != nil {
		return err
	}
	body := strings.Join(rest, " ")
	if strings.TrimSpace(body) == "" {
		body, err = EditorBody("\n#! Your comment. Lines starting with #! are ignored.\n")
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("aborting: empty comment")
	}
	out, err := c.AddComment(ctx, key, body)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(out)
	}
	fmt.Printf("commented on %s\n", key)
	return nil
}

func cmdTransition(ctx context.Context, c *Client, args []string) error {
	key, rest, err := resolveKey(args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("%w: which status? (%s)", errUsage, statusNames())
	}
	issue, err := c.Transition(ctx, key, strings.ToLower(rest[0]))
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(issue)
	}
	fmt.Printf("%s → %s\n", issue.Key, issue.Status)
	return nil
}

func cmdAssign(ctx context.Context, c *Client, args []string) error {
	key, rest, err := resolveKey(args)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("%w: assign to whom? (an email, @me, or - to clear)", errUsage)
	}
	id, err := resolveUser(ctx, c, rest[0])
	if err != nil {
		return err
	}
	issue, err := c.UpdateIssue(ctx, key, map[string]any{"assignee_id": id})
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(issue)
	}
	fmt.Printf("%s assigned to %s\n", issue.Key, assigneeName(issue.Assignee))
	return nil
}

func cmdWatch(ctx context.Context, c *Client, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	off := fs.Bool("off", false, "stop watching")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	key, _, err := resolveKey(positional)
	if err != nil {
		return err
	}
	if err := c.SetWatching(ctx, key, !*off); err != nil {
		return err
	}
	if *off {
		fmt.Printf("no longer watching %s\n", key)
	} else {
		fmt.Printf("watching %s\n", key)
	}
	return nil
}

func cmdSpend(ctx context.Context, c *Client, args []string) error {
	fs := flag.NewFlagSet("spend", flag.ContinueOnError)
	note := fs.String("m", "", "what you did")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	key, rest, err := resolveKey(positional)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("%w: how long? (e.g. 90m, 1.5h, 2d)", errUsage)
	}
	entry, err := c.LogTime(ctx, key, rest[0], *note)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(entry)
	}
	fmt.Printf("logged %d minutes on %s\n", entry.Minutes, key)
	return nil
}

func cmdProjects(ctx context.Context, c *Client) error {
	projects, err := c.ListProjects(ctx)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(projects)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\n", p.Key, p.Name)
	}
	return w.Flush()
}

func cmdWhoami(ctx context.Context, c *Client) error {
	user, err := c.Whoami(ctx)
	if err != nil {
		return err
	}
	if *flagJSON {
		return emitJSON(user)
	}
	fmt.Printf("%s <%s> · %s\n", user.DisplayName, user.Email, user.Role)
	return nil
}

// ── helpers ──

// resolveUser turns "@me", "-" or an email into an assignee id. "-" is the API's
// zero-uuid sentinel for "clear", which is not something anyone would type.
func resolveUser(ctx context.Context, c *Client, who string) (string, error) {
	switch who {
	case "-", "none", "":
		return "00000000-0000-0000-0000-000000000000", nil
	case "@me":
		me, err := c.Whoami(ctx)
		if err != nil {
			return "", err
		}
		return me.ID.String(), nil
	}
	users, err := c.ListUsers(ctx)
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if strings.EqualFold(u.Email, who) || strings.EqualFold(u.DisplayName, who) {
			return u.ID.String(), nil
		}
	}
	return "", fmt.Errorf("no user matching %q", who)
}

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func assigneeName(u *domain.User) string {
	if u == nil {
		return "-"
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func statusNames() string {
	names := make([]string, 0, len(domain.AllStatuses))
	for _, s := range domain.AllStatuses {
		names = append(names, string(s))
	}
	return strings.Join(names, ", ")
}

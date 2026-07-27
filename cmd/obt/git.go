package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// branchKeyRe finds an issue key in a branch name: `bug-42-fix-paging`, `BUG-42`,
// `feature/BUG-42_paging`. Anchored to a boundary so `sub-4200` is not BUG-42.
var branchKeyRe = regexp.MustCompile(`(?i)(^|[/_-])([a-z][a-z0-9]{1,9})-(\d+)($|[/_-])`)

// CurrentBranch returns the checked-out branch, or "" outside a repo.
func CurrentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IssueKeyFromBranch infers the issue from the branch name, so `obt mv in_progress`
// works with no arguments on a branch called `bug-42-fix-paging`.
//
// The key is upper-cased because branch names are conventionally lower-case and issue
// keys never are. Returns "" when the branch says nothing — guessing from a branch
// called `main` would act on whatever issue happened to be numbered like the date.
func IssueKeyFromBranch(branch string) string {
	m := branchKeyRe.FindStringSubmatch(branch)
	if m == nil {
		return ""
	}
	return strings.ToUpper(m[2]) + "-" + m[3]
}

// ProjectFromRemote guesses the project key from the origin remote's repository name,
// upper-cased: `git@github.com:pod32g/omni-bugtracker.git` → OMNI-BUGTRACKER.
//
// A guess, and treated as one: it is only consulted after the config file and the
// environment, and every command that uses it can be overridden with -p.
func ProjectFromRemote() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")
	if i := strings.LastIndexAny(url, "/:"); i >= 0 {
		url = url[i+1:]
	}
	if url == "" {
		return ""
	}
	return strings.ToUpper(url)
}

// EditorBody opens $EDITOR on a temp file seeded with `seed` and returns what was
// written, with comment lines removed.
//
// Comment lines let the template carry instructions without them ending up in the
// issue — the same convention git commit messages use, which is what a terminal user
// already expects.
func EditorBody(seed string) (string, error) {
	editor := os.Getenv("OBT_EDITOR")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	f, err := os.CreateTemp("", "obt-*.md")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name()) //nolint:errcheck
	if _, err := f.WriteString(seed); err != nil {
		f.Close() //nolint:errcheck,gosec
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	// The editor owns the terminal for the duration; anything else garbles the screen.
	cmd := exec.Command("sh", "-c", editor+" \""+f.Name()+"\"")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}

	raw, err := os.ReadFile(f.Name())
	if err != nil {
		return "", err
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#!") {
			continue // "#!" is the comment marker; "#" alone is a markdown heading
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), nil
}

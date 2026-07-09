package git

import "testing"

var (
	closeVerbs = []string{"fixes", "fix", "closes", "close", "resolves", "resolve"}
	linkVerbs  = []string{"refs", "ref", "see", "related"}
)

func TestRefParser(t *testing.T) {
	p := NewRefParser(closeVerbs, linkVerbs)

	cases := []struct {
		name string
		text string
		want []Ref
	}{
		{
			name: "close verb resolves",
			text: "Fixes BUG-421: null deref in auth",
			want: []Ref{{Verb: "fixes", ProjectKey: "BUG", Number: 421, Close: true}},
		},
		{
			name: "link verb only",
			text: "Refs API-19 for context",
			want: []Ref{{Verb: "refs", ProjectKey: "API", Number: 19, Close: false}},
		},
		{
			name: "case-insensitive verb + lowercase key",
			text: "closes bug-82",
			want: []Ref{{Verb: "closes", ProjectKey: "BUG", Number: 82, Close: true}},
		},
		{
			name: "multiple refs, close wins over link for same issue",
			text: "See BUG-1 and also fixes BUG-1; refs API-2",
			want: []Ref{
				{Verb: "fixes", ProjectKey: "BUG", Number: 1, Close: true},
				{Verb: "refs", ProjectKey: "API", Number: 2, Close: false},
			},
		},
		{
			name: "no verb, no match",
			text: "Just mentioning BUG-999 casually",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Parse(tc.text)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d refs %+v, want %d", len(got), got, len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("ref %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseGitHubPush(t *testing.T) {
	payload := []byte(`{
		"repository": {"full_name": "omni/api"},
		"commits": [
			{"id":"abc123","message":"Fixes BUG-7 and refs BUG-8","url":"http://gh/abc","timestamp":"2026-07-08T10:00:00Z","author":{"name":"Dev"}}
		]
	}`)
	ev, err := Parse("github", "push", payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Kind != "push" || ev.Repo != "omni/api" || len(ev.Commits) != 1 {
		t.Fatalf("unexpected event %+v", ev)
	}
	if ev.Commits[0].SHA != "abc123" || ev.Commits[0].Author != "Dev" {
		t.Fatalf("commit parse %+v", ev.Commits[0])
	}
}

func TestParseGitHubPRMerged(t *testing.T) {
	payload := []byte(`{
		"action":"closed",
		"repository":{"full_name":"omni/api"},
		"pull_request":{"number":42,"title":"Fix auth","body":"Closes BUG-7","html_url":"http://gh/pr/42","state":"closed","merged":true,"merge_commit_sha":"def456"}
	}`)
	ev, err := Parse("github", "pull_request", payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.PR == nil || ev.PR.Number != 42 || !ev.PR.Merged {
		t.Fatalf("pr parse %+v", ev.PR)
	}
}

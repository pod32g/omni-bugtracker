package git

import (
	"encoding/json"
	"fmt"
	"time"
)

// Parse converts a provider webhook into a normalized Event. Currently GitHub-compatible
// (push + pull_request); GitLab/Gitea use very similar shapes and can be added here.
func Parse(provider, eventKind string, payload []byte) (*Event, error) {
	switch provider {
	case "", "github":
		return parseGitHub(eventKind, payload)
	default:
		return nil, fmt.Errorf("unsupported git provider %q", provider)
	}
}

type ghRepo struct {
	FullName string `json:"full_name"`
}

func parseGitHub(eventKind string, payload []byte) (*Event, error) {
	switch eventKind {
	case "push":
		return parseGitHubPush(payload)
	case "pull_request":
		return parseGitHubPR(payload)
	case "ping", "":
		return &Event{Provider: "github", Kind: eventKind}, nil
	default:
		return &Event{Provider: "github", Kind: eventKind}, nil // ignored event kind
	}
}

func parseGitHubPush(payload []byte) (*Event, error) {
	var p struct {
		Repository ghRepo `json:"repository"`
		Commits    []struct {
			ID        string `json:"id"`
			Message   string `json:"message"`
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
			Author    struct {
				Name     string `json:"name"`
				Username string `json:"username"`
			} `json:"author"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("push payload: %w", err)
	}
	ev := &Event{Provider: "github", Kind: "push", Repo: p.Repository.FullName}
	for _, c := range p.Commits {
		author := c.Author.Name
		if author == "" {
			author = c.Author.Username
		}
		ts, _ := time.Parse(time.RFC3339, c.Timestamp)
		ev.Commits = append(ev.Commits, Commit{
			SHA: c.ID, Message: c.Message, URL: c.URL, Author: author, Timestamp: ts,
		})
	}
	return ev, nil
}

func parseGitHubPR(payload []byte) (*Event, error) {
	var p struct {
		Action      string `json:"action"`
		Repository  ghRepo `json:"repository"`
		PullRequest struct {
			Number         int    `json:"number"`
			Title          string `json:"title"`
			Body           string `json:"body"`
			HTMLURL        string `json:"html_url"`
			State          string `json:"state"`
			Merged         bool   `json:"merged"`
			MergeCommitSHA string `json:"merge_commit_sha"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("pull_request payload: %w", err)
	}
	pr := p.PullRequest
	return &Event{
		Provider: "github", Kind: "pull_request", Repo: p.Repository.FullName,
		PR: &PullRequest{
			Number: pr.Number, Title: pr.Title, Body: pr.Body, URL: pr.HTMLURL,
			State: pr.State, Merged: pr.Merged, MergeCommitSHA: pr.MergeCommitSHA,
		},
	}, nil
}

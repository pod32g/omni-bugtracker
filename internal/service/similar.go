package service

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/omni/bugtracker/internal/domain"
	"github.com/omni/bugtracker/internal/httpapi"
)

// maxSimilarTerms bounds the query. A pasted stack trace in the title field would
// otherwise build a tsquery with hundreds of branches for no extra signal.
const maxSimilarTerms = 12

// similarQuery turns a title into a full-text query with OR semantics.
//
// The default `websearch_to_tsquery` behaviour ANDs every word, which is right for
// search and wrong for similarity: "crash on startup when config missing" would only
// match an issue containing all five words, and the near-duplicate phrased slightly
// differently is exactly the one being looked for. Joining with `or` keeps
// websearch_to_tsquery's escaping — it never errors on hostile input — while ranking
// does the discriminating.
//
// Returns "" when nothing useful survives, which the caller treats as "no suggestions"
// rather than "match everything".
func similarQuery(title string) string {
	seen := make(map[string]bool)
	terms := make([]string, 0, maxSimilarTerms)

	for _, word := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		// Two-character words carry almost no signal and include the words
		// websearch_to_tsquery would read as operators ("or", "and").
		if len(word) < 3 || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
		if len(terms) == maxSimilarTerms {
			break
		}
	}
	return strings.Join(terms, " or ")
}

// similarIssues suggests issues that look like the one being written.
//
// The FTS index this ranks against has been deployed since search shipped and was only
// ever queried after the fact. Filing is the one moment a duplicate is cheap to prevent.
func (h *httpHandlers) similarIssues(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	title := r.URL.Query().Get("title")

	query := similarQuery(title)
	if query == "" {
		// A title of "fix" or "?" is not enough to suggest from, and matching
		// everything would be worse than matching nothing.
		writeJSON(w, http.StatusOK, map[string]any{"items": []domain.SimilarIssue{}})
		return
	}

	items, err := h.repo.FindSimilarIssues(r.Context(), key, query,
		r.URL.Query().Get("exclude"), int32(atoiDefault(r.URL.Query().Get("limit"), 5)))
	if err != nil {
		httpapi.WriteProblem(w, http.StatusInternalServerError, "lookup failed", err.Error())
		return
	}
	if items == nil {
		items = []domain.SimilarIssue{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

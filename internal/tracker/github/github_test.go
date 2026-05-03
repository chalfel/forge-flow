package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(s.Close)
	return s
}

func TestNew_RejectsBadRepo(t *testing.T) {
	if _, err := New(Options{Repo: "no-slash", APIKey: "x"}); err == nil {
		t.Fatal("expected error for malformed repo")
	}
}

func TestFetchCandidates_FiltersByLabelAndDedupes(t *testing.T) {
	var hits []string
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer ghp_test" {
			t.Errorf("missing/incorrect auth: %q", r.Header.Get("Authorization"))
		}
		// Return one issue per label; the second call returns an issue that
		// also exists in the first list, plus a PR (which must be filtered).
		switch {
		case strings.Contains(r.URL.RawQuery, "labels=Todo"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{
					"id": 100, "number": 7, "title": "Add dark mode", "body": "...",
					"html_url": "https://github.com/o/r/issues/7",
					"state":    "open",
					"created_at": "2026-04-01T10:00:00Z",
					"updated_at": "2026-04-02T11:00:00Z",
					"labels": []map[string]string{{"name": "Todo"}, {"name": "priority:high"}},
				},
			})
		case strings.Contains(r.URL.RawQuery, "labels=In+Progress"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{ // duplicate of issue 7 — should dedupe
					"id": 100, "number": 7, "title": "Add dark mode", "body": "...",
					"html_url": "https://github.com/o/r/issues/7",
					"state":    "open",
					"labels":   []map[string]string{{"name": "Todo"}},
				},
				{ // PR — must be filtered out
					"id": 200, "number": 8, "title": "PR", "body": "",
					"html_url":     "https://github.com/o/r/pull/8",
					"state":        "open",
					"labels":       []map[string]string{{"name": "In Progress"}},
					"pull_request": map[string]any{"url": "..."},
				},
			})
		}
	})
	tr, err := New(Options{
		Endpoint:     srv.URL,
		Repo:         "o/r",
		APIKey:       "ghp_test",
		ActiveStates: []string{"Todo", "In Progress"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.FetchCandidates(context.Background(), []string{"Todo", "In Progress"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 dedupe'd, PR-filtered issue, got %d (%+v)", len(got), got)
	}
	is := got[0]
	if is.Identifier != "o/r#7" {
		t.Errorf("identifier wrong: %q", is.Identifier)
	}
	if is.State != "Todo" {
		t.Errorf("state wrong: %q", is.State)
	}
	if is.Priority != 1 {
		t.Errorf("priority should map priority:high → 1, got %d", is.Priority)
	}
	if is.BranchName != "symphony/r-7" {
		t.Errorf("branch wrong: %q", is.BranchName)
	}
	if len(hits) != 2 {
		t.Errorf("expected 2 label requests, got %d", len(hits))
	}
}

func TestGetIssue_ParsesIdentifier(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues/42") {
			t.Errorf("expected /issues/42, got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 42, "number": 42, "title": "x", "html_url": "u",
			"state":  "open",
			"labels": []map[string]string{{"name": "Todo"}},
		})
	})
	tr, _ := New(Options{Endpoint: srv.URL, Repo: "o/r", APIKey: "k", ActiveStates: []string{"Todo"}})
	got, err := tr.GetIssue(context.Background(), "o/r#42")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Identifier != "o/r#42" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetIssue_BareNumber(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/issues/3") {
			t.Errorf("expected /issues/3, got %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 3, "number": 3, "labels": []any{}})
	})
	tr, _ := New(Options{Endpoint: srv.URL, Repo: "o/r", APIKey: "k"})
	if _, err := tr.GetIssue(context.Background(), "3"); err != nil {
		t.Fatal(err)
	}
}

func TestFetchCandidates_DropsIssueWithNoMatchingLabel(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id": 1, "number": 1, "title": "x",
				"state":  "open",
				"labels": []map[string]string{{"name": "wontfix"}},
			},
		})
	})
	tr, _ := New(Options{Endpoint: srv.URL, Repo: "o/r", APIKey: "k", ActiveStates: []string{"Todo"}})
	got, err := tr.FetchCandidates(context.Background(), []string{"Todo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 issues (no matching label), got %d", len(got))
	}
}

func TestDo_HTTPErrorSurfaced(t *testing.T) {
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	})
	tr, _ := New(Options{Endpoint: srv.URL, Repo: "o/r", APIKey: "bad", ActiveStates: []string{"Todo"}})
	_, err := tr.FetchCandidates(context.Background(), []string{"Todo"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

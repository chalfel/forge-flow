package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer captures the last GraphQL request and replies with the queued
// response. Tests use it to assert query shape + map a deterministic payload
// onto domain.Issue without hitting the real Linear API.
type fakeServer struct {
	*httptest.Server
	lastQuery string
	lastVars  map[string]any
	lastAuth  string
}

func newFakeServer(t *testing.T, response any) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.lastAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.Unmarshal(body, &req)
		fs.lastQuery = req.Query
		fs.lastVars = req.Variables
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": response})
	}))
	t.Cleanup(fs.Server.Close)
	return fs
}

func TestFetchCandidates_MapsIssue(t *testing.T) {
	fs := newFakeServer(t, map[string]any{
		"issues": map[string]any{
			"nodes": []map[string]any{{
				"id":          "iss_1",
				"identifier":  "SYM-7",
				"title":       "Add dark mode",
				"description": "user wants it",
				"priority":    2,
				"branchName":  "user/sym-7-dark-mode",
				"url":         "https://linear.app/foo/SYM-7",
				"createdAt":   "2026-04-01T10:00:00Z",
				"updatedAt":   "2026-04-02T11:00:00Z",
				"state":       map[string]any{"name": "Todo"},
				"labels":      map[string]any{"nodes": []map[string]any{{"name": "frontend"}, {"name": "ux"}}},
				"inverseRelations": map[string]any{"nodes": []map[string]any{
					{"issue": map[string]any{"state": map[string]any{"name": "Done"}}},
				}},
			}},
		},
	})
	tr := New(Options{
		Endpoint:    fs.URL,
		APIKey:      "lin_test",
		ProjectSlug: "SYM",
	})
	got, err := tr.FetchCandidates(context.Background(), []string{"Todo", "In Progress"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(got))
	}
	is := got[0]
	if is.ID != "iss_1" || is.Identifier != "SYM-7" || is.Title != "Add dark mode" {
		t.Errorf("scalar fields wrong: %+v", is)
	}
	if is.Priority != 2 || is.State != "Todo" {
		t.Errorf("priority/state wrong: %+v", is)
	}
	if len(is.Labels) != 2 || is.Labels[0] != "frontend" {
		t.Errorf("labels wrong: %v", is.Labels)
	}
	if len(is.BlockedBy) != 1 || is.BlockedBy[0] != "Done" {
		t.Errorf("blocked_by wrong: %v", is.BlockedBy)
	}
	if is.CreatedAt.Year() != 2026 || is.UpdatedAt.Day() != 2 {
		t.Errorf("timestamps wrong: created=%v updated=%v", is.CreatedAt, is.UpdatedAt)
	}
	// Auth + variables propagated correctly.
	if fs.lastAuth != "lin_test" {
		t.Errorf("auth header wrong: %q", fs.lastAuth)
	}
	if fs.lastVars["slug"] != "SYM" {
		t.Errorf("slug var missing: %v", fs.lastVars)
	}
	states, _ := fs.lastVars["states"].([]any)
	if len(states) != 2 || states[0] != "Todo" {
		t.Errorf("states var wrong: %v", states)
	}
	if !strings.Contains(fs.lastQuery, "issues(") {
		t.Errorf("query missing issues(...)")
	}
}

func TestGetIssue_NotFoundReturnsNilNil(t *testing.T) {
	fs := newFakeServer(t, map[string]any{"issue": nil})
	tr := New(Options{Endpoint: fs.URL, APIKey: "k", ProjectSlug: "X"})
	got, err := tr.GetIssue(context.Background(), "missing")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil issue, got %+v", got)
	}
}

func TestDo_GraphQLErrorsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errors":[{"message":"unauthorized"}]}`))
	}))
	defer srv.Close()
	tr := New(Options{Endpoint: srv.URL, APIKey: "bad", ProjectSlug: "X"})
	_, err := tr.FetchCandidates(context.Background(), []string{"Todo"})
	if err == nil || !strings.Contains(err.Error(), "graphql") {
		t.Fatalf("expected graphql error, got %v", err)
	}
}

func TestDo_HTTPErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer srv.Close()
	tr := New(Options{Endpoint: srv.URL, APIKey: "bad", ProjectSlug: "X"})
	_, err := tr.FetchCandidates(context.Background(), []string{"Todo"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected http 403 error, got %v", err)
	}
}

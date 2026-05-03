// Package github implements the Tracker interface against GitHub Issues via
// the REST API. Because GitHub issues are natively only `open` or `closed`,
// the state machine is overlaid with **labels**: an issue is considered to be
// in state "Todo" iff it carries a label "Todo". The workflow's
// active_states / terminal_states drive both the candidate filter and the
// derived `Issue.State`.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/domain"
)

const (
	defaultEndpoint = "https://api.github.com"
	apiVersion      = "2022-11-28"
)

type Tracker struct {
	endpoint     string
	owner        string
	repo         string
	apiKey       string
	activeLabels []string
	http         *http.Client
}

type Options struct {
	APIKey       string
	Repo         string   // "owner/name"
	Endpoint     string   // optional, defaults to api.github.com
	ActiveStates []string // copied at construction so FetchCandidates can use it for label filtering
	HTTPClient   *http.Client
}

func New(opts Options) (*Tracker, error) {
	owner, name, ok := strings.Cut(opts.Repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("github: repo must be `owner/name`, got %q", opts.Repo)
	}
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Tracker{
		endpoint:     strings.TrimRight(endpoint, "/"),
		owner:        owner,
		repo:         name,
		apiKey:       opts.APIKey,
		activeLabels: opts.ActiveStates,
		http:         client,
	}, nil
}

// FetchCandidates lists open issues whose labels intersect activeStates.
// GitHub's `labels` query parameter is an AND filter, so we issue one request
// per state name and dedupe. For typical workflows this is 2-3 requests per
// tick; well within rate limits.
func (t *Tracker) FetchCandidates(ctx context.Context, activeStates []string) ([]domain.Issue, error) {
	seen := make(map[int64]struct{})
	var out []domain.Issue
	for _, state := range activeStates {
		issues, err := t.listOpenIssuesByLabel(ctx, state)
		if err != nil {
			return nil, fmt.Errorf("github: fetch candidates (%s): %w", state, err)
		}
		for _, ri := range issues {
			if _, ok := seen[ri.ID]; ok {
				continue
			}
			seen[ri.ID] = struct{}{}
			is := ri.toDomain(t.owner, t.repo, activeStates)
			if is.State == "" {
				continue
			}
			out = append(out, is)
		}
	}
	return out, nil
}

func (t *Tracker) GetIssue(ctx context.Context, id string) (*domain.Issue, error) {
	number, err := parseIDToNumber(id, t.owner, t.repo)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", t.endpoint, t.owner, t.repo, number)
	var ri rawIssue
	if err := t.do(ctx, http.MethodGet, u, &ri); err != nil {
		return nil, fmt.Errorf("github: get issue %s: %w", id, err)
	}
	is := ri.toDomain(t.owner, t.repo, t.activeLabels)
	return &is, nil
}

func (t *Tracker) listOpenIssuesByLabel(ctx context.Context, label string) ([]rawIssue, error) {
	q := url.Values{}
	q.Set("state", "open")
	q.Set("labels", label)
	q.Set("per_page", "100")
	u := fmt.Sprintf("%s/repos/%s/%s/issues?%s", t.endpoint, t.owner, t.repo, q.Encode())
	var out []rawIssue
	if err := t.do(ctx, http.MethodGet, u, &out); err != nil {
		return nil, err
	}
	// GitHub's /issues endpoint returns PRs too; filter them out.
	filtered := out[:0]
	for _, ri := range out {
		if ri.PullRequest != nil {
			continue
		}
		filtered = append(filtered, ri)
	}
	return filtered, nil
}

type rawIssue struct {
	ID          int64  `json:"id"`
	NodeID      string `json:"node_id"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	State       string `json:"state"` // "open" or "closed"
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// toDomain maps to domain.Issue. Identifier is "owner/repo#N" so Symphony's
// per-issue logging is unambiguous when one Symphony watches multiple repos.
// State is the first label matching the configured active states; if none
// match, State is left empty so the caller can drop the issue.
func (r rawIssue) toDomain(owner, repo string, activeStates []string) domain.Issue {
	labels := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, l.Name)
	}
	state := matchActiveLabel(labels, activeStates)
	branch := fmt.Sprintf("symphony/%s-%d", strings.ToLower(repo), r.Number)
	return domain.Issue{
		ID:         fmt.Sprintf("%s/%s#%d", owner, repo, r.Number),
		Identifier: fmt.Sprintf("%s/%s#%d", owner, repo, r.Number),
		Title:      r.Title,
		Description: r.Body,
		Priority:    derivePriority(labels),
		State:       state,
		BranchName:  branch,
		URL:         r.HTMLURL,
		Labels:      labels,
		CreatedAt:   parseTime(r.CreatedAt),
		UpdatedAt:   parseTime(r.UpdatedAt),
	}
}

func matchActiveLabel(labels, active []string) string {
	for _, a := range active {
		for _, l := range labels {
			if strings.EqualFold(l, a) {
				return l
			}
		}
	}
	return ""
}

// derivePriority looks for `priority:N` or `p:N` style labels and returns N
// (lower N = higher priority, matching Linear's convention). Defaults to 0.
func derivePriority(labels []string) int {
	for _, l := range labels {
		lower := strings.ToLower(l)
		for _, prefix := range []string{"priority:", "priority-", "p:", "p-"} {
			if strings.HasPrefix(lower, prefix) {
				rest := strings.TrimPrefix(lower, prefix)
				rest = strings.TrimSpace(rest)
				switch rest {
				case "0", "urgent":
					return 0
				case "1", "high":
					return 1
				case "2", "medium", "med":
					return 2
				case "3", "low":
					return 3
				}
			}
		}
	}
	return 0
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// parseIDToNumber accepts either the canonical "owner/repo#N" identifier or
// a bare number string. The latter is convenient for ad-hoc CLI usage.
func parseIDToNumber(id, owner, repo string) (int, error) {
	id = strings.TrimSpace(id)
	prefix := owner + "/" + repo + "#"
	if strings.HasPrefix(id, prefix) {
		id = strings.TrimPrefix(id, prefix)
	}
	id = strings.TrimPrefix(id, "#")
	var n int
	_, err := fmt.Sscanf(id, "%d", &n)
	if err != nil {
		return 0, fmt.Errorf("invalid github issue id %q", id)
	}
	return n, nil
}

func (t *Tracker) do(ctx context.Context, method, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("User-Agent", "symphony")

	res, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("status %d: %s", res.StatusCode, truncate(body, 500))
	}
	return json.Unmarshal(body, out)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

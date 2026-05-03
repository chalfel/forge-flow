// Package linear implements the Tracker interface against Linear's GraphQL
// API (https://api.linear.app/graphql). Issues are filtered by project slug
// and state name; the response is mapped onto domain.Issue.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chalfel/forge-flow/internal/domain"
)

const defaultEndpoint = "https://api.linear.app/graphql"

type Tracker struct {
	endpoint    string
	apiKey      string
	projectSlug string
	http        *http.Client
}

type Options struct {
	APIKey      string
	ProjectSlug string
	Endpoint    string        // optional, defaults to api.linear.app
	HTTPClient  *http.Client  // optional, defaults to a 30s-timeout client
}

func New(opts Options) *Tracker {
	endpoint := opts.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Tracker{
		endpoint:    endpoint,
		apiKey:      opts.APIKey,
		projectSlug: opts.ProjectSlug,
		http:        client,
	}
}

// FetchCandidates returns issues in the project whose state name is in
// activeStates. The Linear filter uses `project.slugId` and a state-name
// `in` clause; pagination is bounded by `first: 100` (the scheduler doesn't
// need more than one page per tick in practice).
func (t *Tracker) FetchCandidates(ctx context.Context, activeStates []string) ([]domain.Issue, error) {
	const query = `query Candidates($slug: String!, $states: [String!]!) {
		issues(
			first: 100,
			filter: {
				project: { slugId: { eq: $slug } },
				state: { name: { in: $states } }
			}
		) {
			nodes { ` + issueFields + ` }
		}
	}`
	var resp struct {
		Issues struct {
			Nodes []rawIssue `json:"nodes"`
		} `json:"issues"`
	}
	vars := map[string]any{"slug": t.projectSlug, "states": activeStates}
	if err := t.do(ctx, query, vars, &resp); err != nil {
		return nil, fmt.Errorf("linear: fetch candidates: %w", err)
	}
	out := make([]domain.Issue, 0, len(resp.Issues.Nodes))
	for _, ri := range resp.Issues.Nodes {
		out = append(out, ri.toDomain())
	}
	return out, nil
}

func (t *Tracker) GetIssue(ctx context.Context, id string) (*domain.Issue, error) {
	const query = `query GetIssue($id: String!) {
		issue(id: $id) { ` + issueFields + ` }
	}`
	var resp struct {
		Issue *rawIssue `json:"issue"`
	}
	if err := t.do(ctx, query, map[string]any{"id": id}, &resp); err != nil {
		return nil, fmt.Errorf("linear: get issue %s: %w", id, err)
	}
	if resp.Issue == nil {
		return nil, nil
	}
	is := resp.Issue.toDomain()
	return &is, nil
}

const issueFields = `
	id
	identifier
	title
	description
	priority
	branchName
	url
	createdAt
	updatedAt
	state { name }
	labels { nodes { name } }
	inverseRelations(filter: { type: { eq: "blocks" } }) {
		nodes { issue { state { name } } }
	}
`

type rawIssue struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Priority    float64 `json:"priority"`
	BranchName  string  `json:"branchName"`
	URL         string  `json:"url"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	InverseRelations struct {
		Nodes []struct {
			Issue struct {
				State struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"issue"`
		} `json:"nodes"`
	} `json:"inverseRelations"`
}

func (r rawIssue) toDomain() domain.Issue {
	labels := make([]string, 0, len(r.Labels.Nodes))
	for _, l := range r.Labels.Nodes {
		labels = append(labels, l.Name)
	}
	blocked := make([]string, 0, len(r.InverseRelations.Nodes))
	for _, n := range r.InverseRelations.Nodes {
		if name := strings.TrimSpace(n.Issue.State.Name); name != "" {
			blocked = append(blocked, name)
		}
	}
	return domain.Issue{
		ID:          r.ID,
		Identifier:  r.Identifier,
		Title:       r.Title,
		Description: r.Description,
		Priority:    int(r.Priority),
		State:       r.State.Name,
		BranchName:  r.BranchName,
		URL:         r.URL,
		Labels:      labels,
		BlockedBy:   blocked,
		CreatedAt:   parseTime(r.CreatedAt),
		UpdatedAt:   parseTime(r.UpdatedAt),
	}
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

// do POSTs a GraphQL request and unmarshals the `data` payload into out.
// GraphQL `errors` are surfaced as a single Go error joined with semicolons.
func (t *Tracker) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", t.apiKey)

	res, err := t.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("status %d: %s", res.StatusCode, truncate(raw, 500))
	}
	var env struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode response: %w (body=%s)", err, truncate(raw, 200))
	}
	if len(env.Errors) > 0 {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, string(e))
		}
		return fmt.Errorf("graphql: %s", strings.Join(msgs, "; "))
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("graphql: empty data")
	}
	return json.Unmarshal(env.Data, out)
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

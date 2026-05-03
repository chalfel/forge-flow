package domain

// IssueDraft is a captain-proposed ticket that has not been written to the
// tracker yet. Adapters convert this into a real Issue via TrackerWriter.
type IssueDraft struct {
	Title       string
	Description string
	Priority    int
	Labels      []string
}

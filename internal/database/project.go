package database

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var factKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)

// ValidateFactKey validates a fact key, which is unique within a project.
func ValidateFactKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("fact_key cannot be empty")
	}
	if len(key) > 128 {
		return fmt.Errorf("fact_key is too long (maximum 128 characters)")
	}
	if !factKeyPattern.MatchString(key) {
		return fmt.Errorf("fact_key has invalid format; only lowercase letters, digits, and . _ / - are allowed, and it must start with a lowercase letter or digit")
	}
	return nil
}

// Project is a penetration testing project with a blackboard shared across conversations.
type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ScopeJSON   string    `json:"scope_json,omitempty"`
	Status      string    `json:"status"` // active | archived
	Pinned      bool      `json:"pinned"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectFact is a project fact, stored as a blackboard entry.
type ProjectFact struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	FactKey                string    `json:"fact_key"`
	Category               string    `json:"category"`
	Summary                string    `json:"summary"`
	Body                   string    `json:"body"`
	Confidence             string    `json:"confidence"` // confirmed | tentative | deprecated
	SourceConversationID   string    `json:"source_conversation_id,omitempty"`
	SourceMessageID        string    `json:"source_message_id,omitempty"`
	Pinned                 bool      `json:"pinned"`
	SupersedesFactID       string    `json:"supersedes_fact_id,omitempty"`
	RelatedVulnerabilityID string    `json:"related_vulnerability_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// ProjectFactListFilter filters fact lists.
type ProjectFactListFilter struct {
	Category               string
	Confidence             string
	Search                 string
	RelatedVulnerabilityID string
	ExcludeDeprecated      bool // When true, excludes confidence=deprecated.
}

// CreateProject creates a project.
func (db *DB) CreateProject(p *Project) (*Project, error) {
	if p.ID == "" {
		p.ID = uuid.New().String()
	}
	if strings.TrimSpace(p.Status) == "" {
		p.Status = "active"
	}
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err := db.Exec(
		`INSERT INTO projects (id, name, description, scope_json, status, pinned, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Description, p.ScopeJSON, p.Status, boolToInt(p.Pinned), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

// GetProject gets a project.
func (db *DB) GetProject(id string) (*Project, error) {
	var p Project
	var pinned int
	var createdAt, updatedAt string
	err := db.QueryRow(
		`SELECT id, name, COALESCE(description,''), COALESCE(scope_json,''), status, pinned, created_at, updated_at
		 FROM projects WHERE id = ?`, id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.ScopeJSON, &p.Status, &pinned, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("project does not exist")
		}
		return nil, fmt.Errorf("get project: %w", err)
	}
	p.Pinned = pinned != 0
	p.CreatedAt = parseDBTime(createdAt)
	p.UpdatedAt = parseDBTime(updatedAt)
	return &p, nil
}

// ListProjects lists projects.
func (db *DB) ListProjects(status string, limit, offset int) ([]*Project, error) {
	if limit <= 0 {
		limit = 200
	}
	query := `SELECT id, name, COALESCE(description,''), COALESCE(scope_json,''), status, pinned, created_at, updated_at
		FROM projects WHERE 1=1`
	args := []interface{}{}
	if s := strings.TrimSpace(status); s != "" {
		query += " AND status = ?"
		args = append(args, s)
	}
	query += " ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	var out []*Project
	for rows.Next() {
		var p Project
		var pinned int
		var createdAt, updatedAt string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.ScopeJSON, &p.Status, &pinned, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		p.Pinned = pinned != 0
		p.CreatedAt = parseDBTime(createdAt)
		p.UpdatedAt = parseDBTime(updatedAt)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// UpdateProject updates a project.
func (db *DB) UpdateProject(p *Project) error {
	p.UpdatedAt = time.Now()
	_, err := db.Exec(
		`UPDATE projects SET name = ?, description = ?, scope_json = ?, status = ?, pinned = ?, updated_at = ? WHERE id = ?`,
		p.Name, p.Description, p.ScopeJSON, p.Status, boolToInt(p.Pinned), p.UpdatedAt, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

// DeleteProject deletes a project, cascading facts; conversation project_id is cleared by FK, and vulnerability project_id is cleared here.
func (db *DB) DeleteProject(id string) error {
	if _, err := db.Exec(`UPDATE vulnerabilities SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return fmt.Errorf("clear vulnerability project association: %w", err)
	}
	_, err := db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// GetConversationProjectID returns the project ID bound to a conversation.
func (db *DB) GetConversationProjectID(conversationID string) (string, error) {
	var pid sql.NullString
	err := db.QueryRow(`SELECT project_id FROM conversations WHERE id = ?`, conversationID).Scan(&pid)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("conversation does not exist")
		}
		return "", err
	}
	if pid.Valid {
		return strings.TrimSpace(pid.String), nil
	}
	return "", nil
}

// SetConversationProjectID sets the project for a conversation; an empty string clears the binding.
func (db *DB) SetConversationProjectID(conversationID, projectID string) error {
	projectID = strings.TrimSpace(projectID)
	if projectID != "" {
		if _, err := db.GetProject(projectID); err != nil {
			return err
		}
	}
	var val interface{}
	if projectID == "" {
		val = nil
	} else {
		val = projectID
	}
	_, err := db.Exec(`UPDATE conversations SET project_id = ?, updated_at = ? WHERE id = ?`, val, time.Now(), conversationID)
	if err != nil {
		return fmt.Errorf("set conversation project: %w", err)
	}
	return nil
}

// ListProjectFactsForIndex lists facts for blackboard index injection, excluding deprecated facts unless includeDeprecated is true.
func (db *DB) ListProjectFactsForIndex(projectID string, includeDeprecated bool) ([]*ProjectFact, error) {
	query := `SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
		COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
		COALESCE(supersedes_fact_id,''), COALESCE(related_vulnerability_id,''), created_at, updated_at
		FROM project_facts WHERE project_id = ?`
	args := []interface{}{projectID}
	if !includeDeprecated {
		query += " AND confidence != 'deprecated'"
	}
	query += " ORDER BY pinned DESC, updated_at DESC"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFacts(rows)
}

// ListProjectFacts lists project facts with pagination.
func (db *DB) ListProjectFacts(projectID string, filter ProjectFactListFilter, limit, offset int) ([]*ProjectFact, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
		COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
		COALESCE(supersedes_fact_id,''), COALESCE(related_vulnerability_id,''), created_at, updated_at
		FROM project_facts WHERE project_id = ?`
	args := []interface{}{projectID}
	if c := strings.TrimSpace(filter.Category); c != "" {
		query += " AND category = ?"
		args = append(args, c)
	}
	if c := strings.TrimSpace(filter.Confidence); c != "" {
		query += " AND confidence = ?"
		args = append(args, c)
	}
	if filter.ExcludeDeprecated {
		query += " AND confidence != 'deprecated'"
	}
	if rid := strings.TrimSpace(filter.RelatedVulnerabilityID); rid != "" {
		query += " AND related_vulnerability_id = ?"
		args = append(args, rid)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		pat := "%" + s + "%"
		query += " AND (fact_key LIKE ? OR summary LIKE ? OR body LIKE ?)"
		args = append(args, pat, pat, pat)
	}
	query += " ORDER BY pinned DESC, updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProjectFacts(rows)
}

// GetProjectFactByKey gets a fact by key.
func (db *DB) GetProjectFactByKey(projectID, factKey string) (*ProjectFact, error) {
	row := db.QueryRow(
		`SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(supersedes_fact_id,''), COALESCE(related_vulnerability_id,''), created_at, updated_at
		 FROM project_facts WHERE project_id = ? AND fact_key = ?`,
		projectID, factKey,
	)
	return scanProjectFactRow(row)
}

// GetProjectFact gets a fact by ID.
func (db *DB) GetProjectFact(id string) (*ProjectFact, error) {
	row := db.QueryRow(
		`SELECT id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(supersedes_fact_id,''), COALESCE(related_vulnerability_id,''), created_at, updated_at
		 FROM project_facts WHERE id = ?`, id,
	)
	return scanProjectFactRow(row)
}

// mergeFactBodyOnUpdate preserves the existing body when incoming body is empty to avoid losing the attack chain during summary-only updates.
func mergeFactBodyOnUpdate(incoming, existing string) string {
	if strings.TrimSpace(incoming) == "" {
		return existing
	}
	return incoming
}

// UpsertProjectFact creates or updates a fact by project_id and fact_key.
func (db *DB) UpsertProjectFact(f *ProjectFact) (*ProjectFact, error) {
	if err := ValidateFactKey(f.FactKey); err != nil {
		return nil, err
	}
	if strings.TrimSpace(f.Category) == "" {
		f.Category = "note"
	}
	if strings.TrimSpace(f.Confidence) == "" {
		f.Confidence = "tentative"
	}
	now := time.Now()

	existing, err := db.GetProjectFactByKey(f.ProjectID, f.FactKey)
	if err == nil && existing != nil {
		f.ID = existing.ID
		f.CreatedAt = existing.CreatedAt
		f.UpdatedAt = now
		f.Body = mergeFactBodyOnUpdate(f.Body, existing.Body)
		if strings.TrimSpace(f.Category) == "" {
			f.Category = existing.Category
		}
		if strings.TrimSpace(f.Confidence) == "" {
			f.Confidence = existing.Confidence
		}
		if projectFactContentChanged(existing, f) {
			versionID, verr := db.InsertProjectFactVersion(existing)
			if verr != nil {
				return nil, verr
			}
			f.SupersedesFactID = versionID
		} else if f.SupersedesFactID == "" {
			f.SupersedesFactID = existing.SupersedesFactID
		}
		_, err = db.Exec(
			`UPDATE project_facts SET category = ?, summary = ?, body = ?, confidence = ?,
				source_conversation_id = COALESCE(?, source_conversation_id),
				source_message_id = COALESCE(?, source_message_id),
				pinned = ?, supersedes_fact_id = ?, related_vulnerability_id = ?, updated_at = ?
			 WHERE id = ?`,
			f.Category, f.Summary, f.Body, f.Confidence,
			nullIfEmpty(f.SourceConversationID), nullIfEmpty(f.SourceMessageID), boolToInt(f.Pinned),
			nullIfEmpty(f.SupersedesFactID), nullIfEmpty(f.RelatedVulnerabilityID), f.UpdatedAt, f.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("update fact: %w", err)
		}
		return f, nil
	}

	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	f.CreatedAt = now
	f.UpdatedAt = now
	_, err = db.Exec(
		`INSERT INTO project_facts (
			id, project_id, fact_key, category, summary, body, confidence,
			source_conversation_id, source_message_id, pinned, supersedes_fact_id, related_vulnerability_id,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.ProjectID, f.FactKey, f.Category, f.Summary, f.Body, f.Confidence,
		nullIfEmpty(f.SourceConversationID), nullIfEmpty(f.SourceMessageID), boolToInt(f.Pinned),
		nullIfEmpty(f.SupersedesFactID), nullIfEmpty(f.RelatedVulnerabilityID),
		f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create fact: %w", err)
	}
	return f, nil
}

// DeprecateProjectFact marks a fact as deprecated.
func (db *DB) DeprecateProjectFact(projectID, factKey string) error {
	res, err := db.Exec(
		`UPDATE project_facts SET confidence = 'deprecated', updated_at = ? WHERE project_id = ? AND fact_key = ?`,
		time.Now(), projectID, factKey,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("fact does not exist")
	}
	return nil
}

// RestoreProjectFact restores a deprecated fact to tentative or confirmed so it participates in the blackboard index again.
func (db *DB) RestoreProjectFact(projectID, factKey, confidence string) error {
	confidence = strings.TrimSpace(strings.ToLower(confidence))
	if confidence == "" {
		confidence = "tentative"
	}
	if confidence != "confirmed" && confidence != "tentative" {
		return fmt.Errorf("confidence must be confirmed or tentative")
	}

	existing, err := db.GetProjectFactByKey(projectID, factKey)
	if err != nil {
		return fmt.Errorf("fact does not exist")
	}
	if strings.ToLower(strings.TrimSpace(existing.Confidence)) != "deprecated" {
		return fmt.Errorf("fact is not deprecated")
	}

	_, err = db.Exec(
		`UPDATE project_facts SET confidence = ?, updated_at = ? WHERE project_id = ? AND fact_key = ?`,
		confidence, time.Now(), projectID, factKey,
	)
	return err
}

// DeleteProjectFact deletes a fact.
func (db *DB) DeleteProjectFact(id string) error {
	_, err := db.Exec(`DELETE FROM project_facts WHERE id = ?`, id)
	return err
}

func scanProjectFacts(rows *sql.Rows) ([]*ProjectFact, error) {
	var out []*ProjectFact
	for rows.Next() {
		f, err := scanProjectFactFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanProjectFactRow(row *sql.Row) (*ProjectFact, error) {
	var f ProjectFact
	var pinned int
	var createdAt, updatedAt string
	err := row.Scan(
		&f.ID, &f.ProjectID, &f.FactKey, &f.Category, &f.Summary, &f.Body, &f.Confidence,
		&f.SourceConversationID, &f.SourceMessageID, &pinned,
		&f.SupersedesFactID, &f.RelatedVulnerabilityID, &createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("fact does not exist")
		}
		return nil, err
	}
	f.Pinned = pinned != 0
	f.CreatedAt = parseDBTime(createdAt)
	f.UpdatedAt = parseDBTime(updatedAt)
	return &f, nil
}

func scanProjectFactFromRows(rows *sql.Rows) (*ProjectFact, error) {
	var f ProjectFact
	var pinned int
	var createdAt, updatedAt string
	err := rows.Scan(
		&f.ID, &f.ProjectID, &f.FactKey, &f.Category, &f.Summary, &f.Body, &f.Confidence,
		&f.SourceConversationID, &f.SourceMessageID, &pinned,
		&f.SupersedesFactID, &f.RelatedVulnerabilityID, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	f.Pinned = pinned != 0
	f.CreatedAt = parseDBTime(createdAt)
	f.UpdatedAt = parseDBTime(updatedAt)
	return &f, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func parseDBTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// go-sqlite3 often reads DATETIME as RFC3339 with T; writes may use a space separator, so support multiple shapes.
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, e := time.Parse(layout, s); e == nil {
			return t
		}
	}
	return time.Time{}
}

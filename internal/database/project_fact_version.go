package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProjectFactVersion fact history snapshot archived before updating the same fact_key.
type ProjectFactVersion struct {
	ID                     string    `json:"id"`
	FactID                 string    `json:"fact_id"`
	ProjectID              string    `json:"project_id"`
	FactKey                string    `json:"fact_key"`
	Category               string    `json:"category"`
	Summary                string    `json:"summary"`
	Body                   string    `json:"body"`
	Confidence             string    `json:"confidence"`
	SourceConversationID   string    `json:"source_conversation_id,omitempty"`
	SourceMessageID        string    `json:"source_message_id,omitempty"`
	Pinned                 bool      `json:"pinned"`
	RelatedVulnerabilityID string    `json:"related_vulnerability_id,omitempty"`
	ArchivedAt             time.Time `json:"archived_at"`
}

// InsertProjectFactVersion writes the current fact row snapshot to the version table.
func (db *DB) InsertProjectFactVersion(f *ProjectFact) (string, error) {
	if f == nil || f.ID == "" {
		return "", fmt.Errorf("invalid fact record")
	}
	id := uuid.New().String()
	now := time.Now()
	_, err := db.Exec(
		`INSERT INTO project_fact_versions (
			id, fact_id, project_id, fact_key, category, summary, body, confidence,
			source_conversation_id, source_message_id, pinned, related_vulnerability_id, archived_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, f.ID, f.ProjectID, f.FactKey, f.Category, f.Summary, f.Body, f.Confidence,
		nullIfEmpty(f.SourceConversationID), nullIfEmpty(f.SourceMessageID), boolToInt(f.Pinned),
		nullIfEmpty(f.RelatedVulnerabilityID), now,
	)
	if err != nil {
		return "", fmt.Errorf("failed to archive fact version: %w", err)
	}
	return id, nil
}

// GetProjectFactVersion gets a snapshot by version ID.
func (db *DB) GetProjectFactVersion(versionID string) (*ProjectFactVersion, error) {
	row := db.QueryRow(
		`SELECT id, fact_id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(related_vulnerability_id,''), archived_at
		 FROM project_fact_versions WHERE id = ?`, versionID,
	)
	return scanProjectFactVersionRow(row)
}

// ListProjectFactVersions lists all historical versions for one fact, newest to oldest.
func (db *DB) ListProjectFactVersions(factID string, limit int) ([]*ProjectFactVersion, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Query(
		`SELECT id, fact_id, project_id, fact_key, category, summary, COALESCE(body,''), confidence,
			COALESCE(source_conversation_id,''), COALESCE(source_message_id,''), pinned,
			COALESCE(related_vulnerability_id,''), archived_at
		 FROM project_fact_versions WHERE fact_id = ? ORDER BY archived_at DESC LIMIT ?`,
		factID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProjectFactVersion
	for rows.Next() {
		v, err := scanProjectFactVersionFromRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func projectFactContentChanged(existing, incoming *ProjectFact) bool {
	if existing == nil || incoming == nil {
		return false
	}
	mergedBody := mergeFactBodyOnUpdate(incoming.Body, existing.Body)
	inCat := stringsTrimDefault(incoming.Category, existing.Category)
	inConf := stringsTrimDefault(incoming.Confidence, existing.Confidence)
	return existing.Summary != incoming.Summary ||
		existing.Body != mergedBody ||
		existing.Category != inCat ||
		existing.Confidence != inConf
}

func stringsTrimDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.TrimSpace(s)
}

func scanProjectFactVersionRow(row *sql.Row) (*ProjectFactVersion, error) {
	var v ProjectFactVersion
	var pinned int
	var archivedAt string
	err := row.Scan(
		&v.ID, &v.FactID, &v.ProjectID, &v.FactKey, &v.Category, &v.Summary, &v.Body, &v.Confidence,
		&v.SourceConversationID, &v.SourceMessageID, &pinned,
		&v.RelatedVulnerabilityID, &archivedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("fact version does not exist")
		}
		return nil, err
	}
	v.Pinned = pinned != 0
	v.ArchivedAt = parseDBTime(archivedAt)
	return &v, nil
}

func scanProjectFactVersionFromRows(rows *sql.Rows) (*ProjectFactVersion, error) {
	var v ProjectFactVersion
	var pinned int
	var archivedAt string
	err := rows.Scan(
		&v.ID, &v.FactID, &v.ProjectID, &v.FactKey, &v.Category, &v.Summary, &v.Body, &v.Confidence,
		&v.SourceConversationID, &v.SourceMessageID, &pinned,
		&v.RelatedVulnerabilityID, &archivedAt,
	)
	if err != nil {
		return nil, err
	}
	v.Pinned = pinned != 0
	v.ArchivedAt = parseDBTime(archivedAt)
	return &v, nil
}

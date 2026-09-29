// Package storage enumerates, evaluates, and cleans up the disk garbage produced by the runtime workspace.
//
// Design constraints (deleting active task data costs far more than the disk it saves, so all of these are hard constraints):
//   - Each category enumerates deletion units only at fixed levels under a fixed root; no unbounded recursive deletion;
//   - Every deletion unit must pass the confined check, ruling out path escapes;
//   - Directory cleanup uses "atomic rename + RemoveAll"; a crashed process only leaves a marked remnant that the next round finishes deleting;
//   - Session-scoped units query the database for last activity before deletion; active sessions are always skipped; on query failure, skip conservatively (fail closed).
package storage

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"cyberstrike-ai/internal/config"
)

// deletionMarkerSuffix marks a directory that has been condemned and is being removed.
// This follows the Prometheus TSDB approach: renaming is atomic, so a crash never leaves the original directory name in a half-deleted state.
const deletionMarkerSuffix = ".tmp-for-deletion"

// Scope describes how a deletion unit is bound to a session/project.
type Scope int

const (
	// ScopeNone is session-independent, judged only by retention period (logs, checkpoints, C2 artifacts).
	ScopeNone Scope = iota
	// ScopeConversation: the directory name is the conversation ID; conversation existence and last activity can be queried.
	ScopeConversation
	// ScopeProject: the directory name is the project ID.
	ScopeProject
)

// Paths collects the root directories of all categories.
// Injected by the app layer using the existing resolution rules (the same values as database.SetEinoConversationDirs),
// so this package never re-derives paths and cleans exactly the directories that are written.
type Paths struct {
	Workspace            string
	Reduction            string
	ConversationArtifact string
	Plantask             string
	C2                   string
	ChatUploads          string
	WorkflowCheckpoints  string
	DiagnosticLogs       string
}

// Unit is an independently deletable cleanup unit (directory or file).
type Unit struct {
	Path    string
	Session string // For non-ScopeNone scopes, the corresponding ID; empty otherwise
	Scope   Scope
	IsDir   bool
	ModTime time.Time
	Size    int64
}

// Age returns how long the unit has been idle relative to now; future timestamps (clock skew) count as 0.
func (u Unit) Age(now time.Time) time.Duration {
	if u.ModTime.IsZero() {
		return 0
	}
	if age := now.Sub(u.ModTime); age > 0 {
		return age
	}
	return 0
}

// scanner enumerates all deletion units under a root directory.
type scanner func(root string) ([]Unit, error)

// category is a named cleanup task, mirroring Gitea's [cron.*] subtask model.
type category struct {
	key        string
	label      string
	hint       string
	root       func(Paths) string
	scan       scanner
	pruneEmpty bool // Reclaim parent directories emptied by unit deletion (the chat_uploads date layer)
}

// CategoryInfo is the static metadata of a cleanup category, served to the API and frontend.
type CategoryInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// DescribeCategories returns all category metadata in registry order.
func DescribeCategories() []CategoryInfo {
	all := categories()
	out := make([]CategoryInfo, 0, len(all))
	for _, cat := range all {
		out = append(out, CategoryInfo{Key: cat.key, Label: cat.label, Hint: cat.hint})
	}
	return out
}

// categories returns all categories in a fixed order; the caller skips categories with an empty root.
func categories() []category {
	return []category{
		{
			key:   config.StorageCategoryWorkspace,
			label: "Agent workspace",
			hint:  "Working directory for agent downloads and scan artifacts (tmp/workspace), split by project/conversation.",
			root:  func(p Paths) string { return p.Workspace },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryReduction,
			label: "Tool output cache",
			hint:  "Truncated spill files of oversized tool output (tmp/reduction), one file per execution; purely derived data.",
			root:  func(p Paths) string { return p.Reduction },
			scan:  scanScopedDirs,
		},
		{
			key:   config.StorageCategoryConversationArtifact,
			label: "Conversation artifacts",
			hint:  "Summary records and oversized user-input ledgers (data/conversation_artifacts).",
			root:  func(p Paths) string { return p.ConversationArtifact },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryPlantask,
			label: "Plan task board",
			hint:  "Eino multi-agent plan board JSON (skills/.eino/plantask); purely derived data.",
			root:  func(p Paths) string { return p.Plantask },
			scan:  func(root string) ([]Unit, error) { return scanSessionDirs(root, ScopeConversation) },
		},
		{
			key:   config.StorageCategoryC2Artifacts,
			label: "C2 artifacts",
			hint:  "C2 screenshots, uploads, delivered files, and generated payload binaries (tmp/c2). After cleanup, the corresponding payload download links stop working.",
			root:  func(p Paths) string { return p.C2 },
			scan: func(root string) ([]Unit, error) {
				return scanSubdirFiles(root, "results", "uploads", "downstream", "payloads")
			},
		},
		{
			key:        config.StorageCategoryChatUploads,
			label:      "Conversation uploads",
			hint:       "Attachments uploaded by users in conversations (chat_uploads/date/conversation).",
			root:       func(p Paths) string { return p.ChatUploads },
			scan:       scanDatedSessionDirs,
			pruneEmpty: true,
		},
		{
			key:   config.StorageCategoryWorkflowCheckpoints,
			label: "Workflow checkpoints",
			hint:  "Workflow run checkpoint files (data/workflow-checkpoints), used only to resume interrupted runs.",
			root:  func(p Paths) string { return p.WorkflowCheckpoints },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "*.ckpt", "*.ckpt.tmp")
			},
		},
		{
			key:   config.StorageCategoryDiagnosticLogs,
			label: "Diagnostic logs",
			hint:  "Daily-rotated diagnostic logs (log/diagnostic-*.log).",
			root:  func(p Paths) string { return p.DiagnosticLogs },
			scan: func(root string) ([]Unit, error) {
				return scanPatternFiles(root, "diagnostic-*.log")
			},
		},
	}
}

// chatUploadsPlaceholderConvs holds placeholder directory names used when an upload happens
// before a conversation ID exists; they cannot be queried as conversation IDs, so they are
// treated as non-session units (retention only).
var chatUploadsPlaceholderConvs = map[string]bool{"_new": true, "_manual": true}

// scanScopedDirs enumerates root/projects/<id> and root/conversations/<id>.
func scanScopedDirs(root string) ([]Unit, error) {
	scopes := []struct {
		dir   string
		scope Scope
	}{{"projects", ScopeProject}, {"conversations", ScopeConversation}}

	var units []Unit
	for _, s := range scopes {
		base := filepath.Join(root, s.dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			u, err := statUnit(filepath.Join(base, e.Name()), e.Name(), s.scope)
			if err != nil {
				continue
			}
			units = append(units, u)
		}
	}
	return units, nil
}

// scanSessionDirs enumerates root/<id>, where the directory name is the conversation ID.
func scanSessionDirs(root string, scope Scope) ([]Unit, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		u, err := statUnit(filepath.Join(root, e.Name()), e.Name(), scope)
		if err != nil {
			continue
		}
		units = append(units, u)
	}
	return units, nil
}

// scanDatedSessionDirs enumerates root/<YYYY-MM-DD>/<conversationID|placeholder>.
func scanDatedSessionDirs(root string) ([]Unit, error) {
	dates, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, d := range dates {
		if !d.IsDir() {
			continue
		}
		dateDir := filepath.Join(root, d.Name())
		convs, err := os.ReadDir(dateDir)
		if err != nil {
			continue
		}
		for _, cd := range convs {
			if !cd.IsDir() {
				// Stray files directly under the date directory: cleaned as non-session files.
				if u, err := statUnit(filepath.Join(dateDir, cd.Name()), "", ScopeNone); err == nil {
					units = append(units, u)
				}
				continue
			}
			session, scope := cd.Name(), ScopeConversation
			if chatUploadsPlaceholderConvs[session] {
				session, scope = "", ScopeNone
			}
			if u, err := statUnit(filepath.Join(dateDir, cd.Name()), session, scope); err == nil {
				units = append(units, u)
			}
		}
	}
	return units, nil
}

// scanSubdirFiles enumerates files under root/<subdir>/ (non-recursive), used for C2 artifacts.
func scanSubdirFiles(root string, subdirs ...string) ([]Unit, error) {
	var units []Unit
	for _, sub := range subdirs {
		base := filepath.Join(root, sub)
		entries, err := os.ReadDir(base)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if u, err := statUnit(filepath.Join(base, e.Name()), "", ScopeNone); err == nil {
				units = append(units, u)
			}
		}
	}
	return units, nil
}

// scanPatternFiles enumerates files under root matching any of the globs.
func scanPatternFiles(root string, patterns ...string) ([]Unit, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var units []Unit
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		matched := false
		for _, p := range patterns {
			if ok, _ := filepath.Match(p, e.Name()); ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if u, err := statUnit(filepath.Join(root, e.Name()), "", ScopeNone); err == nil {
			units = append(units, u)
		}
	}
	return units, nil
}

// statUnit collects a unit's size and last modification time.
// Uses Lstat: symbolic links count only the link itself and are never followed (otherwise data
// outside the working directory could be counted or even deleted).
func statUnit(path, session string, scope Scope) (Unit, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Unit{}, err
	}
	u := Unit{
		Path:    path,
		Session: session,
		Scope:   scope,
		IsDir:   info.IsDir(),
		ModTime: info.ModTime(),
		Size:    info.Size(),
	}
	if u.IsDir {
		size, newest := dirStats(path)
		u.Size = size
		if newest.After(u.ModTime) {
			u.ModTime = newest
		}
	}
	if scope == ScopeNone {
		u.Session = ""
	}
	return u, nil
}

// dirStats sums a directory's bytes and the newest modification time inside it.
// filepath.WalkDir does not follow symlinks and skips unreadable subtrees best-effort,
// without aborting the whole scan.
func dirStats(root string) (int64, time.Time) {
	var size int64
	var newest time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if !d.IsDir() {
			size += info.Size()
		}
		if mt := info.ModTime(); mt.After(newest) {
			newest = mt
		}
		return nil
	})
	return size, newest
}

// confined verifies that candidate really is inside root, preventing any form of path escape.
func confined(root, candidate string) bool {
	if root == "" || candidate == "" {
		return false
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// pruneEmptyDirs reclaims empty directories bottom-up, descending at most depth levels.
// After chat_uploads removes conversation directories, empty date directories remain and must
// be reclaimed too. Only descendants that are truly empty are deleted; root itself is never deleted.
func pruneEmptyDirs(root string, depth int) int {
	if depth <= 0 {
		return 0
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(root, e.Name())
		removed += pruneEmptyDirs(sub, depth-1)
		if isEmptyDir(sub) && os.Remove(sub) == nil {
			removed++
		}
	}
	return removed
}

func isEmptyDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

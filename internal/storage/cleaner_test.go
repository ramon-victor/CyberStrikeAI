package storage

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"cyberstrike-ai/internal/config"
)

// fakeActivity is a test double for Activity.
type fakeActivity struct {
	conversations map[string]time.Time
	projects      map[string]time.Time
	err           error
}

func (f fakeActivity) ConversationLastActivity(id string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	at, ok := f.conversations[id]
	return at, ok, nil
}

func (f fakeActivity) ProjectLastActivity(id string) (time.Time, bool, error) {
	if f.err != nil {
		return time.Time{}, false, f.err
	}
	at, ok := f.projects[id]
	return at, ok, nil
}

// ageTree builds a directory tree under root and sets the mtime of every file and directory to now-age.
// Directory mtimes must change too: statUnit takes the newest mtime of the directory and its
// contents, so a freshly created directory would otherwise be protected by the activity guard.
func ageTree(t *testing.T, root string, files map[string]int, age time.Duration, now time.Time) {
	t.Helper()
	stamp := now.Add(-age)
	dirs := map[string]bool{root: true}
	for rel, size := range files {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatalf("chtimes %s: %v", path, err)
		}
		for dir := filepath.Dir(path); dir != root && strings.HasPrefix(dir, root); dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	// Bottom-up: touching a subdirectory refreshes the parent mtime; the reverse order would leave parents looking new.
	order := make([]string, 0, len(dirs))
	for dir := range dirs {
		order = append(order, dir)
	}
	sort.Slice(order, func(i, j int) bool { return len(order[i]) > len(order[j]) })
	for _, dir := range order {
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatalf("chtimes dir %s: %v", dir, err)
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func newTestCleaner(t *testing.T, cfg *config.Config, paths Paths, activity Activity, now time.Time) *Cleaner {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	return NewCleaner(Options{
		Config:   cfg,
		Paths:    paths,
		Activity: activity,
		Now:      func() time.Time { return now },
		CacheTTL: -1, // Disable caching in tests so every call really traverses
	})
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

func categoryCfg(days int) config.StorageCategoryConfig {
	return config.StorageCategoryConfig{RetentionDays: intPtr(days)}
}

// workspace root layout: tmp/workspace/{projects,conversations}/<id>/
func TestCleanExpiresIdleWorkspaceAndKeepsFresh(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")

	old := filepath.Join(ws, "conversations", "conv-old")
	fresh := filepath.Join(ws, "conversations", "conv-fresh")
	ageTree(t, old, map[string]int{"scan/nmap.txt": 4096}, 40*24*time.Hour, now)
	ageTree(t, fresh, map[string]int{"scan/nmap.txt": 1024}, 2*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{
		"conv-old":   now.Add(-40 * 24 * time.Hour),
		"conv-fresh": now.Add(-2 * 24 * time.Hour),
	}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{Categories: []string{config.StorageCategoryWorkspace}})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(fresh) {
		t.Errorf("unexpired workspace was deleted: %s", fresh)
	}
	if exists(old) {
		t.Errorf("workspace past retention was not deleted: %s", old)
	}
	cat := findCategory(t, rep, config.StorageCategoryWorkspace)
	if cat.RemovedUnits != 1 {
		t.Errorf("RemovedUnits = %d, want 1", cat.RemovedUnits)
	}
	if cat.FreedBytes != 4096 {
		t.Errorf("FreedBytes = %d, want 4096", cat.FreedBytes)
	}
}

func TestDryRunDeletesNothing(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	old := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, old, map[string]int{"a.txt": 2048}, 40*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{"conv-old": now.Add(-40 * 24 * time.Hour)}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{DryRun: true})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(old) {
		t.Fatal("dry-run deleted files")
	}
	if rep.Totals.ReclaimableBytes != 2048 {
		t.Errorf("ReclaimableBytes = %d, want 2048", rep.Totals.ReclaimableBytes)
	}
	if rep.Totals.RemovedUnits != 0 {
		t.Errorf("dry-run should have no RemovedUnits, got %d", rep.Totals.RemovedUnits)
	}
}

// Conversation gone from the database -> orphan directory, reclaimed under the shorter orphan_grace_days.
func TestOrphanReclaimedEvenWhenRetentionIsZero(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	orphan := filepath.Join(ws, "conversations", "conv-gone")
	ageTree(t, orphan, map[string]int{"a.txt": 10}, 5*24*time.Hour, now)

	// retention_days: 0 means no retention-based cleanup, but orphan directories are still reclaimed.
	cfg := &config.Config{Storage: config.StorageConfig{
		OrphanGraceDays: intPtr(1),
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(0),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(orphan) {
		t.Error("orphan directory was not reclaimed")
	}
}

// Conversation still exists with recent activity -> must be protected even if the directory mtime is old.
func TestActiveSessionIsProtected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-busy")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 40*24*time.Hour, now)

	activity := fakeActivity{conversations: map[string]time.Time{
		"conv-busy": now.Add(-10 * time.Minute), // still running 10 minutes ago
	}}
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, activity, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Fatal("workspace of an active conversation was deleted")
	}
	if rep.Totals.SkippedActive != 1 {
		t.Errorf("SkippedActive = %d, want 1", rep.Totals.SkippedActive)
	}
}

// When the activity lookup fails, skip conservatively: better to delete too little than to delete the data of a running task.
func TestActivityLookupErrorFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-x")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{err: errors.New("db locked")}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Fatal("no data should be deleted when the lookup fails")
	}
}

// A symlink pointing at a directory is never treated as a deletion unit by any scanner:
// os.ReadDir's DirEntry.IsDir() returns false for symlinks.
// That is the safer behavior — when a symlink to /etc is planted in the workspace,
// it is neither followed nor treated as a conversation directory.
func TestSymlinkInsideRootIsNeverADeletionUnit(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	outside := filepath.Join(tmp, "precious")
	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(outside, "sub", "keep.txt")
	if err := os.WriteFile(victim, []byte("important"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkParent := filepath.Join(ws, "conversations")
	link := filepath.Join(linkParent, "conv-link")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	stamp := now.Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(link, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, nil, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(link) {
		t.Error("a symlink must not be removed as a deletion unit")
	}
	if !exists(victim) {
		t.Fatal("symlink target deleted: path escape")
	}
	if rep.Totals.Units != 0 {
		t.Errorf("Units = %d, want 0 (symlinks not counted)", rep.Totals.Units)
	}
}

// A marker directory left by a crashed cleanup round must be deleted unconditionally;
// otherwise its mtime is the rename time and the activity guard would block it forever.
func TestLeftoverDeletionMarkerIsReclaimed(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	marker := filepath.Join(ws, "conversations", "conv-crash"+deletionMarkerSuffix)
	// The mtime is "right now", simulating an immediate rerun after a crash.
	ageTree(t, marker, map[string]int{"a.txt": 10}, 0, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: categoryCfg(30),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(marker) {
		t.Error("crash-leftover marker directory was not finished")
	}
}

// chat_uploads uses a root/<date>/<conversation> layout; after conversation directories are deleted, empty date directories must be reclaimed too.
func TestChatUploadsDatedLayoutAndEmptyDirPrune(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	uploads := filepath.Join(tmp, "chat_uploads")
	dateDir := filepath.Join(uploads, "2026-06-01")
	convDir := filepath.Join(dateDir, "conv-old")
	ageTree(t, convDir, map[string]int{"report.pdf": 100}, 120*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryChatUploads: categoryCfg(90),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{ChatUploads: uploads}, fakeActivity{}, now)

	rep, err := c.Clean(CleanRequest{})
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(convDir) {
		t.Error("expired upload directory was not deleted")
	}
	if exists(dateDir) {
		t.Error("empty date directory was not reclaimed")
	}
	if !exists(uploads) {
		t.Error("the category root must not be deleted")
	}
	// Clean with no categories returns all registered categories; look them up by key, never by index.
	cat := findCategory(t, rep, config.StorageCategoryChatUploads)
	if cat.RemovedEmptyDirs != 1 {
		t.Errorf("RemovedEmptyDirs = %d, want 1", cat.RemovedEmptyDirs)
	}
	if cat.RemovedUnits != 1 {
		t.Errorf("RemovedUnits = %d, want 1", cat.RemovedUnits)
	}
}

// findCategory returns a report entry by key; it fails fast when missing, so a wrong index cannot assert against another category.
func findCategory(t *testing.T, rep *Report, key string) CategoryReport {
	t.Helper()
	for _, cat := range rep.Categories {
		if cat.Key == key {
			return cat
		}
	}
	t.Fatalf("category %s missing from the report", key)
	return CategoryReport{}
}

// Placeholder directories _new / _manual are not conversation IDs; they must not be queried in the database and are treated as non-session units.
func TestChatUploadsPlaceholderDirIsNotSessionScoped(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	uploads := filepath.Join(tmp, "chat_uploads")
	placeholder := filepath.Join(uploads, "2026-06-01", "_new")
	ageTree(t, placeholder, map[string]int{"a.png": 10}, 120*24*time.Hour, now)

	// Activity errors on every lookup: if placeholders were treated as conversations, fail-closed would keep them.
	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryChatUploads: categoryCfg(90),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{ChatUploads: uploads},
		fakeActivity{err: errors.New("should not be called")}, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(placeholder) {
		t.Error("placeholder directories should be cleaned by retention, not via the conversation lookup")
	}
}

// Glob-style category: only matching files are deleted; other files in the same directory are untouched.
func TestPatternCategoryOnlyMatchesItsGlob(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	logs := filepath.Join(tmp, "log")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-40 * 24 * time.Hour)
	stale := filepath.Join(logs, "diagnostic-2026-08-01.log")
	keepName := filepath.Join(logs, "app.log")
	for _, p := range []string{stale, keepName} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryDiagnosticLogs: categoryCfg(14),
		},
	}}
	c := newTestCleaner(t, cfg, Paths{DiagnosticLogs: logs}, nil, now)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if exists(stale) {
		t.Error("expired diagnostic log was not deleted")
	}
	if !exists(keepName) {
		t.Error("a file not matching the glob was deleted by mistake")
	}
}

func TestDisabledCategoryIsReportedButNotCleaned(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	cfg := &config.Config{Storage: config.StorageConfig{
		Categories: map[string]config.StorageCategoryConfig{
			config.StorageCategoryWorkspace: {Enabled: boolPtr(false), RetentionDays: intPtr(30)},
		},
	}}
	c := newTestCleaner(t, cfg, Paths{Workspace: ws}, fakeActivity{}, now)

	// Inspect should still list disabled categories so administrators can see the reclaimable space before enabling them.
	inspected := c.Inspect(true)
	var found bool
	for _, cat := range inspected.Categories {
		if cat.Key == config.StorageCategoryWorkspace {
			found = true
			if cat.Enabled {
				t.Error("category should be disabled")
			}
			if cat.ReclaimableUnits != 1 {
				t.Errorf("ReclaimableUnits = %d, want 1", cat.ReclaimableUnits)
			}
		}
	}
	if !found {
		t.Fatal("Inspect did not return the workspace category")
	}

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if !exists(dir) {
		t.Error("a disabled category must not be cleaned")
	}
}

func TestUnknownCategoryIsRejected(t *testing.T) {
	now := time.Now()
	c := newTestCleaner(t, nil, Paths{Workspace: t.TempDir()}, nil, now)
	if _, err := c.Clean(CleanRequest{Categories: []string{"../../etc"}}); !errors.Is(err, ErrUnknownCategory) {
		t.Errorf("err = %v, want ErrUnknownCategory", err)
	}
}

func TestConcurrentCleanIsRejected(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	for i := 0; i < 20; i++ {
		dir := filepath.Join(ws, "conversations", "conv-"+string(rune('a'+i)))
		ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)
	}
	c := newTestCleaner(t, nil, Paths{Workspace: ws}, fakeActivity{}, now)

	// Hold the execution slot manually, simulating "a round already running".
	if !c.running.CompareAndSwap(false, true) {
		t.Fatal("could not acquire the execution slot")
	}
	if _, err := c.Clean(CleanRequest{}); !errors.Is(err, ErrCleanupInProgress) {
		t.Errorf("err = %v, want ErrCleanupInProgress", err)
	}
	c.running.Store(false)

	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("cleanup should succeed after release: %v", err)
	}
}

// When cleanup is triggered concurrently, only one round may actually run; the rest should immediately get ErrCleanupInProgress instead of queueing up deletions.
func TestParallelCleanHasSingleWinner(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-old")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	c := newTestCleaner(t, nil, Paths{Workspace: ws}, fakeActivity{}, now)

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := c.Clean(CleanRequest{})
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	inProgress := 0
	for _, err := range results {
		switch {
		case err == nil:
		case errors.Is(err, ErrCleanupInProgress):
			inProgress++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if inProgress == 0 {
		t.Log("note: all goroutines finished serially this round, no concurrency conflict observed (not a failure)")
	}
}

func TestConfinedRejectsEscapes(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "srv", "app", "tmp", "workspace")
	cases := []struct {
		candidate string
		want      bool
	}{
		{filepath.Join(root, "conversations", "abc"), true},
		{filepath.Join(root, "a", "b", "c"), true},
		{root, false},
		{filepath.Join(root, ".."), false},
		{filepath.Join(root, "..", "secrets"), false},
		{filepath.Join(string(filepath.Separator), "etc", "passwd"), false},
		{"", false},
	}
	for _, tc := range cases {
		if got := confined(root, tc.candidate); got != tc.want {
			t.Errorf("confined(%q) = %v, want %v", tc.candidate, got, tc.want)
		}
	}
	if confined("", filepath.Join(root, "x")) {
		t.Error("an empty root must not pass validation")
	}
}

// A missing root must not be an error, just a Missing flag — it is normal for the system not to have produced that kind of garbage yet.
func TestMissingRootIsNotAnError(t *testing.T) {
	now := time.Now()
	c := newTestCleaner(t, nil, Paths{Workspace: filepath.Join(t.TempDir(), "never-created")}, nil, now)

	rep := c.Inspect(true)
	if len(rep.Categories) != len(config.StorageCategoryOrder) {
		t.Fatalf("Categories = %d, want %d", len(rep.Categories), len(config.StorageCategoryOrder))
	}
	for _, cat := range rep.Categories {
		if !cat.Missing {
			t.Errorf("category %s has no/absent root and should be flagged Missing", cat.Key)
		}
		if cat.Units != 0 || len(cat.Errors) != 0 {
			t.Errorf("category %s should have no stats or errors: units=%d errors=%v", cat.Key, cat.Units, cat.Errors)
		}
	}
	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Errorf("Clean: %v", err)
	}
}

func TestInspectCachesUntilRefresh(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	tmp := t.TempDir()
	ws := filepath.Join(tmp, "workspace")
	dir := filepath.Join(ws, "conversations", "conv-a")
	ageTree(t, dir, map[string]int{"a.txt": 10}, 400*24*time.Hour, now)

	c := NewCleaner(Options{
		Config:   &config.Config{},
		Paths:    Paths{Workspace: ws},
		Activity: fakeActivity{},
		Now:      func() time.Time { return now },
		CacheTTL: time.Minute,
	})

	first := c.Inspect(false)
	if first.Totals.Units != 1 {
		t.Fatalf("first inspect Units = %d, want 1", first.Totals.Units)
	}
	// Directories added while the cache is warm must not be visible.
	ageTree(t, filepath.Join(ws, "conversations", "conv-b"), map[string]int{"b.txt": 10}, 400*24*time.Hour, now)
	if cached := c.Inspect(false); cached.Totals.Units != 1 {
		t.Errorf("cached Units = %d, want 1", cached.Totals.Units)
	}
	if refreshed := c.Inspect(true); refreshed.Totals.Units != 2 {
		t.Errorf("after refresh Units = %d, want 2", refreshed.Totals.Units)
	}
	// The cache must be invalidated after cleanup, otherwise the status page keeps showing deleted content.
	if _, err := c.Clean(CleanRequest{}); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if after := c.Inspect(false); after.Totals.Units != 0 {
		t.Errorf("after cleanup Units = %d, want 0", after.Totals.Units)
	}
}

func TestRetentionLoopDisabledByDefault(t *testing.T) {
	s := NewService(nil, &config.Config{}, nil)
	if s.AutoCleanEnabled() {
		t.Error("auto_clean must default to off")
	}
	// With it off, PurgeExpired is a no-op and must not panic.
	s.PurgeExpired()

	enabled := &config.Config{Storage: config.StorageConfig{AutoClean: boolPtr(true)}}
	if !NewService(nil, enabled, nil).AutoCleanEnabled() {
		t.Error("should be on when explicitly true")
	}
}

func TestIntervalHasFloor(t *testing.T) {
	cfg := &config.Config{Storage: config.StorageConfig{IntervalMinutes: intPtr(1)}}
	if got := NewService(nil, cfg, nil).Interval(); got != minSweepInterval {
		t.Errorf("Interval = %v, want %v", got, minSweepInterval)
	}
	cfg = &config.Config{Storage: config.StorageConfig{IntervalMinutes: intPtr(180)}}
	if got := NewService(nil, cfg, nil).Interval(); got != 3*time.Hour {
		t.Errorf("Interval = %v, want 3h", got)
	}
}

package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Step is one schema transition, applied in ascending order.
type Step struct {
	FromVersion int
	ToVersion   int
	Name        string
	Apply       func(dataDir string) error
}

// steps is the ordered migration chain for this build.
var steps = []Step{
	{FromVersion: 0, ToVersion: 1, Name: "v0-to-v1-normalize-stores", Apply: migrateV0ToV1},
}

// Options configures Run.
type Options struct {
	DataDir    string
	AppVersion string
	// BackupRoot defaults to <parent of DataDir>/migration_backups when empty.
	BackupRoot string
	// Now defaults to time.Now when nil.
	Now func() time.Time
	// Logf defaults to a no-op when nil.
	Logf func(format string, args ...any)
}

// Result reports what Run did.
type Result struct {
	FromVersion int
	ToVersion   int
	BackupDir   string
	Applied     []string
	Already     bool
}

// Run backs up the data directory, applies every migration step needed to
// reach SchemaVersion, validates the result and writes the schema stamp. If
// anything fails after the backup, the data directory is rolled back
// automatically so no metadata is lost.
func Run(opts Options) (*Result, error) {
	if opts.DataDir == "" {
		return nil, fmt.Errorf("migration: DataDir is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	if err := os.MkdirAll(opts.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("migration: mkdir data dir: %w", err)
	}

	from, err := DetectVersion(opts.DataDir)
	if err != nil {
		return nil, err
	}
	if from > SchemaVersion {
		return nil, fmt.Errorf("migration: data schema v%d is newer than supported v%d (downgrade refused)", from, SchemaVersion)
	}
	if from == SchemaVersion {
		return &Result{FromVersion: from, ToVersion: from, Already: true}, nil
	}

	backupRoot := opts.BackupRoot
	if backupRoot == "" {
		backupRoot = filepath.Join(filepath.Dir(opts.DataDir), BackupDirName)
	}
	backupDir, _, err := Backup(opts.DataDir, backupRoot, opts.AppVersion, from, now())
	if err != nil {
		return nil, fmt.Errorf("migration: backup failed, data untouched: %w", err)
	}
	logf("[Migration] pre-upgrade backup created at %s (schema v%d)", backupDir, from)

	res := &Result{FromVersion: from, BackupDir: backupDir}
	cur := from
	for cur < SchemaVersion {
		step, ok := nextStep(cur)
		if !ok {
			rollbackAfterFailure(opts.DataDir, backupDir, logf)
			return nil, fmt.Errorf("migration: no step from schema v%d towards v%d", cur, SchemaVersion)
		}
		logf("[Migration] applying %s (v%d -> v%d)", step.Name, step.FromVersion, step.ToVersion)
		if err := step.Apply(opts.DataDir); err != nil {
			rollbackAfterFailure(opts.DataDir, backupDir, logf)
			return nil, fmt.Errorf("migration: step %s failed (rolled back): %w", step.Name, err)
		}
		res.Applied = append(res.Applied, step.Name)
		cur = step.ToVersion
	}

	if err := Validate(opts.DataDir); err != nil {
		rollbackAfterFailure(opts.DataDir, backupDir, logf)
		return nil, fmt.Errorf("migration: validation failed (rolled back): %w", err)
	}

	stamp := Stamp{
		Version:     SchemaVersion,
		PrevVersion: from,
		UpdatedAt:   now().UTC(),
		AppVersion:  opts.AppVersion,
		Backup:      backupDir,
	}
	if err := writeStamp(opts.DataDir, stamp); err != nil {
		rollbackAfterFailure(opts.DataDir, backupDir, logf)
		return nil, fmt.Errorf("migration: write stamp failed (rolled back): %w", err)
	}

	res.ToVersion = SchemaVersion
	return res, nil
}

func nextStep(from int) (Step, bool) {
	for _, s := range steps {
		if s.FromVersion == from {
			return s, true
		}
	}
	return Step{}, false
}

func rollbackAfterFailure(dataDir, backupDir string, logf func(string, ...any)) {
	if err := Rollback(dataDir, backupDir); err != nil {
		logf("[Migration] WARNING automatic rollback failed: %v (backup kept at %s)", err, backupDir)
		return
	}
	logf("[Migration] rolled back to %s", backupDir)
}

// migrateV0ToV1 adds any missing required top-level key to the known stores,
// preserving all existing data. Files that already contain every key are left
// byte-for-byte unchanged.
func migrateV0ToV1(dataDir string) error {
	for _, spec := range KnownStores {
		path := filepath.Join(dataDir, spec.File)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("read %s: %w", spec.File, err)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("parse %s: %w", spec.File, err)
		}
		changed := false
		for _, key := range spec.Required {
			if _, ok := obj[key]; !ok {
				obj[key] = json.RawMessage(defaultForKey(key))
				changed = true
			}
		}
		if !changed {
			continue
		}
		if err := writeJSONAtomic(path, obj); err != nil {
			return fmt.Errorf("write %s: %w", spec.File, err)
		}
	}
	return nil
}

// defaultForKey returns the empty JSON value used to initialise a missing key.
func defaultForKey(key string) []byte {
	if key == "blocked" {
		return []byte("[]")
	}
	return []byte("{}")
}

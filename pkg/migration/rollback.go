package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Rollback restores dataDir from a backup folder produced by Backup and removes
// the schema stamp, returning the directory to its pre-upgrade state.
func Rollback(dataDir, backupDir string) error {
	raw, err := os.ReadFile(filepath.Join(backupDir, ManifestName))
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if len(man.Files) == 0 {
		return fmt.Errorf("backup %s contains no files", backupDir)
	}
	for _, f := range man.Files {
		src := filepath.Join(backupDir, f.File)
		dst := filepath.Join(dataDir, f.File)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("restore %s: %w", f.File, err)
		}
	}
	// The stamp is created by migration; it must not survive a rollback.
	if err := os.Remove(filepath.Join(dataDir, StampFile)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stamp: %w", err)
	}
	return nil
}

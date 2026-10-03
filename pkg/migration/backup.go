package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ManifestEntry records one backed-up file.
type ManifestEntry struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest describes a pre-upgrade backup.
type Manifest struct {
	CreatedAt   time.Time       `json:"created_at"`
	AppVersion  string          `json:"app_version"`
	FromVersion int             `json:"from_version"`
	Files       []ManifestEntry `json:"files"`
}

// Backup copies every top-level file of dataDir into a new timestamped folder
// under backupRoot and writes a manifest with sizes and SHA-256 hashes. The
// source directory is never modified.
func Backup(dataDir, backupRoot, appVersion string, fromVersion int, now time.Time) (string, *Manifest, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("mkdir data dir: %w", err)
	}
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", nil, fmt.Errorf("mkdir backup root: %w", err)
	}

	base := "pre-upgrade-" + now.Format("20060102-150405")
	backupDir := filepath.Join(backupRoot, base)
	for i := 1; ; i++ {
		if _, err := os.Stat(backupDir); os.IsNotExist(err) {
			break
		}
		backupDir = filepath.Join(backupRoot, fmt.Sprintf("%s-%d", base, i))
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", nil, fmt.Errorf("create backup dir: %w", err)
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", nil, fmt.Errorf("read data dir: %w", err)
	}

	man := &Manifest{CreatedAt: now.UTC(), AppVersion: appVersion, FromVersion: fromVersion}
	for _, e := range entries {
		if e.IsDir() {
			continue // only the flat JSON stores are backed up
		}
		name := e.Name()
		src := filepath.Join(dataDir, name)
		dst := filepath.Join(backupDir, name)
		if err := copyFile(src, dst); err != nil {
			return "", nil, fmt.Errorf("backup %s: %w", name, err)
		}
		info, err := os.Stat(src)
		if err != nil {
			return "", nil, fmt.Errorf("stat %s: %w", name, err)
		}
		sum, err := hashFile(src)
		if err != nil {
			return "", nil, fmt.Errorf("hash %s: %w", name, err)
		}
		man.Files = append(man.Files, ManifestEntry{File: name, Size: info.Size(), SHA256: sum})
	}

	if err := writeJSONAtomic(filepath.Join(backupDir, ManifestName), man); err != nil {
		return "", nil, fmt.Errorf("write manifest: %w", err)
	}
	return backupDir, man, nil
}

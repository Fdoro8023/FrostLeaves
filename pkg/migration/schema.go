// Package migration implements versioned, validated and reversible migrations
// for FrostLeaves' on-disk JSON state (the Windows server's "database").
//
// The server keeps its persistent state as flat JSON files inside the
// configured DataDir (default ./data):
//
//	devices.json  { devices, codes }   设备 / 验证码
//	quota.json    { configs, usage }   配额 / 用量（权限配置）
//	tokens.json   { tokens, blocked }  账号令牌
//	shares.json   { shares }           分享链接
//
// Legacy builds (<= 1.0.1) wrote those files without any schema version. This
// package introduces a schema descriptor (schema_version.json) plus a small
// framework to back up, migrate, validate and roll back that state.
package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// SchemaVersion is the schema version this build writes and expects.
	SchemaVersion = 1
	// LegacyVersion is the version of pre-versioned data (no stamp file).
	LegacyVersion = 0
	// StampFile is the schema descriptor stored inside DataDir.
	StampFile = "schema_version.json"
	// BackupDirName is the default backup folder, created next to DataDir.
	BackupDirName = "migration_backups"
	// ManifestName is the file describing a backup inside its folder.
	ManifestName = "manifest.json"
)

// Stamp records the current schema version of a data directory.
type Stamp struct {
	Version     int       `json:"version"`
	PrevVersion int       `json:"previous_version"`
	UpdatedAt   time.Time `json:"updated_at"`
	AppVersion  string    `json:"app_version,omitempty"`
	Backup      string    `json:"backup,omitempty"`
}

// StoreSpec describes one known JSON store and the top-level keys a healthy
// file must contain.
type StoreSpec struct {
	File     string
	Required []string
}

// KnownStores is the authoritative list of persistent stores handled by the
// backup, migration and validation logic.
var KnownStores = []StoreSpec{
	{File: "devices.json", Required: []string{"devices", "codes"}},
	{File: "quota.json", Required: []string{"configs", "usage"}},
	{File: "tokens.json", Required: []string{"tokens", "blocked"}},
	{File: "shares.json", Required: []string{"shares"}},
}

// DetectVersion returns the schema version of dataDir. A missing or unreadable
// stamp means legacy (v0) data.
func DetectVersion(dataDir string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, StampFile))
	if err != nil {
		if os.IsNotExist(err) {
			return LegacyVersion, nil
		}
		return LegacyVersion, fmt.Errorf("read %s: %w", StampFile, err)
	}
	var s Stamp
	if err := json.Unmarshal(raw, &s); err != nil {
		return LegacyVersion, fmt.Errorf("parse %s: %w", StampFile, err)
	}
	if s.Version < LegacyVersion {
		return LegacyVersion, nil
	}
	return s.Version, nil
}

// writeStamp persists the schema descriptor.
func writeStamp(dataDir string, s Stamp) error {
	return writeJSONAtomic(filepath.Join(dataDir, StampFile), s)
}

package file_gateway

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PruneRetention deletes rotated audit_YYYY-MM-DD.log files older than the
// given number of days and reports how many were removed (模块 3-9). days <= 0
// disables pruning.
func (al *AuditLogger) PruneRetention(days int, now time.Time) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	dir := filepath.Dir(al.filePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	cutoff := now.AddDate(0, 0, -days)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "audit_") || !strings.HasSuffix(name, ".log") {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, "audit_"), ".log")
		t, perr := time.Parse("2006-01-02", day)
		if perr != nil {
			continue
		}
		if t.Before(cutoff) {
			if rerr := os.Remove(filepath.Join(dir, name)); rerr == nil {
				removed++
			}
		}
	}
	return removed, nil
}

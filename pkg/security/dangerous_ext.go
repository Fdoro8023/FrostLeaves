package security

import (
	"path/filepath"
	"strings"
)

// DangerousExt reports whether name contains a known dangerous executable or
// script segment. It is an extension/name-only check and does not inspect
// file content (see SniffDangerous for the content-aware variant).
func DangerousExt(name string) (string, bool) {
	lower := strings.ToLower(name)
	for _, seg := range containsBadExt {
		if strings.Contains(lower, seg) {
			return seg, true
		}
	}
	if ext := filepath.Ext(lower); ext != "" && dangerousExt[ext] {
		return ext, true
	}
	return "", false
}

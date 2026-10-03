package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Validate checks that every known store present in dataDir is valid JSON and
// contains its required top-level keys. Missing optional files are allowed.
func Validate(dataDir string) error {
	for _, spec := range KnownStores {
		path := filepath.Join(dataDir, spec.File)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("%s: %w", spec.File, err)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("%s: invalid JSON: %w", spec.File, err)
		}
		for _, key := range spec.Required {
			if _, ok := obj[key]; !ok {
				return fmt.Errorf("%s: missing required key %q", spec.File, key)
			}
		}
	}
	return nil
}

package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Store holds the three policy levels plus the device->account mapping and
// persists them to a single JSON file.
type Store struct {
	mu       sync.RWMutex
	global   Policy
	accounts map[string]*Override
	devices  map[string]*Override
	devAcct  map[string]string

	storePath string
	storeMu   sync.Mutex
}

type storeFile struct {
	Version        int                  `json:"version"`
	Global         Policy               `json:"global"`
	Accounts       map[string]*Override `json:"accounts,omitempty"`
	Devices        map[string]*Override `json:"devices,omitempty"`
	DeviceAccounts map[string]string    `json:"device_accounts,omitempty"`
}

const storeVersion = 1

// NewStore returns a store seeded with the safe global default.
func NewStore() *Store {
	return &Store{
		global:   DefaultPolicy(),
		accounts: map[string]*Override{},
		devices:  map[string]*Override{},
		devAcct:  map[string]string{},
	}
}

// SetStore sets the persistence path ("" disables persistence).
func (s *Store) SetStore(path string) { s.storePath = path }

// Global returns a copy of the global policy.
func (s *Store) Global() Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyPolicy(s.global)
}

// SetGlobal replaces the global policy.
func (s *Store) SetGlobal(p Policy) error {
	s.mu.Lock()
	s.global = copyPolicy(p)
	s.mu.Unlock()
	return s.save()
}

// SetAccountOverride sets or clears (nil) the override for an account.
func (s *Store) SetAccountOverride(accountID string, o *Override) error {
	if accountID == "" {
		return fmt.Errorf("policy: empty account id")
	}
	s.mu.Lock()
	if o == nil {
		delete(s.accounts, accountID)
	} else {
		s.accounts[accountID] = o
	}
	s.mu.Unlock()
	return s.save()
}

// SetDeviceOverride sets or clears (nil) the override for a device.
func (s *Store) SetDeviceOverride(deviceID string, o *Override) error {
	if deviceID == "" {
		return fmt.Errorf("policy: empty device id")
	}
	s.mu.Lock()
	if o == nil {
		delete(s.devices, deviceID)
	} else {
		s.devices[deviceID] = o
	}
	s.mu.Unlock()
	return s.save()
}

// SetDeviceAccount assigns a device to an account ("" unassigns).
func (s *Store) SetDeviceAccount(deviceID, accountID string) error {
	if deviceID == "" {
		return fmt.Errorf("policy: empty device id")
	}
	s.mu.Lock()
	if accountID == "" {
		delete(s.devAcct, deviceID)
	} else {
		s.devAcct[deviceID] = accountID
	}
	s.mu.Unlock()
	return s.save()
}

// AccountOf returns the account a device belongs to ("" when unassigned).
func (s *Store) AccountOf(deviceID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.devAcct[deviceID]
}

// AccountOverride returns a copy of the account override, or nil.
func (s *Store) AccountOverride(accountID string) *Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneOverride(s.accounts[accountID])
}

// DeviceOverride returns a copy of the device override, or nil.
func (s *Store) DeviceOverride(deviceID string) *Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneOverride(s.devices[deviceID])
}

// Accounts returns a copy of all account overrides.
func (s *Store) Accounts() map[string]*Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*Override, len(s.accounts))
	for k, v := range s.accounts {
		out[k] = cloneOverride(v)
	}
	return out
}

// Devices returns a copy of all device overrides.
func (s *Store) Devices() map[string]*Override {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*Override, len(s.devices))
	for k, v := range s.devices {
		out[k] = cloneOverride(v)
	}
	return out
}

// Resolve returns the effective policy for a device (device > account > global).
func (s *Store) Resolve(deviceID string) Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveLocked(s.devAcct[deviceID], deviceID)
}

// ResolveFor returns the effective policy for an explicit account/device pair.
func (s *Store) ResolveFor(accountID, deviceID string) Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resolveLocked(accountID, deviceID)
}

func (s *Store) resolveLocked(accountID, deviceID string) Policy {
	p := copyPolicy(s.global)
	if accountID != "" {
		s.accounts[accountID].apply(&p)
	}
	if deviceID != "" {
		s.devices[deviceID].apply(&p)
	}
	return p
}

// Load restores policy state from disk. A missing file is not an error.
func (s *Store) Load() error {
	if s.storePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("policy: read %s: %w", s.storePath, err)
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("policy: parse %s: %w", s.storePath, err)
	}
	s.mu.Lock()
	s.global = copyPolicy(f.Global)
	s.accounts = map[string]*Override{}
	s.devices = map[string]*Override{}
	s.devAcct = map[string]string{}
	for k, v := range f.Accounts {
		s.accounts[k] = v
	}
	for k, v := range f.Devices {
		s.devices[k] = v
	}
	for k, v := range f.DeviceAccounts {
		s.devAcct[k] = v
	}
	s.mu.Unlock()
	return nil
}

// Save persists current policy state to disk (atomic write).
func (s *Store) Save() error { return s.save() }

func (s *Store) save() error {
	if s.storePath == "" {
		return nil
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	s.mu.RLock()
	f := storeFile{
		Version:        storeVersion,
		Global:         copyPolicy(s.global),
		Accounts:       map[string]*Override{},
		Devices:        map[string]*Override{},
		DeviceAccounts: map[string]string{},
	}
	for k, v := range s.accounts {
		f.Accounts[k] = cloneOverride(v)
	}
	for k, v := range s.devices {
		f.Devices[k] = cloneOverride(v)
	}
	for k, v := range s.devAcct {
		f.DeviceAccounts[k] = v
	}
	s.mu.RUnlock()

	return writeJSONAtomic(s.storePath, f)
}

func copyPolicy(p Policy) Policy {
	p.AllowedExts = append([]string(nil), p.AllowedExts...)
	p.DeniedExts = append([]string(nil), p.DeniedExts...)
	return p
}

func cloneOverride(o *Override) *Override {
	if o == nil {
		return nil
	}
	cp := *o
	if o.AllowedExts != nil {
		v := append([]string(nil), (*o.AllowedExts)...)
		cp.AllowedExts = &v
	}
	if o.DeniedExts != nil {
		v := append([]string(nil), (*o.DeniedExts)...)
		cp.DeniedExts = &v
	}
	return &cp
}

func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

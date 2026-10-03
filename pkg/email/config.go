package email

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Config is the admin-configurable email registration configuration.
type Config struct {
	Enabled   bool   `json:"enabled"`
	Provider  string `json:"provider"` // preset key or "custom"
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Security  string `json:"security"` // "ssl" | "starttls" | "none"
	Username  string `json:"username"`
	Password  string `json:"password"`
	FromName  string `json:"from_name"`
	FromEmail string `json:"from_email"`

	SubjectTemplate string `json:"subject_template"`
	BodyTemplate    string `json:"body_template"`
	CodeLength      int    `json:"code_length"`
	CodeTTLMinutes  int    `json:"code_ttl_minutes"`

	EmailPerMinute int `json:"email_per_minute"`
	EmailPerHour   int `json:"email_per_hour"`
	IPPerMinute    int `json:"ip_per_minute"`
}

// DefaultConfig returns safe defaults with registration disabled.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		Provider:        "custom",
		Security:        "ssl",
		SubjectTemplate: "【FrostLeaves】邮箱验证码",
		BodyTemplate:    "您的验证码是 {code}，{ttl} 分钟内有效。请勿泄露给他人。",
		CodeLength:      6,
		CodeTTLMinutes:  10,
		EmailPerMinute:  1,
		EmailPerHour:    5,
		IPPerMinute:     3,
	}
}

// ApplyPreset fills host/port/security from a preset key.
func (c *Config) ApplyPreset(key string) bool {
	p, ok := LookupPreset(key)
	if !ok {
		return false
	}
	c.Provider = p.Key
	c.Host = p.Host
	c.Port = p.Port
	c.Security = p.Security
	return true
}

// Validate reports whether the transport settings are usable.
func (c Config) Validate() error {
	if c.Host == "" || c.Port <= 0 {
		return fmt.Errorf("email: smtp host and port are required")
	}
	switch c.Security {
	case "ssl", "starttls", "none":
	default:
		return fmt.Errorf("email: unknown security mode %q", c.Security)
	}
	if c.FromEmail == "" {
		return fmt.Errorf("email: from_email is required")
	}
	if c.CodeLength < 4 || c.CodeLength > 10 {
		return fmt.Errorf("email: code_length must be between 4 and 10")
	}
	if c.CodeTTLMinutes <= 0 {
		return fmt.Errorf("email: code_ttl_minutes must be positive")
	}
	return nil
}

// Redacted returns a copy with the password removed (for admin GET).
func (c Config) Redacted() Config {
	if c.Password != "" {
		c.Password = ""
	}
	return c
}

// Store persists the email configuration to a JSON file.
type Store struct {
	mu        sync.RWMutex
	cfg       Config
	storePath string
	storeMu   sync.Mutex
}

// NewStore returns a store seeded with DefaultConfig.
func NewStore() *Store { return &Store{cfg: DefaultConfig()} }

// SetStore sets the persistence path ("" disables persistence).
func (s *Store) SetStore(path string) { s.storePath = path }

// Get returns a copy of the current configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Set replaces the configuration and persists it.
func (s *Store) Set(c Config) error {
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
	return s.Save()
}

// Load restores configuration from disk. A missing file is not an error.
func (s *Store) Load() error {
	if s.storePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("email: read %s: %w", s.storePath, err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("email: parse %s: %w", s.storePath, err)
	}
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
	return nil
}

// Save persists the configuration atomically.
func (s *Store) Save() error {
	if s.storePath == "" {
		return nil
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.storePath), 0o755); err != nil {
		return err
	}
	tmp := s.storePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.storePath)
}

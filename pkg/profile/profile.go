// Package profile implements client/server personalization (模块 4): per-device
// display names and avatars, plus the server name and avatar shown on every
// client home page. Avatar images are stored on the Windows server.
package profile

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"frostleaves/pkg/security"
)

// ServerOwner is the avatar owner key used for the server avatar.
const ServerOwner = "__server__"

// MaxAvatarBytes caps avatar size for lightweight uploads (512 KiB).
const MaxAvatarBytes = 512 * 1024

// allowedAvatarTypes maps the detected MIME type to a file extension.
var allowedAvatarTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// DeviceProfile is a device's personalization.
type DeviceProfile struct {
	DeviceID    string    `json:"device_id"`
	DisplayName string    `json:"display_name,omitempty"`
	AvatarFile  string    `json:"avatar_file,omitempty"`
	AvatarType  string    `json:"avatar_type,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ServerProfile is the server's personalization.
type ServerProfile struct {
	Name       string    `json:"name,omitempty"`
	AvatarFile string    `json:"avatar_file,omitempty"`
	AvatarType string    `json:"avatar_type,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	Revision   int64     `json:"revision"`
}

// Store holds personalization state and persists it to JSON.
type Store struct {
	mu        sync.RWMutex
	root      string // avatar directory
	devices   map[string]*DeviceProfile
	server    ServerProfile
	storePath string
	storeMu   sync.Mutex
}

type storeFile struct {
	Version int                       `json:"version"`
	Devices map[string]*DeviceProfile `json:"devices,omitempty"`
	Server  ServerProfile             `json:"server"`
}

// NewStore returns a store writing avatars under avatarRoot.
func NewStore(avatarRoot string) *Store {
	return &Store{root: avatarRoot, devices: map[string]*DeviceProfile{}}
}

// SetStore sets the JSON persistence path ("" disables persistence).
func (s *Store) SetStore(path string) { s.storePath = path }

// validOwner rejects unsafe owner keys.
func validOwner(id string) bool {
	if id == "" || security.HasDotDot(id) {
		return false
	}
	return id == filepath.Base(id)
}

// Device returns a copy of a device profile (zero value when unknown).
func (s *Store) Device(deviceID string) DeviceProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.devices[deviceID]; ok && p != nil {
		return *p
	}
	return DeviceProfile{DeviceID: deviceID}
}

// SetDeviceName stores a device display name.
func (s *Store) SetDeviceName(deviceID, name string) error {
	if !validOwner(deviceID) {
		return fmt.Errorf("profile: invalid device id")
	}
	name = security.CleanInput(name, 64)
	s.mu.Lock()
	p := s.deviceLocked(deviceID)
	p.DisplayName = name
	p.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	return s.Save()
}

// Server returns a copy of the server profile.
func (s *Store) Server() ServerProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server
}

// SetServerName stores the server display name.
func (s *Store) SetServerName(name string) error {
	name = security.CleanInput(name, 64)
	s.mu.Lock()
	s.server.Name = name
	s.server.UpdatedAt = time.Now().UTC()
	s.server.Revision++
	s.mu.Unlock()
	return s.Save()
}

// SetDeviceAvatar validates and stores a device avatar, returning its URL path.
func (s *Store) SetDeviceAvatar(deviceID string, data []byte) (string, error) {
	if !validOwner(deviceID) {
		return "", fmt.Errorf("profile: invalid device id")
	}
	file, mime, err := s.writeAvatar(deviceID, data)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	p := s.deviceLocked(deviceID)
	old := p.AvatarFile
	p.AvatarFile = file
	p.AvatarType = mime
	p.UpdatedAt = time.Now().UTC()
	s.mu.Unlock()
	if old != "" && old != file {
		_ = os.Remove(filepath.Join(s.root, old))
	}
	if err := s.Save(); err != nil {
		return "", err
	}
	return "/api/v1/avatars/device/" + deviceID, nil
}

// SetServerAvatar validates and stores the server avatar.
func (s *Store) SetServerAvatar(data []byte) (string, error) {
	file, mime, err := s.writeAvatar(ServerOwner, data)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	old := s.server.AvatarFile
	s.server.AvatarFile = file
	s.server.AvatarType = mime
	s.server.UpdatedAt = time.Now().UTC()
	s.server.Revision++
	s.mu.Unlock()
	if old != "" && old != file {
		_ = os.Remove(filepath.Join(s.root, old))
	}
	if err := s.Save(); err != nil {
		return "", err
	}
	return "/api/v1/avatars/server", nil
}

// AvatarData returns the stored avatar bytes and MIME type for an owner.
func (s *Store) AvatarData(owner string) ([]byte, string, bool) {
	var file, mime string
	s.mu.RLock()
	switch owner {
	case ServerOwner:
		file, mime = s.server.AvatarFile, s.server.AvatarType
	default:
		if p, ok := s.devices[owner]; ok && p != nil {
			file, mime = p.AvatarFile, p.AvatarType
		}
	}
	s.mu.RUnlock()
	if file == "" {
		return nil, "", false
	}
	data, err := os.ReadFile(filepath.Join(s.root, file))
	if err != nil {
		return nil, "", false
	}
	return data, mime, true
}

func (s *Store) deviceLocked(deviceID string) *DeviceProfile {
	p, ok := s.devices[deviceID]
	if !ok || p == nil {
		p = &DeviceProfile{DeviceID: deviceID}
		s.devices[deviceID] = p
	}
	return p
}

// writeAvatar validates the payload (lightweight + image only) and stores it.
func (s *Store) writeAvatar(owner string, data []byte) (string, string, error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("profile: empty avatar")
	}
	if len(data) > MaxAvatarBytes {
		return "", "", fmt.Errorf("profile: avatar exceeds %d bytes", MaxAvatarBytes)
	}
	mime := http.DetectContentType(data)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	ext, ok := allowedAvatarTypes[mime]
	if !ok {
		return "", "", fmt.Errorf("profile: unsupported avatar type %q", mime)
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return "", "", err
	}
	file := owner + ext
	if err := os.WriteFile(filepath.Join(s.root, file), data, 0o644); err != nil {
		return "", "", err
	}
	return file, mime, nil
}

// Load restores personalization from disk.
func (s *Store) Load() error {
	if s.storePath == "" {
		return nil
	}
	raw, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("profile: read %s: %w", s.storePath, err)
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return fmt.Errorf("profile: parse %s: %w", s.storePath, err)
	}
	s.mu.Lock()
	s.devices = map[string]*DeviceProfile{}
	for k, v := range f.Devices {
		s.devices[k] = v
	}
	s.server = f.Server
	s.mu.Unlock()
	return nil
}

// Save persists personalization atomically.
func (s *Store) Save() error {
	if s.storePath == "" {
		return nil
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()
	s.mu.RLock()
	f := storeFile{Version: 1, Devices: map[string]*DeviceProfile{}, Server: s.server}
	for k, v := range s.devices {
		cp := *v
		f.Devices[k] = &cp
	}
	s.mu.RUnlock()

	b, err := json.MarshalIndent(f, "", "  ")
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

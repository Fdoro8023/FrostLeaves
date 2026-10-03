package profile

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 200, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	s := NewStore(filepath.Join(root, "avatars"))
	s.SetStore(filepath.Join(root, "profiles.json"))
	return s, root
}

func TestDeviceNameRoundTrip(t *testing.T) {
	s, root := newTestStore(t)
	if err := s.SetDeviceName("dev1", "  客厅 PC  "); err != nil {
		t.Fatal(err)
	}
	if got := s.Device("dev1").DisplayName; got != "客厅 PC" {
		t.Fatalf("display name = %q", got)
	}
	s2 := NewStore(filepath.Join(root, "avatars"))
	s2.SetStore(filepath.Join(root, "profiles.json"))
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := s2.Device("dev1").DisplayName; got != "客厅 PC" {
		t.Fatalf("persisted display name = %q", got)
	}
}

func TestServerNameAndRevision(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.SetServerName("我的私有网盘"); err != nil {
		t.Fatal(err)
	}
	first := s.Server()
	if first.Name != "我的私有网盘" {
		t.Fatalf("server name = %q", first.Name)
	}
	if err := s.SetServerName("改个名"); err != nil {
		t.Fatal(err)
	}
	if s.Server().Revision <= first.Revision {
		t.Fatal("revision must increase on change")
	}
}

func TestDeviceAvatarStoredAndServed(t *testing.T) {
	s, root := newTestStore(t)
	png := makePNG(t, 8, 8)
	url, err := s.SetDeviceAvatar("dev1", png)
	if err != nil {
		t.Fatalf("SetDeviceAvatar: %v", err)
	}
	if url != "/api/v1/avatars/device/dev1" {
		t.Fatalf("avatar url = %q", url)
	}
	data, mime, ok := s.AvatarData("dev1")
	if !ok {
		t.Fatal("avatar should be retrievable")
	}
	if mime != "image/png" {
		t.Fatalf("mime = %q", mime)
	}
	if !bytes.Equal(data, png) {
		t.Fatal("stored avatar bytes differ")
	}
	if _, err := os.Stat(filepath.Join(root, "avatars", "dev1.png")); err != nil {
		t.Fatalf("avatar file missing on disk: %v", err)
	}
}

func TestServerAvatarStored(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.SetServerAvatar(makePNG(t, 4, 4)); err != nil {
		t.Fatal(err)
	}
	if _, mime, ok := s.AvatarData(ServerOwner); !ok || mime != "image/png" {
		t.Fatalf("server avatar missing: ok=%v mime=%q", ok, mime)
	}
}

func TestAvatarRejectsOversize(t *testing.T) {
	s, _ := newTestStore(t)
	big := make([]byte, MaxAvatarBytes+1)
	copy(big, makePNG(t, 2, 2))
	if _, err := s.SetDeviceAvatar("dev1", big); err == nil {
		t.Fatal("oversize avatar must be rejected")
	}
}

func TestAvatarRejectsNonImage(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.SetDeviceAvatar("dev1", []byte("this is not an image at all")); err == nil {
		t.Fatal("non-image payload must be rejected")
	}
	if _, err := s.SetServerAvatar([]byte{0x4D, 0x5A, 0x90, 0x00}); err == nil {
		t.Fatal("executable payload must be rejected as an avatar")
	}
}

func TestInvalidOwnerRejected(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.SetDeviceName("../evil", "x"); err == nil {
		t.Fatal("unsafe device id must be rejected")
	}
	if _, err := s.SetDeviceAvatar("..\\evil", makePNG(t, 2, 2)); err == nil {
		t.Fatal("unsafe device id must be rejected for avatars")
	}
}

func TestReplacingAvatarRemovesOldFile(t *testing.T) {
	s, root := newTestStore(t)
	if _, err := s.SetDeviceAvatar("dev1", makePNG(t, 2, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetDeviceAvatar("dev1", makePNG(t, 16, 16)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "avatars"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one avatar file, got %d", len(entries))
	}
}

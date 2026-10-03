package policy

import (
	"path/filepath"
	"testing"
)

func ptrInt64(v int64) *int64       { return &v }
func ptrInt(v int) *int             { return &v }
func ptrBool(v bool) *bool          { return &v }
func ptrStrs(v ...string) *[]string { return &v }

func TestResolvePriorityDeviceOverAccountOverGlobal(t *testing.T) {
	s := NewStore()
	g := DefaultPolicy()
	g.MaxFileSizeBytes = 1000
	if err := s.SetGlobal(g); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountOverride("acct1", &Override{MaxFileSizeBytes: ptrInt64(2000)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceOverride("dev1", &Override{MaxFileSizeBytes: ptrInt64(3000)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceAccount("dev1", "acct1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceAccount("dev2", "acct1"); err != nil {
		t.Fatal(err)
	}

	if got := s.Resolve("dev1").MaxFileSizeBytes; got != 3000 {
		t.Fatalf("device must win: got %d", got)
	}
	if got := s.Resolve("dev2").MaxFileSizeBytes; got != 2000 {
		t.Fatalf("account must apply when no device override: got %d", got)
	}
	if got := s.Resolve("dev9").MaxFileSizeBytes; got != 1000 {
		t.Fatalf("global must apply when nothing else set: got %d", got)
	}
}

func TestOpTogglesAreEnforcedServerSide(t *testing.T) {
	s := NewStore()
	if err := s.AuthorizeOp("d1", OpUpload); err != nil {
		t.Fatalf("upload should be allowed by default: %v", err)
	}
	if err := s.SetDeviceOverride("d1", &Override{AllowUpload: ptrBool(false), AllowDelete: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeOp("d1", OpUpload); err == nil {
		t.Fatal("upload must be denied when disabled")
	}
	if err := s.AuthorizeOp("d1", OpDelete); err == nil {
		t.Fatal("delete must be denied when disabled")
	}
	if err := s.AuthorizeOp("d1", OpDownload); err != nil {
		t.Fatalf("download should still be allowed: %v", err)
	}
}

func TestMagicBlocksExecutableDisguisedAsImage(t *testing.T) {
	s := NewStore()
	mz := []byte{0x4D, 0x5A, 0x90, 0x00} // MZ = Windows PE
	err := s.AuthorizeUpload("d1", UploadRequest{Name: "photo.png", Size: 4, Head: mz})
	if err == nil {
		t.Fatal("MZ payload disguised as .png must be blocked")
	}
	png := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "photo.png", Size: 6, Head: png}); err != nil {
		t.Fatalf("genuine png must pass: %v", err)
	}
}

func TestExeBlockedByDefaultAndAllowedForDeveloperDevice(t *testing.T) {
	s := NewStore()
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "setup.exe"}); err == nil {
		t.Fatal("exe must be blocked by default")
	}
	if err := s.SetDeviceOverride("d1", &Override{AllowExecutable: ptrBool(true)}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "setup.exe"}); err != nil {
		t.Fatalf("developer device should upload exe: %v", err)
	}
}

func TestMagicCheckSwitchOffStillBlocksExeByExtension(t *testing.T) {
	s := NewStore()
	g := DefaultPolicy()
	g.MagicCheck = false
	if err := s.SetGlobal(g); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "tool.exe"}); err == nil {
		t.Fatal("exe extension must still be blocked when magic check is off")
	}
	mz := []byte{0x4D, 0x5A}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "photo.png", Head: mz}); err != nil {
		t.Fatalf("content check is off, so .png + MZ should pass: %v", err)
	}
}

func TestQuotaSizeAndCountLimits(t *testing.T) {
	s := NewStore()
	if err := s.SetDeviceOverride("d1", &Override{
		StorageQuotaBytes: ptrInt64(100),
		MaxFileSizeBytes:  ptrInt64(50),
		MaxFileCount:      ptrInt64(2),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.txt", Size: 60}); err == nil {
		t.Fatal("single file size limit must reject 60 > 50")
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.txt", Size: 10, UsedBytes: 95}); err == nil {
		t.Fatal("storage quota must reject 95+10 > 100")
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.txt", Size: 10, FileCount: 2}); err == nil {
		t.Fatal("file count limit must reject at 2/2")
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.txt", Size: 10}); err != nil {
		t.Fatalf("within limits must pass: %v", err)
	}
}

func TestExtAllowAndDenyLists(t *testing.T) {
	s := NewStore()
	if err := s.SetDeviceOverride("d1", &Override{AllowedExts: ptrStrs(".txt", ".md")}); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.png"}); err == nil {
		t.Fatal("ext outside allow list must be rejected")
	}
	if err := s.AuthorizeUpload("d1", UploadRequest{Name: "a.txt"}); err != nil {
		t.Fatalf("allowed ext must pass: %v", err)
	}

	s2 := NewStore()
	if err := s2.SetDeviceOverride("d1", &Override{DeniedExts: ptrStrs(".zip")}); err != nil {
		t.Fatal(err)
	}
	if err := s2.AuthorizeUpload("d1", UploadRequest{Name: "a.zip"}); err == nil {
		t.Fatal("deny-listed ext must be rejected")
	}
}

func TestSharePermissionPerAccount(t *testing.T) {
	s := NewStore()
	if err := s.AuthorizeOp("d1", OpShare); err != nil {
		t.Fatalf("share allowed by default: %v", err)
	}
	if err := s.SetAccountOverride("acct", &Override{AllowShare: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceAccount("d1", "acct"); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeOp("d1", OpShare); err == nil {
		t.Fatal("share must be denied for the account")
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policies.json")
	s := NewStore()
	s.SetStore(path)
	g := DefaultPolicy()
	g.AuditRetentionDays = 5
	if err := s.SetGlobal(g); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountOverride("acct", &Override{MaxFileCount: ptrInt64(7)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceOverride("d1", &Override{AllowUpload: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceAccount("d1", "acct"); err != nil {
		t.Fatal(err)
	}

	s2 := NewStore()
	s2.SetStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := s2.Resolve("d1")
	if got.MaxFileCount != 7 {
		t.Fatalf("account override lost: %d", got.MaxFileCount)
	}
	if got.AllowUpload {
		t.Fatal("device override lost")
	}
	if got.AuditRetentionDays != 5 {
		t.Fatalf("global lost: %d", got.AuditRetentionDays)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	s := NewStore()
	s.SetStore(filepath.Join(t.TempDir(), "nope.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !s.Global().MagicCheck {
		t.Fatal("default global must keep magic check on")
	}
}

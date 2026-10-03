package file_gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"frostleaves/pkg/policy"
)

func doJSON(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Device-Id", "dev1")
	req.Header.Set("X-Device-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

type recycleListResp struct {
	Code int `json:"code"`
	Data struct {
		Total int `json:"total"`
		Items []struct {
			ID    string `json:"id"`
			Size  int64  `json:"size"`
			IsDir bool   `json:"is_directory"`
		} `json:"items"`
	} `json:"data"`
}

func listRecycle(t *testing.T, base, token string) recycleListResp {
	t.Helper()
	code, body := doJSON(t, http.MethodGet, base+"/api/v1/recycle/list", token, nil)
	if code != 200 {
		t.Fatalf("recycle list status %d: %s", code, body)
	}
	var out recycleListResp
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("parse list: %v (%s)", err, body)
	}
	return out
}

func TestDeleteMovesFileToRecycleNotDisk(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()

	if code := doUpload(t, srv.URL, token, "private/note.txt", "note.txt", []byte("keep me")); code >= 300 {
		t.Fatalf("upload failed: %d", code)
	}
	onDisk := filepath.Join(gw.config.StorageRoot, "devices", "dev1", "private", "note.txt")
	if _, err := os.Stat(onDisk); err != nil {
		t.Fatalf("file should exist before delete: %v", err)
	}

	code, body := doJSON(t, http.MethodDelete, srv.URL+"/api/v1/files/delete", token, map[string]any{"paths": []string{"private/note.txt"}})
	if code != 200 {
		t.Fatalf("delete status %d: %s", code, body)
	}
	if _, err := os.Stat(onDisk); !os.IsNotExist(err) {
		t.Fatalf("deleted file must not remain on disk, err=%v", err)
	}
	lst := listRecycle(t, srv.URL, token)
	if lst.Data.Total != 1 {
		t.Fatalf("recycle should hold 1 item, got %d", lst.Data.Total)
	}
}

func TestRecycleRestoreToOriginalPath(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()

	content := []byte("restore me")
	if code := doUpload(t, srv.URL, token, "private/doc.txt", "doc.txt", content); code >= 300 {
		t.Fatalf("upload failed: %d", code)
	}
	if code, body := doJSON(t, http.MethodDelete, srv.URL+"/api/v1/files/delete", token, map[string]any{"paths": []string{"private/doc.txt"}}); code != 200 {
		t.Fatalf("delete status %d: %s", code, body)
	}

	lst := listRecycle(t, srv.URL, token)
	if len(lst.Data.Items) != 1 {
		t.Fatalf("expected 1 recycle item, got %d", len(lst.Data.Items))
	}
	id := lst.Data.Items[0].ID

	if code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/recycle/restore", token, map[string]any{"id": id}); code != 200 {
		t.Fatalf("restore status %d: %s", code, body)
	}
	back := filepath.Join(gw.config.StorageRoot, "devices", "dev1", "private", "doc.txt")
	got, err := os.ReadFile(back)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("restored content mismatch: %q", got)
	}
	if lst2 := listRecycle(t, srv.URL, token); lst2.Data.Total != 0 {
		t.Fatalf("recycle should be empty after restore, got %d", lst2.Data.Total)
	}
}

func TestRecyclePermanentDelete(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()

	if code := doUpload(t, srv.URL, token, "private/trash.txt", "trash.txt", []byte("bye")); code >= 300 {
		t.Fatalf("upload failed: %d", code)
	}
	if code, _ := doJSON(t, http.MethodDelete, srv.URL+"/api/v1/files/delete", token, map[string]any{"paths": []string{"private/trash.txt"}}); code != 200 {
		t.Fatalf("delete failed: %d", code)
	}

	lst := listRecycle(t, srv.URL, token)
	if len(lst.Data.Items) != 1 {
		t.Fatalf("expected 1 recycle item, got %d", len(lst.Data.Items))
	}
	id := lst.Data.Items[0].ID
	recyclePath := filepath.Join(gw.recycleDeviceDir("dev1"), id)

	if code, body := doJSON(t, http.MethodDelete, srv.URL+"/api/v1/recycle/delete", token, map[string]any{"ids": []string{id}}); code != 200 {
		t.Fatalf("permanent delete status %d: %s", code, body)
	}
	if _, err := os.Stat(recyclePath); !os.IsNotExist(err) {
		t.Fatalf("permanent delete must remove the file from disk, err=%v", err)
	}
	if _, err := os.Stat(recyclePath + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("metadata should be removed too, err=%v", err)
	}
	if lst2 := listRecycle(t, srv.URL, token); lst2.Data.Total != 0 {
		t.Fatalf("recycle should be empty, got %d", lst2.Data.Total)
	}
}

func TestRecycleDirectoryListedAndRestored(t *testing.T) {
	gw, _, _, token := newTestGateway(t)
	srv := httptest.NewServer(gw.mux)
	defer srv.Close()

	dirOnDisk := filepath.Join(gw.config.StorageRoot, "devices", "dev1", "private", "folder")
	if err := os.MkdirAll(dirOnDisk, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirOnDisk, "inner.txt"), []byte("inner-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, body := doJSON(t, http.MethodDelete, srv.URL+"/api/v1/files/delete", token, map[string]any{"paths": []string{"private/folder"}}); code != 200 {
		t.Fatalf("delete dir status %d: %s", code, body)
	}
	if _, err := os.Stat(dirOnDisk); !os.IsNotExist(err) {
		t.Fatalf("directory must be moved off its original location, err=%v", err)
	}

	lst := listRecycle(t, srv.URL, token)
	if len(lst.Data.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(lst.Data.Items))
	}
	if !lst.Data.Items[0].IsDir {
		t.Fatal("recycled directory must be reported as a directory")
	}
	if lst.Data.Items[0].Size <= 0 {
		t.Fatal("recycled directory size must include nested files")
	}

	id := lst.Data.Items[0].ID
	if code, body := doJSON(t, http.MethodPost, srv.URL+"/api/v1/recycle/restore", token, map[string]any{"id": id}); code != 200 {
		t.Fatalf("restore dir status %d: %s", code, body)
	}
	inner, err := os.ReadFile(filepath.Join(dirOnDisk, "inner.txt"))
	if err != nil {
		t.Fatalf("nested file not restored: %v", err)
	}
	if string(inner) != "inner-content" {
		t.Fatalf("nested content mismatch: %q", inner)
	}
}

func TestRecycleRetentionCleanup(t *testing.T) {
	gw, pol, _, _ := newTestGateway(t)
	dir := gw.recycleDeviceDir("dev1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldItem := filepath.Join(dir, "20260101_000000_old.txt")
	freshItem := filepath.Join(dir, "20261002_000000_new.txt")
	for _, p := range []string{oldItem, freshItem} {
		if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p+".meta", []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().AddDate(0, 0, -10)
	_ = os.Chtimes(oldItem, old, old)
	_ = os.Chtimes(oldItem+".meta", old, old)

	p := policy.DefaultPolicy()
	p.RecycleBinRetentionDays = 3
	if err := pol.SetGlobal(p); err != nil {
		t.Fatal(err)
	}
	if got := gw.recycleRetentionDays(); got != 3 {
		t.Fatalf("retention should be 3, got %d", got)
	}

	gw.cleanupExpiredRecycleBin()

	if _, err := os.Stat(oldItem); !os.IsNotExist(err) {
		t.Fatalf("expired item must be removed, err=%v", err)
	}
	if _, err := os.Stat(oldItem + ".meta"); !os.IsNotExist(err) {
		t.Fatalf("expired metadata must be removed, err=%v", err)
	}
	if _, err := os.Stat(freshItem); err != nil {
		t.Fatalf("fresh item must survive: %v", err)
	}
}

func TestRecycleCountsTowardsQuotaWhenEnabled(t *testing.T) {
	gw, pol, _, _ := newTestGateway(t)
	dir := gw.recycleDeviceDir("dev1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), make([]byte, 256), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := gw.deviceUsageBytes("dev1"); got < 256 {
		t.Fatalf("recycle usage should count towards quota by default, got %d", got)
	}
	if err := pol.SetDeviceOverride("dev1", &policy.Override{RecycleBinCountsQuota: fpBool(false)}); err != nil {
		t.Fatal(err)
	}
	if got := gw.deviceUsageBytes("dev1"); got != 0 {
		t.Fatalf("recycle usage must be excluded when the switch is off, got %d", got)
	}
}

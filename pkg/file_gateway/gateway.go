// Package file_gateway implements the internal HTTP file gateway.
// It handles file upload/download/list/delete with permission checks,
// quota enforcement, rate limiting, and audit logging.
package file_gateway

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"frostleaves/pkg/security"
)

// gwLimiter 文件接口限流（需求 7）：按来源 IP，约 3 req/s、突发 60
var gwLimiter = security.NewRateLimiter(3, 60)

// ========== Configuration ==========

type GatewayConfig struct {
	BindAddr          string `json:"bind_addr" yaml:"bind_addr"`
	Port              int    `json:"port" yaml:"port"`
	StorageRoot       string `json:"storage_root" yaml:"storage_root"`
	PublicSharedDir   string `json:"public_shared_dir" yaml:"public_shared_dir"`
	MaxFileSizeBytes  int64  `json:"max_file_size_bytes" yaml:"max_file_size_bytes"`
	UploadSpeedKBPS   int64  `json:"upload_speed_kbps" yaml:"upload_speed_kbps"`
	DownloadSpeedKBPS int64  `json:"download_speed_kbps" yaml:"download_speed_kbps"`
	EnableTLS         bool   `json:"enable_tls" yaml:"enable_tls"`
	TLSCertPath       string `json:"tls_cert_path" yaml:"tls_cert_path"`
	TLSKeyPath        string `json:"tls_key_path" yaml:"tls_key_path"`
}

// ========== Auth Context ==========

type AuthContext struct {
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	Platform   string `json:"platform"`
	Role       string `json:"role"` // "client" or "admin"
	Token      string `json:"-"`
}

// ========== File Gateway ==========

type FileGateway struct {
	config      GatewayConfig
	quota       *QuotaManager
	audit       *AuditLogger
	mux         *http.ServeMux
	auth        *Authenticator
	shares      *ShareStore
	adminSecret string
}

func NewFileGateway(cfg GatewayConfig, quota *QuotaManager, audit *AuditLogger, auth *Authenticator) *FileGateway {
	gw := &FileGateway{
		config: cfg,
		quota:  quota,
		audit:  audit,
		auth:   auth,
		mux:    http.NewServeMux(),
	}
	gw.setupRoutes()
	return gw
}

// SetShareStore 注入分享存储（由 main 统一创建并持久化）
func (gw *FileGateway) SetShareStore(s *ShareStore) { gw.shares = s }

// ========== 管理员本机通道（服务端模式 GUI 使用，需求 3） ==========

// AdminDeviceID 管理通道使用的伪设备 ID
const AdminDeviceID = "__admin__"

// SetAdminSecret 注入管理密钥（与 config.admin_secret 一致）
func (gw *FileGateway) SetAdminSecret(secret string) { gw.adminSecret = secret }

// isLoopbackAddr 判断远端地址是否为本机回环
func isLoopbackAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// adminAuthorized 校验管理员通道：必须来自回环 + 密钥匹配（恒定时间比较）
func (gw *FileGateway) adminAuthorized(r *http.Request) bool {
	if gw.adminSecret == "" || !isLoopbackAddr(r.RemoteAddr) {
		return false
	}
	token := r.Header.Get("X-Admin-Token")
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(gw.adminSecret)) == 1
}

// Shares 返回分享存储
func (gw *FileGateway) Shares() *ShareStore { return gw.shares }

func (gw *FileGateway) setupRoutes() {
	gw.mux.HandleFunc("/api/v1/files/list", gw.authMiddleware(gw.handleList))
	gw.mux.HandleFunc("/api/v1/files/upload", gw.authMiddleware(gw.handleUpload))
	gw.mux.HandleFunc("/api/v1/files/download", gw.authMiddleware(gw.handleDownload))
	gw.mux.HandleFunc("/api/v1/files/delete", gw.authMiddleware(gw.handleDelete))
	gw.mux.HandleFunc("/api/v1/files/rename", gw.authMiddleware(gw.handleRename))
	gw.mux.HandleFunc("/api/v1/files/mkdir", gw.authMiddleware(gw.handleMkdir))

	// Recycle bin routes
	gw.mux.HandleFunc("/api/v1/recycle/list", gw.authMiddleware(gw.handleRecycleList))
	gw.mux.HandleFunc("/api/v1/recycle/restore", gw.authMiddleware(gw.handleRecycleRestore))
	gw.mux.HandleFunc("/api/v1/recycle/delete", gw.authMiddleware(gw.handleRecycleDelete))
	gw.mux.HandleFunc("/api/v1/recycle/clear", gw.authMiddleware(gw.handleRecycleClear))

	// 访客分享（免设备 token，靠分享码校验；实现见 share.go）
	gw.mux.HandleFunc("/api/v1/share/", gw.handleShareVisitor)
}

func (gw *FileGateway) Start() error {
	addr := fmt.Sprintf("%s:%d", gw.config.BindAddr, gw.config.Port)

	// Ensure storage root exists
	os.MkdirAll(gw.config.StorageRoot, 0755)
	os.MkdirAll(filepath.Join(gw.config.StorageRoot, gw.config.PublicSharedDir), 0755)

	server := &http.Server{
		Handler:      security.LogFailures("Files", gw.corsMiddleware(gw.mux)),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 600 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("[FileGateway] Starting on %s (storage: %s)", addr, gw.config.StorageRoot)

	// Start recycle bin cleanup (daily)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			log.Printf("[FileGateway] Running recycle bin cleanup...")
			gw.cleanupExpiredRecycleBin()
		}
	}()

	// Create listener to allow port reuse
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	if gw.config.EnableTLS {
		// 回环保留 HTTP：桌面端管理通道与网页分享反代都走 127.0.0.1
		if loopLn, lerr := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", gw.config.Port)); lerr == nil {
			go func() {
				_ = server.Serve(loopLn)
			}()
		}
		return server.ServeTLS(listener, gw.config.TLSCertPath, gw.config.TLSKeyPath)
	}
	return server.Serve(listener)
}

// ========== Middleware ==========

func (gw *FileGateway) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Device-Id, X-Device-Token, X-Admin-Token")
		if r.Method == "OPTIONS" {
			w.WriteHeader(200)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (gw *FileGateway) authMiddleware(handler func(http.ResponseWriter, *http.Request, *AuthContext)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 需求 7：接口限流（按来源 IP，先于鉴权，抵御暴力请求/爬虫）
		if !gwLimiter.Allow(security.ClientIP(r)) {
			gw.audit.Log("", "rate_limited", r.URL.Path, "denied", "too many requests", r.RemoteAddr)
			writeJSON(w, 429, map[string]string{"error": "too many requests"})
			return
		}

		// 管理员本机通道：回环 + X-Admin-Token（服务端模式的管理界面使用）
		if gw.adminAuthorized(r) {
			handler(w, r, &AuthContext{
				DeviceID:   AdminDeviceID,
				DeviceName: "admin",
				Platform:   "windows",
				Role:       "admin",
			})
			return
		}

		deviceID := r.Header.Get("X-Device-Id")
		token := r.Header.Get("X-Device-Token")

		if deviceID == "" || token == "" {
			writeJSON(w, 401, map[string]string{"error": "missing credentials"})
			return
		}

		// Validate token
		ctx, err := gw.auth.Validate(deviceID, token)
		if err != nil {
			writeJSON(w, 401, map[string]string{"error": err.Error()})
			return
		}

		handler(w, r, ctx)
	}
}

// ========== Handlers ==========

// handleList lists files in a directory
func (gw *FileGateway) handleList(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = "/"
	}

	// Resolve to filesystem path
	fsPath := gw.resolvePath(ctx.DeviceID, reqPath)
	if fsPath == "" {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	// Check if path exists
	info, statErr := os.Stat(fsPath)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			// Path doesn't exist - check if parent exists
			parentDir := filepath.Dir(fsPath)
			if _, err := os.Stat(parentDir); os.IsNotExist(err) {
				// Parent doesn't exist either, return empty list
				writeJSON(w, 200, map[string]interface{}{"items": []interface{}{}, "path": reqPath})
				return
			}
			// Parent exists but this path doesn't - it might be a file that doesn't exist yet
			// Return empty list instead of creating a directory
			writeJSON(w, 200, map[string]interface{}{"items": []interface{}{}, "path": reqPath})
			return
		}
		writeJSON(w, 500, map[string]string{"error": statErr.Error()})
		return
	}

	// If it's a file (not a directory), return it as a single-item list
	if !info.IsDir() {
		item := map[string]interface{}{
			"name":     info.Name(),
			"size":     info.Size(),
			"type":     "file",
			"modified": info.ModTime().Local().Format("2006-01-02 15:04:05"),
		}
		writeJSON(w, 200, map[string]interface{}{"items": []interface{}{item}, "path": reqPath})
		return
	}

	entries, err := os.ReadDir(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 404, map[string]string{"error": "directory not found"})
		} else {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
		}
		return
	}

	type FileInfo struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Type     string `json:"type"` // "file" or "directory"
		Modified string `json:"modified"`
	}

	var items []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		item := FileInfo{
			Name:     entry.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().Local().Format("2006-01-02 15:04:05"),
		}
		if entry.IsDir() {
			item.Type = "directory"
		} else {
			item.Type = "file"
		}
		items = append(items, item)
	}

	gw.audit.Log(ctx.DeviceID, "file_list", reqPath, "success", fmt.Sprintf("count=%d", len(items)), r.RemoteAddr)
	writeJSON(w, 200, map[string]interface{}{"items": items, "path": reqPath})
}

// handleUpload handles file upload via multipart/form-data
func (gw *FileGateway) handleUpload(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	// Global panic recovery
	defer func() {
		if err := recover(); err != nil {
			log.Printf("[FileGateway] PANIC in handleUpload: %v", err)
			writeJSON(w, 500, map[string]string{"error": fmt.Sprintf("internal server error: %v", err)})
		}
	}()

	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	// Parse multipart (max 10GB)
	if err := r.ParseMultipartForm(10 << 30); err != nil {
		writeJSON(w, 400, map[string]string{"error": "parse multipart: " + err.Error()})
		return
	}

	targetPath := r.FormValue("path")
	if targetPath == "" {
		writeJSON(w, 400, map[string]string{"error": "missing path parameter"})
		return
	}

	// Permission check: shared directory allows upload but not overwrite others' files
	if gw.isPublicPath(targetPath) {
		// In public dir, check if file already exists and belongs to another device
		fullPath := gw.resolvePath(ctx.DeviceID, targetPath)
		if fullPath != "" {
			if _, err := os.Stat(fullPath); err == nil {
				owner := gw.getFileOwner(fullPath)
				if owner != "" && owner != ctx.DeviceID {
					gw.audit.Log(ctx.DeviceID, "permission_denied", targetPath, "denied", "cannot overwrite other's file", r.RemoteAddr)
					writeJSON(w, 403, map[string]string{"error": "cannot overwrite file owned by another device"})
					return
				}
			}
		}
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "missing file field"})
		return
	}
	defer file.Close()

	// Single file size check
	// 需求 4：拒绝可执行文件/脚本（扩展名 + 魔数双重判断，防改名绕过）
	if f, ferr := header.Open(); ferr == nil {
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		f.Close()
		if bad, reason := security.SniffDangerous(header.Filename, head[:n]); bad {
			gw.audit.Log(ctx.DeviceID, "upload_blocked", targetPath, "denied", reason, r.RemoteAddr)
			writeJSON(w, 415, map[string]string{"error": "upload blocked: " + reason})
			return
		}
	}

	if gw.config.MaxFileSizeBytes > 0 && header.Size > gw.config.MaxFileSizeBytes {
		gw.audit.Log(ctx.DeviceID, "quota_exceeded", targetPath, "denied",
			fmt.Sprintf("file_size=%d > max=%d", header.Size, gw.config.MaxFileSizeBytes), r.RemoteAddr)
		writeJSON(w, 413, map[string]string{"error": "file too large"})
		return
	}

	// Quota check
	if !gw.quota.CheckUploadQuota(ctx.DeviceID, header.Size) {
		gw.audit.Log(ctx.DeviceID, "quota_exceeded", targetPath, "denied", "upload quota exhausted", r.RemoteAddr)
		writeJSON(w, 403, map[string]string{"error": "upload quota exceeded"})
		return
	}

	// Resolve filesystem path
	fsPath := gw.resolvePath(ctx.DeviceID, targetPath)
	if fsPath == "" {
		log.Printf("[FileGateway] Upload path resolution failed for device %s, path: %s", ctx.DeviceID, targetPath)
		writeJSON(w, 403, map[string]string{"error": "access denied - invalid path"})
		return
	}

	log.Printf("[FileGateway] Upload resolved path: %s", fsPath)

	// Ensure parent directory exists (create if needed)
	parentDir := filepath.Dir(fsPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		log.Printf("[FileGateway] Failed to create parent directory %s: %v", parentDir, err)
		writeJSON(w, 500, map[string]string{"error": "create directory: " + err.Error()})
		return
	}

	log.Printf("[FileGateway] Uploading file to: %s (size: %d)", fsPath, header.Size)

	// Check if target path is an existing directory (from previous failed uploads)
	if info, err := os.Stat(fsPath); err == nil && info.IsDir() {
		log.Printf("[FileGateway] Target path is a directory, removing it: %s", fsPath)
		if err := os.RemoveAll(fsPath); err != nil {
			log.Printf("[FileGateway] Failed to remove directory %s: %v", fsPath, err)
			writeJSON(w, 500, map[string]string{"error": "cannot overwrite directory with file"})
			return
		}
	}

	dst, err := os.Create(fsPath)
	if err != nil {
		log.Printf("[FileGateway] Failed to create file %s: %v", fsPath, err)
		writeJSON(w, 500, map[string]string{"error": "create file: " + err.Error()})
		return
	}
	defer dst.Close()

	// Write with optional rate limiting
	var reader io.Reader = file
	if gw.config.UploadSpeedKBPS > 0 {
		reader = NewRateLimitedReader(file, gw.config.UploadSpeedKBPS*1024)
	}

	written, err := io.Copy(dst, reader)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "write file: " + err.Error()})
		return
	}

	// Update stats
	gw.quota.AddUpload(ctx.DeviceID, written)
	gw.setFileOwner(fsPath, ctx.DeviceID)

	gw.audit.Log(ctx.DeviceID, "file_upload", targetPath, "success",
		fmt.Sprintf("size=%d", written), r.RemoteAddr)

	writeJSON(w, 200, map[string]interface{}{
		"path": targetPath,
		"size": written,
	})
}

// handleDownload handles file download with optional rate limiting
func (gw *FileGateway) handleDownload(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		writeJSON(w, 400, map[string]string{"error": "missing path parameter"})
		return
	}

	fsPath := gw.resolvePath(ctx.DeviceID, reqPath)
	if fsPath == "" {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, 404, map[string]string{"error": "file not found"})
		} else {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
		}
		return
	}

	if info.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "cannot download directory"})
		return
	}

	// HEAD request: return metadata only
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Quota-Remaining", fmt.Sprintf("%d", gw.quota.GetRemainingQuota(ctx.DeviceID)))
		w.WriteHeader(200)
		return
	}

	file, err := os.Open(fsPath)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "open file: " + err.Error()})
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fsPath)))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))

	var writer io.Writer = w
	if gw.config.DownloadSpeedKBPS > 0 {
		writer = NewRateLimitedWriter(w, gw.config.DownloadSpeedKBPS*1024)
	}

	written, err := io.Copy(writer, file)
	if err != nil {
		log.Printf("[FileGateway] download error: %v", err)
		return
	}

	gw.quota.AddDownload(ctx.DeviceID, written)
	gw.audit.Log(ctx.DeviceID, "file_download", reqPath, "success",
		fmt.Sprintf("size=%d", written), r.RemoteAddr)
}

// handleDelete moves files to recycle bin instead of permanent deletion
func (gw *FileGateway) handleDelete(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	deleted := 0
	failed := 0

	for _, p := range req.Paths {
		fsPath := gw.resolvePath(ctx.DeviceID, p)
		if fsPath == "" {
			failed++
			continue
		}

		// Check if file exists
		info, err := os.Stat(fsPath)
		if os.IsNotExist(err) {
			deleted++
			continue
		}
		if err != nil {
			failed++
			continue
		}

		// Verify ownership
		owner := gw.getFileOwner(fsPath)
		if owner != "" && owner != ctx.DeviceID {
			gw.audit.Log(ctx.DeviceID, "permission_denied", p, "denied",
				"not file owner", r.RemoteAddr)
			failed++
			continue
		}

		// Move to recycle bin
		recycleDir := filepath.Join(gw.config.StorageRoot, "recycle_bin", ctx.DeviceID)
		os.MkdirAll(recycleDir, 0755)

		timestamp := time.Now().Format("20060102_150405")
		recycleName := fmt.Sprintf("%s_%s", timestamp, filepath.Base(fsPath))
		recyclePath := filepath.Join(recycleDir, recycleName)

		// Handle name conflicts
		if _, err := os.Stat(recyclePath); err == nil {
			recycleName = fmt.Sprintf("%s_%s_%d", timestamp, filepath.Base(fsPath), time.Now().UnixMilli())
			recyclePath = filepath.Join(recycleDir, recycleName)
		}

		// Save original path metadata
		metaPath := recyclePath + ".meta"
		meta := map[string]string{
			"original_path": p,
			"original_name": filepath.Base(fsPath),
			"deleted_at":    time.Now().UTC().Format(time.RFC3339),
			"device_id":     ctx.DeviceID,
		}
		metaJSON, _ := json.Marshal(meta)
		os.WriteFile(metaPath, metaJSON, 0644)

		if info.IsDir() {
			err = gw.moveDir(fsPath, recyclePath)
		} else {
			err = os.Rename(fsPath, recyclePath)
		}

		if err != nil {
			// Fallback: copy then delete
			if info.IsDir() {
				err = gw.copyDir(fsPath, recyclePath)
			} else {
				err = gw.copyFile(fsPath, recyclePath)
			}
			if err == nil {
				os.RemoveAll(fsPath)
			}
		}

		if err != nil {
			failed++
			continue
		}

		gw.audit.Log(ctx.DeviceID, "file_recycle", p, "success",
			fmt.Sprintf("recycle=%s", recycleName), r.RemoteAddr)
		deleted++
	}

	writeJSON(w, 200, map[string]int{"deleted": deleted, "failed": failed})
}

// handleRename handles file rename (private directory only)
func (gw *FileGateway) handleRename(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	// Cannot rename in public directories
	if (gw.isPublicPath(req.OldPath) || gw.isPublicPath(req.NewPath)) && ctx.Role != "admin" {
		gw.audit.Log(ctx.DeviceID, "permission_denied", req.OldPath, "denied",
			"rename not allowed in shared directory", r.RemoteAddr)
		writeJSON(w, 403, map[string]string{"error": "rename not allowed in shared directory"})
		return
	}

	oldFsPath := gw.resolvePath(ctx.DeviceID, req.OldPath)
	newFsPath := gw.resolvePath(ctx.DeviceID, req.NewPath)
	if oldFsPath == "" || newFsPath == "" {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	// Verify ownership
	owner := gw.getFileOwner(oldFsPath)
	if owner != "" && owner != ctx.DeviceID {
		writeJSON(w, 403, map[string]string{"error": "not file owner"})
		return
	}

	if err := os.Rename(oldFsPath, newFsPath); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	gw.audit.Log(ctx.DeviceID, "file_rename", req.OldPath, "success",
		fmt.Sprintf("new_path=%s", req.NewPath), r.RemoteAddr)

	writeJSON(w, 200, map[string]string{"old_path": req.OldPath, "new_path": req.NewPath})
}

// handleMkdir creates a new directory
func (gw *FileGateway) handleMkdir(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	if req.Path == "" {
		writeJSON(w, 400, map[string]string{"error": "missing path"})
		return
	}

	fsPath := gw.resolvePath(ctx.DeviceID, req.Path)
	if fsPath == "" {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	if err := os.MkdirAll(fsPath, 0755); err != nil {
		log.Printf("[FileGateway] Failed to create directory %s: %v", fsPath, err)
		writeJSON(w, 500, map[string]string{"error": "create directory: " + err.Error()})
		return
	}

	gw.audit.Log(ctx.DeviceID, "dir_create", req.Path, "success", "", r.RemoteAddr)
	writeJSON(w, 200, map[string]string{"path": req.Path})
}

// ========== Path Resolution ==========

func (gw *FileGateway) resolvePath(deviceID, reqPath string) string {
	// 隐私隔离（需求 3）：管理通道只允许 shared/public，private 一律拒绝
	if deviceID == AdminDeviceID {
		p := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(reqPath)), "/")
		if !strings.HasPrefix(p, "shared") && !strings.HasPrefix(p, "public") {
			return ""
		}
	}
	// 需求 1：路径穿越防护 —— 任何 .. 段一律拒绝
	if security.HasDotDot(reqPath) {
		return ""
	}

	// Normalize path
	reqPath = filepath.Clean(reqPath)
	reqPath = strings.ReplaceAll(reqPath, "\\", "/")
	reqPath = strings.TrimPrefix(reqPath, "/")

	// Determine base directory based on path prefix
	var basePath string
	var relativePath string

	switch {
	case strings.HasPrefix(reqPath, "private"):
		// Private directory: device-specific
		basePath = filepath.Join(gw.config.StorageRoot, "devices", deviceID, "private")
		relativePath = strings.TrimPrefix(reqPath, "private")
		relativePath = strings.TrimPrefix(relativePath, "/")
	case strings.HasPrefix(reqPath, "shared"):
		// Shared directory: common for all devices
		basePath = filepath.Join(gw.config.StorageRoot, "shared")
		relativePath = strings.TrimPrefix(reqPath, "shared")
		relativePath = strings.TrimPrefix(relativePath, "/")
	case strings.HasPrefix(reqPath, "public"):
		// Public directory: common for all devices
		basePath = filepath.Join(gw.config.StorageRoot, "public")
		relativePath = strings.TrimPrefix(reqPath, "public")
		relativePath = strings.TrimPrefix(relativePath, "/")
	default:
		// Default to private
		basePath = filepath.Join(gw.config.StorageRoot, "devices", deviceID, "private")
		relativePath = reqPath
	}

	fullPath := filepath.Join(basePath, relativePath)

	// Security: prevent path traversal
	fullPath = filepath.Clean(fullPath)
	// 需求 1/8：必须落在本类别基目录内（防 private ↔ shared ↔ 他人 private 横跳）
	if basePath == "" || !security.ContainPath(basePath, fullPath) {
		return ""
	}
	if !strings.HasPrefix(fullPath, filepath.Clean(gw.config.StorageRoot)) {
		return ""
	}

	return fullPath
}

func (gw *FileGateway) isPublicPath(reqPath string) bool {
	reqPath = strings.TrimPrefix(reqPath, "/")
	return strings.HasPrefix(reqPath, "shared/") || strings.HasPrefix(reqPath, "public/")
}

// ========== File Ownership Tracking ==========

var ownershipDB = struct {
	sync.RWMutex
	data map[string]string // path -> deviceID
}{data: make(map[string]string)}

func (gw *FileGateway) setFileOwner(fsPath, deviceID string) {
	ownershipDB.Lock()
	defer ownershipDB.Unlock()
	ownershipDB.data[fsPath] = deviceID
}

func (gw *FileGateway) getFileOwner(fsPath string) string {
	ownershipDB.RLock()
	defer ownershipDB.RUnlock()
	return ownershipDB.data[fsPath]
}

// ========== Rate Limiting ==========

type RateLimitedReader struct {
	reader      io.Reader
	bytesPerSec int64
	bytesRead   int64
	startTime   time.Time
}

func NewRateLimitedReader(r io.Reader, bytesPerSec int64) *RateLimitedReader {
	return &RateLimitedReader{
		reader:      r,
		bytesPerSec: bytesPerSec,
		startTime:   time.Now(),
	}
}

func (rl *RateLimitedReader) Read(p []byte) (int, error) {
	if rl.bytesPerSec <= 0 {
		return rl.reader.Read(p)
	}

	elapsed := time.Since(rl.startTime).Seconds()
	expectedBytes := int64(elapsed * float64(rl.bytesPerSec))

	if rl.bytesRead >= expectedBytes && rl.bytesRead > 0 {
		sleepDuration := time.Duration(float64(rl.bytesPerSec) / float64(rl.bytesPerSec) * float64(time.Second) * 0.1)
		time.Sleep(sleepDuration)
	}

	maxRead := rl.bytesPerSec / 10
	if maxRead <= 0 {
		maxRead = 32768
	}
	if int64(len(p)) > maxRead {
		p = p[:maxRead]
	}

	n, err := rl.reader.Read(p)
	rl.bytesRead += int64(n)
	return n, err
}

type RateLimitedWriter struct {
	writer       io.Writer
	bytesPerSec  int64
	bytesWritten int64
	startTime    time.Time
}

func NewRateLimitedWriter(w io.Writer, bytesPerSec int64) *RateLimitedWriter {
	return &RateLimitedWriter{
		writer:      w,
		bytesPerSec: bytesPerSec,
		startTime:   time.Now(),
	}
}

func (rl *RateLimitedWriter) Write(p []byte) (int, error) {
	if rl.bytesPerSec <= 0 {
		return rl.writer.Write(p)
	}

	elapsed := time.Since(rl.startTime).Seconds()
	expectedBytes := int64(elapsed * float64(rl.bytesPerSec))

	if rl.bytesWritten >= expectedBytes && rl.bytesWritten > 0 {
		sleepDuration := time.Duration(float64(rl.bytesPerSec) / float64(rl.bytesPerSec) * float64(time.Second) * 0.1)
		time.Sleep(sleepDuration)
	}

	maxWrite := rl.bytesPerSec / 10
	if maxWrite <= 0 {
		maxWrite = 32768
	}
	if int64(len(p)) > maxWrite {
		p = p[:maxWrite]
	}

	n, err := rl.writer.Write(p)
	rl.bytesWritten += int64(n)
	return n, err
}

// ========== Helpers ==========

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// 需求 6：出口统一脱敏（错误信息不泄露本地路径/堆栈）
	json.NewEncoder(w).Encode(security.SanitizeResponse(data))
}

// ========== Recycle Bin Helpers ==========

func (gw *FileGateway) copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func (gw *FileGateway) copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return gw.copyFile(path, dstPath)
	})
}

func (gw *FileGateway) moveDir(src, dst string) error {
	// Try rename first
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Fallback: copy then delete
	if err := gw.copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

// RecycleBinItem represents a file in recycle bin
type RecycleBinItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	OriginalPath string `json:"original_path"`
	OriginalName string `json:"original_name"`
	DeletedAt    string `json:"deleted_at"`
	Size         int64  `json:"size"`
	IsDir        bool   `json:"is_directory"`
}

// handleRecycleList lists files in recycle bin
func (gw *FileGateway) handleRecycleList(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	recycleDir := filepath.Join(gw.config.StorageRoot, "recycle_bin", ctx.DeviceID)
	var items []RecycleBinItem

	entries, err := os.ReadDir(recycleDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			// Skip .meta files
			if strings.HasSuffix(entry.Name(), ".meta") {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			item := RecycleBinItem{
				ID:    entry.Name(),
				Name:  entry.Name(),
				Size:  info.Size(),
				IsDir: false,
			}

			// Try to read metadata
			metaPath := filepath.Join(recycleDir, entry.Name()+".meta")
			if meta, err := os.ReadFile(metaPath); err == nil {
				var metaMap map[string]string
				if json.Unmarshal(meta, &metaMap) == nil {
					item.OriginalPath = metaMap["original_path"]
					item.OriginalName = metaMap["original_name"]
					item.DeletedAt = metaMap["deleted_at"]
				}
			}

			items = append(items, item)
		}
	}

	writeJSON(w, 200, map[string]interface{}{
		"code": 0,
		"data": map[string]interface{}{
			"total": len(items),
			"items": items,
		},
	})
}

// handleRecycleRestore restores a file from recycle bin
func (gw *FileGateway) handleRecycleRestore(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	recycleDir := filepath.Join(gw.config.StorageRoot, "recycle_bin", ctx.DeviceID)
	recyclePath := filepath.Join(recycleDir, req.ID)

	// Read metadata
	metaPath := recyclePath + ".meta"
	meta, err := os.ReadFile(metaPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "metadata not found"})
		return
	}

	var metaMap map[string]string
	if err := json.Unmarshal(meta, &metaMap); err != nil {
		writeJSON(w, 500, map[string]string{"error": "invalid metadata"})
		return
	}

	// Resolve original path
	originalPath := gw.resolvePath(ctx.DeviceID, metaMap["original_path"])
	if originalPath == "" {
		writeJSON(w, 403, map[string]string{"error": "invalid restore path"})
		return
	}

	// Ensure parent directory exists
	os.MkdirAll(filepath.Dir(originalPath), 0755)

	// Move back
	if err := os.Rename(recyclePath, originalPath); err != nil {
		if err := gw.copyFile(recyclePath, originalPath); err != nil {
			writeJSON(w, 500, map[string]string{"error": "restore failed: " + err.Error()})
			return
		}
		os.Remove(recyclePath)
	}

	// Remove metadata
	os.Remove(metaPath)

	gw.audit.Log(ctx.DeviceID, "file_restore", metaMap["original_path"], "success", "", r.RemoteAddr)
	writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
}

// handleRecycleDelete permanently deletes a file from recycle bin
func (gw *FileGateway) handleRecycleDelete(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}

	recycleDir := filepath.Join(gw.config.StorageRoot, "recycle_bin", ctx.DeviceID)
	deleted := 0

	for _, id := range req.IDs {
		recyclePath := filepath.Join(recycleDir, id)
		metaPath := recyclePath + ".meta"

		os.Remove(recyclePath)
		os.Remove(metaPath)
		deleted++
	}

	gw.audit.Log(ctx.DeviceID, "recycle_permanent_delete", "", "success",
		fmt.Sprintf("count=%d", deleted), r.RemoteAddr)
	writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok", "data": map[string]int{"deleted": deleted}})
}

// handleRecycleClear clears all files in recycle bin
func (gw *FileGateway) handleRecycleClear(w http.ResponseWriter, r *http.Request, ctx *AuthContext) {
	if r.Method != http.MethodDelete {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	recycleDir := filepath.Join(gw.config.StorageRoot, "recycle_bin", ctx.DeviceID)
	os.RemoveAll(recycleDir)
	os.MkdirAll(recycleDir, 0755)

	gw.audit.Log(ctx.DeviceID, "recycle_clear", "", "success", "", r.RemoteAddr)
	writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
}

// cleanupExpiredRecycleBin removes files older than 30 days
func (gw *FileGateway) cleanupExpiredRecycleBin() {
	recycleBase := filepath.Join(gw.config.StorageRoot, "recycle_bin")
	entries, err := os.ReadDir(recycleBase)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -30)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		deviceDir := filepath.Join(recycleBase, entry.Name())
		files, err := os.ReadDir(deviceDir)
		if err != nil {
			continue
		}

		for _, file := range files {
			if strings.HasSuffix(file.Name(), ".meta") {
				continue
			}

			info, err := file.Info()
			if err != nil {
				continue
			}

			if info.ModTime().Before(cutoff) {
				os.Remove(filepath.Join(deviceDir, file.Name()))
				os.Remove(filepath.Join(deviceDir, file.Name()+".meta"))
			}
		}
	}
}

// Ensure unused imports don't cause errors
var _ multipart.File

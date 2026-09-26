package file_gateway

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"frostleaves/pkg/security"
)

// ========== 分享码 / 访客链接 ==========
//
// 让没有设备 token 的访客（亲友浏览器）也能按分享码访问指定目录。
// 设计见 docs/DESIGN-share.md：
//   - 免设备鉴权，但必须过分享码校验、目录边界校验、失败限流、审计
//   - 默认只读、默认 24 小时过期、默认不能覆盖已有文件

const (
	shareCodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ" // 去掉 0/O/1/I 等易混字符
	shareCodeLength   = 10
	shareDefaultTTL   = 24 * time.Hour

	shareFailWindow   = time.Minute      // 统计窗口
	shareFailLimit    = 10               // 窗口内允许的失败次数
	shareFailBlockFor = 10 * time.Minute // 超限封禁时长
)

type SharePerm string

const (
	SharePermRead   SharePerm = "read"   // 只看 / 下载
	SharePermUpload SharePerm = "upload" // 只读 + 可上传（投递箱），不能删除/改名/覆盖
	SharePermWrite  SharePerm = "write"  // 完整读写
)

// Share 一条分享记录
type Share struct {
	Code         string    `json:"code"`
	Target       string    `json:"target"` // 相对 storage 的目录（或单个文件）
	Perm         SharePerm `json:"perm"`
	ExpiresAt    time.Time `json:"expires_at"`    // 零值 = 永久
	MaxDownloads int64     `json:"max_downloads"` // 0 = 不限
	Downloads    int64     `json:"downloads"`
	Note         string    `json:"note,omitempty"`
	Files        []string  `json:"files,omitempty"` // 白名单：仅暴露这些文件（空 = 整个目录）
	CreatedAt    time.Time `json:"created_at"`
	CreatedBy    string    `json:"created_by,omitempty"`
	Revoked      bool      `json:"revoked"`
}

// ShareCreateRequest 创建分享的入参
type ShareCreateRequest struct {
	Target       string    `json:"target"`
	Perm         SharePerm `json:"perm"`
	ExpiresHours int       `json:"expires_hours"` // 0 = 默认 24h；-1 = 永久
	MaxDownloads int64     `json:"max_downloads"`
	Note         string    `json:"note"`
	Files        []string  `json:"files,omitempty"` // 可选：只分享这几个文件
	CreatedBy    string    `json:"-"`
}

type shareStoreFile struct {
	Shares map[string]*Share `json:"shares"`
}

type shareFailEntry struct {
	count      int
	windowFrom time.Time
	blockedTil time.Time
}

// ShareStore 分享记录 + 访问限流
type ShareStore struct {
	mu     sync.RWMutex
	shares map[string]*Share

	storePath string
	storeMu   sync.Mutex

	failMu sync.Mutex
	fails  map[string]*shareFailEntry
}

func NewShareStore() *ShareStore {
	return &ShareStore{
		shares: make(map[string]*Share),
		fails:  make(map[string]*shareFailEntry),
	}
}

// ========== 持久化 ==========

func (s *ShareStore) SetStore(path string) { s.storePath = path }

func (s *ShareStore) Load() error {
	if s.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var f shareStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", s.storePath, err)
	}
	s.mu.Lock()
	for code, sh := range f.Shares {
		if sh == nil || code == "" {
			continue
		}
		s.shares[code] = sh
	}
	total := len(s.shares)
	s.mu.Unlock()
	log.Printf("[Share] restored %d share(s)", total)
	return nil
}

func (s *ShareStore) save() {
	if s.storePath == "" {
		return
	}
	s.storeMu.Lock()
	defer s.storeMu.Unlock()

	s.mu.RLock()
	snap := shareStoreFile{Shares: make(map[string]*Share, len(s.shares))}
	for k, v := range s.shares {
		cp := *v
		snap.Shares[k] = &cp
	}
	s.mu.RUnlock()

	if err := writeJSONAtomic(s.storePath, snap); err != nil {
		log.Printf("[Share] save failed: %v", err)
	}
}

// ========== 管理操作 ==========

func generateShareCode() (string, error) {
	buf := make([]byte, shareCodeLength)
	max := big.NewInt(int64(len(shareCodeAlphabet)))
	for i := 0; i < shareCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buf[i] = shareCodeAlphabet[n.Int64()]
	}
	return string(buf), nil
}

// Create 新建分享
func (s *ShareStore) Create(req ShareCreateRequest) (*Share, error) {
	target := filepath.ToSlash(filepath.Clean(strings.TrimSpace(req.Target)))
	target = strings.TrimPrefix(target, "/")
	if target == "" || target == "." {
		return nil, fmt.Errorf("target is required")
	}
	// 需求 8：分享目标必须落在 shared/ 或 public/（任意 .. 段一律拒绝，防穿越到私人目录）
	if !security.IsPublicTarget(target) {
		return nil, fmt.Errorf("invalid target")
	}

	perm := req.Perm
	switch perm {
	case SharePermRead, SharePermUpload, SharePermWrite:
	case "":
		perm = SharePermRead
	default:
		return nil, fmt.Errorf("invalid perm: %s", req.Perm)
	}

	var expires time.Time
	switch {
	case req.ExpiresHours < 0:
		expires = time.Time{} // 永久
	case req.ExpiresHours == 0:
		expires = time.Now().Add(shareDefaultTTL)
	default:
		expires = time.Now().Add(time.Duration(req.ExpiresHours) * time.Hour)
	}

	// 文件白名单（可选）：只暴露指定文件，名字必须是相对目标目录的安全路径
	var files []string
	if len(req.Files) > 0 {
		seen := map[string]bool{}
		for _, f := range req.Files {
			name := strings.TrimSpace(filepath.ToSlash(f))
			name = strings.TrimPrefix(name, "/")
			if name == "" || name == "." || security.HasDotDot(name) {
				return nil, fmt.Errorf("invalid file in whitelist: %s", f)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			files = append(files, name)
			if len(files) >= 200 {
				break
			}
		}
	}

	var code string
	for i := 0; i < 5; i++ {
		c, err := generateShareCode()
		if err != nil {
			return nil, err
		}
		s.mu.RLock()
		_, exists := s.shares[c]
		s.mu.RUnlock()
		if !exists {
			code = c
			break
		}
	}
	if code == "" {
		return nil, fmt.Errorf("failed to allocate share code")
	}

	sh := &Share{
		Code:         code,
		Target:       target,
		Perm:         perm,
		ExpiresAt:    expires,
		MaxDownloads: req.MaxDownloads,
		Note:         req.Note,
		Files:        files,
		CreatedAt:    time.Now(),
		CreatedBy:    req.CreatedBy,
	}

	s.mu.Lock()
	s.shares[code] = sh
	s.mu.Unlock()
	s.save()

	log.Printf("[Share] created %s -> %s (perm=%s)", code, target, perm)
	return sh, nil
}

// List 返回全部分享（含已过期/已吊销，由调用方判断）
func (s *ShareStore) List() []*Share {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Share, 0, len(s.shares))
	for _, sh := range s.shares {
		cp := *sh
		out = append(out, &cp)
	}
	return out
}

// Get 按码取分享（不做有效性判断）
func (s *ShareStore) Get(code string) (*Share, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shares[code]
	if !ok {
		return nil, false
	}
	cp := *sh
	return &cp, true
}

// Revoke 吊销分享
func (s *ShareStore) Revoke(code string) error {
	s.mu.Lock()
	sh, ok := s.shares[code]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("share not found")
	}
	sh.Revoked = true
	s.mu.Unlock()
	s.save()
	log.Printf("[Share] revoked %s", code)
	return nil
}

// validateLocked 校验分享有效性（调用方需持读锁）
func validateShare(sh *Share) error {
	if sh == nil {
		return fmt.Errorf("share not found")
	}
	if sh.Revoked {
		return fmt.Errorf("share revoked")
	}
	if !sh.ExpiresAt.IsZero() && time.Now().After(sh.ExpiresAt) {
		return fmt.Errorf("share expired")
	}
	if sh.MaxDownloads > 0 && sh.Downloads >= sh.MaxDownloads {
		return fmt.Errorf("share download limit reached")
	}
	return nil
}

// registerDownload 下载计数 +1，并在达到上限时自动失效
func (s *ShareStore) registerDownload(code string) {
	s.mu.Lock()
	if sh, ok := s.shares[code]; ok {
		sh.Downloads++
	}
	s.mu.Unlock()
	s.save()
}

// ========== 访问限流（防分享码爆破） ==========

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *ShareStore) allowAttempt(ip string) bool {
	s.failMu.Lock()
	defer s.failMu.Unlock()

	e, ok := s.fails[ip]
	if !ok {
		return true
	}
	if time.Now().Before(e.blockedTil) {
		return false
	}
	return true
}

func (s *ShareStore) noteFailure(ip string) {
	s.failMu.Lock()
	defer s.failMu.Unlock()

	now := time.Now()
	e, ok := s.fails[ip]
	if !ok || now.Sub(e.windowFrom) > shareFailWindow {
		s.fails[ip] = &shareFailEntry{count: 1, windowFrom: now}
		return
	}
	e.count++
	if e.count >= shareFailLimit {
		e.blockedTil = now.Add(shareFailBlockFor)
		e.count = 0
		e.windowFrom = now
		log.Printf("[Share] IP %s temporarily blocked after %d failed attempts", ip, shareFailLimit)
	}
}

func (s *ShareStore) noteSuccess(ip string) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	delete(s.fails, ip)
}

// ========== 访客 HTTP 处理（挂在文件网关 9090，免设备鉴权） ==========

// handleShareVisitor 路由：
//
//	GET  /api/v1/share/{code}                   分享信息 + 文件列表
//	GET  /api/v1/share/{code}/download?path=    下载
//	POST /api/v1/share/{code}/upload?path=      上传（perm=upload/write）
func (gw *FileGateway) handleShareVisitor(w http.ResponseWriter, r *http.Request) {
	if gw.shares == nil {
		writeJSON(w, 404, map[string]string{"error": "sharing is disabled"})
		return
	}

	ip := clientIP(r)
	if !gw.shares.allowAttempt(ip) {
		gw.audit.Log("share", "share_access", r.URL.Path, "denied", "rate limited", r.RemoteAddr)
		writeJSON(w, 429, map[string]string{"error": "too many attempts, try again later"})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/share/")
	parts := strings.SplitN(rest, "/", 2)
	code := strings.ToUpper(strings.TrimSpace(parts[0]))
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if code == "" {
		writeJSON(w, 404, map[string]string{"error": "not found"})
		return
	}

	sh, ok := gw.shares.Get(code)
	if !ok {
		gw.shares.noteFailure(ip)
		gw.audit.Log("share:"+code, "share_access", r.URL.Path, "denied", "invalid code", r.RemoteAddr)
		writeJSON(w, 404, map[string]string{"error": "invalid or expired share code"})
		return
	}
	if err := validateShare(sh); err != nil {
		gw.shares.noteFailure(ip)
		gw.audit.Log("share:"+code, "share_access", r.URL.Path, "denied", err.Error(), r.RemoteAddr)
		writeJSON(w, 410, map[string]string{"error": err.Error()})
		return
	}
	gw.shares.noteSuccess(ip)

	switch action {
	case "":
		gw.handleShareInfo(w, r, sh)
	case "download":
		gw.handleShareDownload(w, r, sh)
	case "upload":
		gw.handleShareUpload(w, r, sh)
	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

// shareRoot 计算分享目标目录的绝对路径，并确认它在 storage 之内
func (gw *FileGateway) shareRoot(sh *Share) (string, error) {
	storage := filepath.Clean(gw.config.StorageRoot)
	root := filepath.Clean(filepath.Join(storage, filepath.FromSlash(sh.Target)))
	if root != storage && !strings.HasPrefix(root, storage+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid share target")
	}
	return root, nil
}

// shareResolve 把访客给的相对路径解析到 root 之下（防目录穿越）
func shareResolve(root, rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	clean := filepath.Clean("/" + strings.TrimPrefix(rel, "/"))
	full := filepath.Join(root, filepath.FromSlash(clean))
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path out of share scope")
	}
	return full, nil
}

type shareFileItem struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Type     string `json:"type"`
	Modified string `json:"modified"`
}

func (gw *FileGateway) handleShareInfo(w http.ResponseWriter, r *http.Request, sh *Share) {
	if r.Method != http.MethodGet {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	root, err := gw.shareRoot(sh)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	info := map[string]interface{}{
		"code":       sh.Code,
		"note":       sh.Note,
		"perm":       sh.Perm,
		"expires_at": sh.ExpiresAt,
		"path":       r.URL.Query().Get("path"),
	}

	// 白名单分享：只返回指定文件，禁止目录导航
	if len(sh.Files) > 0 {
		wlItems := make([]shareFileItem, 0, len(sh.Files))
		for _, name := range sh.Files {
			full, err2 := shareResolve(root, name)
			if err2 != nil {
				continue
			}
			fi, err2 := os.Stat(full)
			if err2 != nil {
				continue
			}
			typ := "file"
			if fi.IsDir() {
				typ = "directory"
			}
			wlItems = append(wlItems, shareFileItem{
				Name:     name,
				Size:     fi.Size(),
				Type:     typ,
				Modified: fi.ModTime().Local().Format("2006-01-02 15:04:05"),
			})
		}
		gw.audit.Log("share:"+sh.Code, "file_list", sh.Target, "success",
			fmt.Sprintf("whitelist=%d", len(wlItems)), r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"share": info, "items": wlItems})
		return
	}

	var items []shareFileItem

	// 允许访客在分享目录内浏览子目录（path 为相对分享根的路径）
	listRoot := root
	if rel := r.URL.Query().Get("path"); rel != "" {
		sub, err := shareResolve(root, rel)
		if err != nil {
			writeJSON(w, 403, map[string]string{"error": "access denied"})
			return
		}
		if st, err := os.Stat(sub); err == nil && st.IsDir() {
			listRoot = sub
		} else {
			writeJSON(w, 404, map[string]string{"error": "directory not found"})
			return
		}
	}

	st, err := os.Stat(listRoot)
	if err != nil {
		writeJSON(w, 200, map[string]interface{}{"share": info, "items": []shareFileItem{}})
		return
	}

	if !st.IsDir() {
		items = append(items, shareFileItem{
			Name:     st.Name(),
			Size:     st.Size(),
			Type:     "file",
			Modified: st.ModTime().Local().Format("2006-01-02 15:04:05"),
		})
		writeJSON(w, 200, map[string]interface{}{"share": info, "items": items})
		return
	}

	entries, err := os.ReadDir(listRoot)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			continue
		}
		item := shareFileItem{
			Name:     e.Name(),
			Size:     fi.Size(),
			Modified: fi.ModTime().Local().Format("2006-01-02 15:04:05"),
		}
		if e.IsDir() {
			item.Type = "directory"
		} else {
			item.Type = "file"
		}
		items = append(items, item)
	}

	gw.audit.Log("share:"+sh.Code, "file_list", sh.Target, "success",
		fmt.Sprintf("count=%d", len(items)), r.RemoteAddr)
	writeJSON(w, 200, map[string]interface{}{"share": info, "items": items})
}

func (gw *FileGateway) handleShareDownload(w http.ResponseWriter, r *http.Request, sh *Share) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}

	root, err := gw.shareRoot(sh)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		// 单文件分享时允许省略 path
		if st, err := os.Stat(root); err == nil && !st.IsDir() {
			rel = filepath.Base(root)
			root = filepath.Dir(root)
		} else {
			writeJSON(w, 400, map[string]string{"error": "missing path parameter"})
			return
		}
	}

	// 白名单分享：只允许下载白名单内的文件
	if len(sh.Files) > 0 {
		cleanRel := filepath.ToSlash(strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/"))
		allowed := false
		for _, name := range sh.Files {
			if name == cleanRel {
				allowed = true
				break
			}
		}
		if !allowed {
			gw.audit.Log("share:"+sh.Code, "file_download", rel, "denied", "not in whitelist", r.RemoteAddr)
			writeJSON(w, 403, map[string]string{"error": "file not in share whitelist"})
			return
		}
	}

	fsPath, err := shareResolve(root, rel)
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	info, err := os.Stat(fsPath)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "file not found"})
		return
	}
	if info.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "cannot download directory"})
		return
	}

	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
		w.Header().Set("Content-Type", "application/octet-stream")
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
		log.Printf("[Share] download error: %v", err)
		return
	}

	gw.shares.registerDownload(sh.Code)
	gw.audit.Log("share:"+sh.Code, "file_download", rel, "success",
		fmt.Sprintf("size=%d", written), r.RemoteAddr)
}

func (gw *FileGateway) handleShareUpload(w http.ResponseWriter, r *http.Request, sh *Share) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	if sh.Perm != SharePermUpload && sh.Perm != SharePermWrite {
		gw.audit.Log("share:"+sh.Code, "file_upload", "", "denied", "share is read-only", r.RemoteAddr)
		writeJSON(w, 403, map[string]string{"error": "this share is read-only"})
		return
	}

	root, err := gw.shareRoot(sh)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}

	rel := r.URL.Query().Get("path")
	dirPath, err := shareResolve(root, rel)
	if err != nil {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}
	if st, err := os.Stat(dirPath); err != nil || !st.IsDir() {
		writeJSON(w, 400, map[string]string{"error": "target directory not found"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "missing file field"})
		return
	}
	defer file.Close()

	// 安全补丁 1.0.1：上传内容校验（包含式黑名单 + 后缀黑名单 + MZ 魔数），
	// 投递箱/公开分享是暴露面最大的入口，必须与主上传路径同等校验。
	if f, ferr := header.Open(); ferr == nil {
		head := make([]byte, 512)
		n, _ := io.ReadFull(f, head)
		f.Close()
		if bad, reason := security.SniffDangerous(header.Filename, head[:n]); bad {
			gw.audit.Log("share:"+sh.Code, "upload_blocked", rel+"/"+filepath.Base(header.Filename), "denied", reason, r.RemoteAddr)
			writeJSON(w, 415, map[string]string{"error": "upload blocked: " + reason})
			return
		}
	}

	name := filepath.Base(header.Filename)
	if name == "" || name == "." || name == string(os.PathSeparator) {
		writeJSON(w, 400, map[string]string{"error": "invalid file name"})
		return
	}

	target := filepath.Join(dirPath, name)
	if target != dirPath && !strings.HasPrefix(target, dirPath+string(os.PathSeparator)) {
		writeJSON(w, 403, map[string]string{"error": "access denied"})
		return
	}

	if _, err := os.Stat(target); err == nil {
		writeJSON(w, 409, map[string]string{"error": "file already exists"})
		return
	}

	dst, err := os.Create(target)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "create file: " + err.Error()})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(target)
		writeJSON(w, 500, map[string]string{"error": "write file: " + err.Error()})
		return
	}

	gw.quota.AddUpload("share:"+sh.Code, written)
	gw.audit.Log("share:"+sh.Code, "file_upload", rel+"/"+name, "success",
		fmt.Sprintf("size=%d", written), r.RemoteAddr)

	writeJSON(w, 200, map[string]interface{}{
		"code": 0,
		"msg":  "ok",
		"data": map[string]interface{}{"name": name, "size": written},
	})
}

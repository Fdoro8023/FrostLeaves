package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ===================== 需求 1：客户端版本校验 =====================

// ClientVersionHeader 是客户端上报自身版本号的请求头。
const ClientVersionHeader = "X-Client-Version"

// normalizeVersion 归一化版本串：去掉前导 v、空格、大小写差异。
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	v = strings.ReplaceAll(v, " ", "")
	return strings.ToLower(v)
}

// checkClientVersion 校验客户端版本是否与服务端完全一致。
//
// 规则：
//   - 客户端带 X-Client-Version 且与服务端不一致 → 426 拒绝；
//   - 未带该头且 platform 不是 "web" → 426 拒绝（旧版客户端强制升级）；
//   - 内置网页端（platform=web，与服务端同版本一起发布）放行。
//
// 返回 false 时响应已写好。
func checkClientVersion(w http.ResponseWriter, r *http.Request, platform string) bool {
	v := strings.TrimSpace(r.Header.Get(ClientVersionHeader))
	if v == "" {
		if strings.EqualFold(platform, "web") {
			return true
		}
		writeJSON(w, 426, map[string]interface{}{
			"error":          "client version header is required; please update your client",
			"code":           "version_required",
			"server_version": AppVersion,
		})
		return false
	}
	if normalizeVersion(v) != normalizeVersion(AppVersion) {
		writeJSON(w, 426, map[string]interface{}{
			"error":          "client version does not match the server",
			"code":           "version_mismatch",
			"server_version": AppVersion,
			"client_version": v,
		})
		return false
	}
	return true
}

// ===================== 需求 2：检查更新 =====================

// GitHubRepo 是发布更新包的仓库（owner/repo）。
const GitHubRepo = "Fdoro8023/FrostLeaves"

// GitHubRelease 是 GitHub releases 的精简结构。
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// UpdateStatus 是对外返回的更新检查结果。
type UpdateStatus struct {
	CurrentVersion string  `json:"current_version"`
	LatestVersion  string  `json:"latest_version"`
	HasUpdate      bool    `json:"has_update"`
	ReleaseName    string  `json:"release_name"`
	Notes          string  `json:"notes"`
	PublishedAt    string  `json:"published_at"`
	HTMLURL        string  `json:"html_url"`
	Assets         []Asset `json:"assets"`
	CheckedAt      string  `json:"checked_at"`
	Error          string  `json:"error,omitempty"`
}

// Asset 是可下载的更新包。
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

var (
	updateMu     sync.RWMutex
	updateCached *UpdateStatus
)

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// parseVersionParts 提取 x.y.z 数字，找不到时返回 ok=false。
func parseVersionParts(v string) (int, int, int, bool) {
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return 0, 0, 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	c, _ := strconv.Atoi(m[3])
	return a, b, c, true
}

// isNewerVersion 判断 latest 是否比 current 新（按 x.y.z 数字比较）。
func isNewerVersion(current, latest string) bool {
	ca, cb, cc, ok1 := parseVersionParts(current)
	la, lb, lc, ok2 := parseVersionParts(latest)
	if !ok1 || !ok2 {
		return false
	}
	if la != ca {
		return la > ca
	}
	if lb != cb {
		return lb > cb
	}
	return lc > cc
}

// fetchLatestRelease 拉取 GitHub 最新 release（服务端出网）。
func fetchLatestRelease() (*GitHubRelease, error) {
	url := "https://api.github.com/repos/" + GitHubRepo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FrostLeaves-Server/"+AppVersion)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no published release found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var rel GitHubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// checkForUpdate 执行一次更新检查并返回结果（不写缓存）。
func checkForUpdate() UpdateStatus {
	st := UpdateStatus{
		CurrentVersion: AppVersion,
		CheckedAt:      time.Now().Format(time.RFC3339),
	}
	rel, err := fetchLatestRelease()
	if err != nil {
		st.Error = err.Error()
		return st
	}
	latest := rel.TagName
	if latest == "" {
		latest = rel.Name
	}
	st.LatestVersion = latest
	st.ReleaseName = rel.Name
	st.Notes = rel.Body
	st.PublishedAt = rel.PublishedAt
	st.HTMLURL = rel.HTMLURL
	st.HasUpdate = isNewerVersion(AppVersion, latest)
	for _, a := range rel.Assets {
		st.Assets = append(st.Assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	return st
}

// updateDailyCheckLoop 每隔 24 小时自动检查一次（需求 2）。
func updateDailyCheckLoop() {
	run := func() {
		st := checkForUpdate()
		updateMu.Lock()
		updateCached = &st
		updateMu.Unlock()
		if st.Error != "" {
			log.Printf("[Update] check failed: %s", st.Error)
			return
		}
		if st.HasUpdate {
			log.Printf("[Update] new version available: %s (current %s)", st.LatestVersion, st.CurrentVersion)
		} else {
			log.Printf("[Update] already up to date (%s)", st.CurrentVersion)
		}
	}
	run()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		run()
	}
}

// ===================== 更新接口 =====================

// setupUpdateRoutes 注册检查/下载更新接口。
func setupUpdateRoutes(mux *http.ServeMux, cfg *ServerConfig) {
	// 检查更新：客户端与服务端【关于】页都调用（不需要管理员权限）
	mux.HandleFunc("/api/v1/update/check", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("refresh") == "1"
		if !force {
			updateMu.RLock()
			cached := updateCached
			updateMu.RUnlock()
			if cached != nil && cached.Error == "" {
				writeJSON(w, 200, map[string]interface{}{"code": 0, "data": cached})
				return
			}
		}
		st := checkForUpdate()
		updateMu.Lock()
		updateCached = &st
		updateMu.Unlock()
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": st})
	})

	// 下载更新包：只允许 GitHub 域名，落到 <data>/updates/
	mux.HandleFunc("/api/v1/update/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			URL  string `json:"url"`
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
			writeJSON(w, 400, map[string]string{"error": "url is required"})
			return
		}
		if !isAllowedUpdateURL(body.URL) {
			writeJSON(w, 400, map[string]string{"error": "download url must point to the release host"})
			return
		}
		name := sanitizeAssetName(body.Name)
		if name == "" {
			writeJSON(w, 400, map[string]string{"error": "invalid asset name"})
			return
		}
		dir := filepath.Join(cfg.DataDir, "updates")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		dst := filepath.Join(dir, name)
		n, err := downloadTo(body.URL, dst)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": "download failed: " + err.Error()})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"path": dst,
				"name": name,
				"size": n,
			},
		})
	})
}

// isAllowedUpdateURL 限制只从 GitHub 官方域名下载，避免 SSRF。
func isAllowedUpdateURL(raw string) bool {
	if !strings.HasPrefix(raw, "https://") {
		return false
	}
	host := raw
	host = strings.TrimPrefix(host, "https://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	host = strings.ToLower(host)
	for _, ok := range []string{
		"github.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"codeload.github.com",
		"github-releases.githubusercontent.com",
	} {
		if host == ok || strings.HasSuffix(host, "."+ok) {
			return true
		}
	}
	return false
}

// sanitizeAssetName 只保留安全文件名，防目录穿越。
func sanitizeAssetName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if name != filepath.Base(name) {
		return ""
	}
	return name
}

// downloadTo 下载 url 到 dst，返回字节数。
func downloadTo(url, dst string) (int64, error) {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, resp.Body)
	cerr := f.Close()
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return 0, cerr
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return n, nil
}

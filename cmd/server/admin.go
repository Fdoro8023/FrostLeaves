package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// ========== 管理接口保护 ==========
//
// 规则（见 CHANGES.md）：
//   - 来自 127.0.0.1 / ::1 的请求（本机 GUI、本机浏览器）直接放行；
//   - 远程请求默认一律拒绝；只有 admin_allow_remote=true 且携带
//     X-Admin-Token 等于 config 里的 admin_secret 时才放行。

// isLoopback 判断请求是否来自本机回环地址
func isLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// adminAllowed 校验管理接口访问权限，不允许时已写好响应并返回 false
func adminAllowed(w http.ResponseWriter, r *http.Request, cfg *ServerConfig) bool {
	if isLoopback(r) {
		return true
	}

	if cfg == nil || !cfg.AdminAllowRemote {
		writeJSON(w, 403, map[string]string{
			"error": "admin API is restricted to localhost; set admin_allow_remote=true and send X-Admin-Token to manage remotely",
		})
		return false
	}

	token := r.Header.Get("X-Admin-Token")
	if cfg.AdminSecret == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.AdminSecret)) != 1 {
		writeJSON(w, 403, map[string]string{"error": "invalid admin token"})
		return false
	}
	return true
}

// ========== 密钥生成 ==========

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// 极端情况下退化为时间戳，绝不返回空密钥
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ensureSecrets 首次运行时生成随机密钥并写回 config.json
func ensureSecrets(cfg *ServerConfig, path string) {
	changed := false

	if cfg.AuthSecret == "" || cfg.AuthSecret == "change-this-secret-in-production" {
		cfg.AuthSecret = randomHex(32)
		changed = true
		log.Printf("[Security] generated new auth_secret (device token signing key)")
	}
	if cfg.AdminSecret == "" {
		cfg.AdminSecret = randomHex(24)
		changed = true
		log.Printf("[Security] generated new admin_secret (X-Admin-Token for remote admin access)")
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data"
		changed = true
	}

	if changed {
		if err := saveConfig(path, *cfg); err != nil {
			log.Printf("[Security] failed to persist generated secrets: %v", err)
		} else {
			log.Printf("[Security] secrets saved to %s — keep this file private", path)
		}
	}

	if cfg.HTTPBindAddr == "0.0.0.0" && !cfg.EnableTLS {
		log.Printf("[Security] WARNING: binding 0.0.0.0 without TLS — traffic (including tokens) is plaintext on the LAN")
	}
	if cfg.AuthSecret != "" && len(cfg.AuthSecret) < 32 {
		log.Printf("[Security] WARNING: auth_secret is shorter than 32 chars; consider regenerating it")
	}
}

// ========== 配置校验 ==========

// validateNewConfig 校验通过 /api/v1/system/config 提交的新配置
func validateNewConfig(c ServerConfig) error {
	ports := []struct {
		name string
		port int
	}{
		{"http_port", c.HTTPPort},
		{"auth_port", c.AuthPort},
		{"web_port", c.WebPort},
	}
	for _, p := range ports {
		if p.port < 1 || p.port > 65535 {
			return fmt.Errorf("%s out of range: %d", p.name, p.port)
		}
	}
	if c.HTTPPort == c.AuthPort || c.HTTPPort == c.WebPort || c.AuthPort == c.WebPort {
		return fmt.Errorf("ports must be distinct (http=%d auth=%d web=%d)", c.HTTPPort, c.AuthPort, c.WebPort)
	}

	if ip := net.ParseIP(c.HTTPBindAddr); ip == nil && !strings.EqualFold(c.HTTPBindAddr, "localhost") {
		return fmt.Errorf("http_bind_addr is not a valid IP: %q", c.HTTPBindAddr)
	}
	if strings.TrimSpace(c.StorageRoot) == "" {
		return fmt.Errorf("storage_root must not be empty")
	}
	if c.EnableTLS && (c.TLSCertPath == "" || c.TLSKeyPath == "") {
		return fmt.Errorf("enable_tls requires tls_cert_path and tls_key_path")
	}

	limits := []struct {
		name string
		val  int64
	}{
		{"max_upload_bytes", c.MaxUploadBytes},
		{"upload_speed_kbps", c.UploadSpeedKBPS},
		{"download_speed_kbps", c.DownloadSpeedKBPS},
		{"max_file_size_bytes", c.MaxFileSizeBytes},
	}
	for _, l := range limits {
		if l.val < 0 {
			return fmt.Errorf("%s must be >= 0", l.name)
		}
	}
	return nil
}

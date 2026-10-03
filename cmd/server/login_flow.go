package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"frostleaves/pkg/file_gateway"
)

// smtpConfigured reports whether the email service is ready to deliver
// verification codes (item 4: force-email-login requires a working SMTP).
func smtpConfigured() bool {
	if emailStore == nil {
		return false
	}
	c := emailStore.Get()
	return c.Enabled && strings.TrimSpace(c.Host) != "" && c.Port > 0 && strings.TrimSpace(c.FromEmail) != ""
}

// setupLoginFlowRoutes registers the client-facing login-mode and connect
// endpoints (item 4).
//
//	GET  /api/v1/login-mode        -> is forced email login on? is SMTP ready?
//	POST /api/v1/device/connect    -> approved device connects without a code
//	POST /api/v1/auth/email/login  -> email + code login (auto-approves device)
func setupLoginFlowRoutes(
	mux *http.ServeMux,
	dm *DeviceManager,
	auth *file_gateway.Authenticator,
	audit *file_gateway.AuditLogger,
	cfg *ServerConfig,
) {
	// ---- client: ask which login mode the server uses ----
	mux.HandleFunc("/api/v1/login-mode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		serverName, serverVersion := "", AppVersion
		if cfg != nil {
			serverName = cfg.ServerName
		}
		force := cfg != nil && cfg.ForceEmailLogin
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"force_email_login": force,
				"smtp_configured":   smtpConfigured(),
				"server_name":       serverName,
				"server_version":    serverVersion,
			},
		})
	})

	// ---- client: approved device connects without a verification code ----
	mux.HandleFunc("/api/v1/device/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		// 需求 1：客户端版本必须与服务端完全一致
		if !checkClientVersion(w, r, "") {
			return
		}
		var req struct {
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.DeviceID) == "" {
			writeJSON(w, 400, map[string]string{"error": "device_id required"})
			return
		}
		deviceID := strings.TrimSpace(req.DeviceID)
		// 强制邮箱登录模式下：设备必须已完成邮箱登录（已绑定邮箱账号）才能接入；
		// 但依然需要管理员在【设备管理-待审核】审核通过（下面 switch 分支）。
		if cfg != nil && cfg.ForceEmailLogin {
			linked := policyStore != nil && policyStore.AccountOf(deviceID) != ""
			if !linked {
				writeJSON(w, 403, map[string]string{
					"error": "server requires email login",
					"code":  "email_login_required",
				})
				return
			}
		}
		d, err := dm.GetDevice(deviceID)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "device not registered", "code": "unknown"})
			return
		}
		switch d.Status {
		case StatusBlacklisted:
			writeJSON(w, 403, map[string]string{"error": "device is blacklisted", "code": "blacklisted"})
			return
		case StatusRejected:
			writeJSON(w, 403, map[string]string{"error": "device was rejected", "code": "rejected"})
			return
		case StatusPending:
			writeJSON(w, 403, map[string]string{"error": "device is not approved yet", "code": "pending"})
			return
		}
		// approved / connected / disconnected -> issue a token (no code needed)
		token := auth.RegisterDevice(deviceID, d.Name, d.Platform)
		_ = dm.MarkVerified(deviceID)
		audit.Log(deviceID, "device_connect", "", "success", "approved without code", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"token":      token,
				"tailnet_ip": "",
			},
		})
	})

	// ---- client: email + verification code login (forced mode) ----
	mux.HandleFunc("/api/v1/auth/email/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if cfg == nil || !cfg.ForceEmailLogin {
			writeJSON(w, 403, map[string]string{"error": "email login is disabled on this server"})
			return
		}
		if emailService == nil {
			writeJSON(w, 503, map[string]string{"error": "email service unavailable"})
			return
		}
		var req struct {
			Email      string `json:"email"`
			Code       string `json:"code"`
			DeviceID   string `json:"device_id"`
			DeviceName string `json:"device_name"`
			Platform   string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))
		deviceID := strings.TrimSpace(req.DeviceID)
		if email == "" || deviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "email and device_id are required"})
			return
		}
		// 需求 1：客户端版本必须与服务端完全一致
		if !checkClientVersion(w, r, req.Platform) {
			return
		}
		if err := emailService.VerifyCode(email, strings.TrimSpace(req.Code)); err != nil {
			audit.Log(deviceID, "email_login", email, "denied", err.Error(), r.RemoteAddr)
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		// 注册/刷新设备，但**不直接放行**：仍需管理员在【设备管理-待审核】审核通过。
		if err := dm.ApplyDevice(deviceID, req.DeviceName, req.Platform, ""); err != nil {
			if !strings.Contains(err.Error(), "already connected") {
				audit.Log(deviceID, "email_login", email, "denied", err.Error(), r.RemoteAddr)
				code := "policy_denied"
				if strings.Contains(err.Error(), "blacklisted") {
					code = "blacklisted"
				}
				writeJSON(w, 403, map[string]string{"error": err.Error(), "code": code})
				return
			}
		}
		// item 5 关联：把邮箱作为「账号 ID」，供账号级权限与归属账号展示使用
		if policyStore != nil {
			if err := policyStore.SetDeviceAccount(deviceID, email); err != nil {
				fmt.Printf("[Login] link device %s to account %s failed: %v\n", deviceID, email, err)
			}
		}
		status := "pending"
		if d, derr := dm.GetDevice(deviceID); derr == nil && d != nil {
			status = string(d.Status)
		}
		audit.Log(deviceID, "email_login", email, "success", "awaiting approval", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"status":            status,
				"awaiting_approval": true,
				"email":             email,
				"account_id":        email,
			},
		})
	})
}

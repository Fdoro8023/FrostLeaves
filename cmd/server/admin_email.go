package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"frostleaves/pkg/email"
	"frostleaves/pkg/file_gateway"
)

// emailStore / emailService implement the email-registration module (模块 5).
var (
	emailStore   *email.Store
	emailService *email.Service
)

// clientIP extracts the caller IP from a request.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// setupEmailRoutes registers the public registration endpoints and the admin
// configuration endpoints for module 5.
func setupEmailRoutes(mux *http.ServeMux, cfg *ServerConfig, audit *file_gateway.AuditLogger) {
	adminGuard := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(w, r, cfg) {
				return
			}
			h(w, r)
		}
	}

	// ---- public: image captcha ----
	mux.HandleFunc("/api/v1/auth/captcha", func(w http.ResponseWriter, r *http.Request) {
		if emailService == nil {
			writeJSON(w, 503, map[string]string{"error": "email service unavailable"})
			return
		}
		c, err := emailService.NewCaptcha()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "captcha generation failed"})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"id":    c.ID,
				"image": "data:image/png;base64," + base64.StdEncoding.EncodeToString(c.Image),
			},
		})
	})

	// ---- public: request a verification code ----
	mux.HandleFunc("/api/v1/auth/email/request", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if emailService == nil {
			writeJSON(w, 503, map[string]string{"error": "email service unavailable"})
			return
		}
		var body struct {
			CaptchaID   string `json:"captcha_id"`
			CaptchaCode string `json:"captcha_code"`
			Email       string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		err := emailService.RequestCode(body.CaptchaID, body.CaptchaCode, body.Email, clientIP(r))
		switch {
		case err == nil:
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
		case errors.Is(err, email.ErrDisabled):
			writeJSON(w, 403, map[string]string{"error": err.Error()})
		case errors.Is(err, email.ErrCaptcha):
			writeJSON(w, 400, map[string]string{"error": err.Error()})
		case errors.Is(err, email.ErrSendFailed):
			writeJSON(w, 502, map[string]string{"error": "verification email could not be sent"})
		default:
			var rl *email.RateLimitedError
			if errors.As(err, &rl) {
				writeJSON(w, 429, map[string]string{"error": rl.Error()})
				return
			}
			writeJSON(w, 400, map[string]string{"error": err.Error()})
		}
	})

	// ---- public: verify a code ----
	mux.HandleFunc("/api/v1/auth/email/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if emailService == nil {
			writeJSON(w, 503, map[string]string{"error": "email service unavailable"})
			return
		}
		var body struct {
			Email string `json:"email"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := emailService.VerifyCode(body.Email, body.Code); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "verified"})
	})

	// ---- admin: configuration ----
	mux.HandleFunc("/api/v1/admin/email/config", adminGuard(func(w http.ResponseWriter, r *http.Request) {
		if emailStore == nil {
			writeJSON(w, 500, map[string]string{"error": "email store not initialized"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": emailStore.Get().Redacted()})
		case http.MethodPost:
			current := emailStore.Get()
			var incoming email.Config
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid body"})
				return
			}
			// keep the stored password when the client sends none
			if incoming.Password == "" {
				incoming.Password = current.Password
			}
			if incoming.Enabled {
				if err := incoming.Validate(); err != nil {
					writeJSON(w, 400, map[string]string{"error": err.Error()})
					return
				}
			}
			if err := emailStore.Set(incoming); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			audit.Log("admin", "email_config_change", "", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": emailStore.Get().Redacted()})
		default:
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		}
	}))

	mux.HandleFunc("/api/v1/admin/email/providers", adminGuard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"all":       email.Presets,
				"by_region": email.PresetsByRegion(),
			},
		})
	}))

	mux.HandleFunc("/api/v1/admin/email/preset", adminGuard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if emailStore == nil {
			writeJSON(w, 500, map[string]string{"error": "email store not initialized"})
			return
		}
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Provider == "" {
			writeJSON(w, 400, map[string]string{"error": "provider is required"})
			return
		}
		c := emailStore.Get()
		if !c.ApplyPreset(body.Provider) {
			writeJSON(w, 404, map[string]string{"error": "unknown provider preset"})
			return
		}
		if err := emailStore.Set(c); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "email_config_change", "preset:"+body.Provider, "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": c.Redacted()})
	}))
}

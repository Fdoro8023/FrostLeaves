package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"frostleaves/pkg/file_gateway"
	"frostleaves/pkg/profile"
)

// profileStore holds personalization state (模块 4).
var profileStore *profile.Store

// deviceFromRequest authenticates a device request using the gateway tokens.
func deviceFromRequest(r *http.Request, auth *file_gateway.Authenticator) (string, bool) {
	deviceID := r.Header.Get("X-Device-Id")
	token := r.Header.Get("X-Device-Token")
	if deviceID == "" || token == "" || auth == nil {
		return "", false
	}
	ctx, err := auth.Validate(deviceID, token)
	if err != nil || ctx == nil {
		return "", false
	}
	return ctx.DeviceID, true
}

// setupPersonalizationRoutes registers the personalization API (模块 4).
func setupPersonalizationRoutes(mux *http.ServeMux, cfg *ServerConfig, audit *file_gateway.AuditLogger, auth *file_gateway.Authenticator) {
	adminGuard := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(w, r, cfg) {
				return
			}
			h(w, r)
		}
	}

	// ---- server profile (shown on every client home) ----
	mux.HandleFunc("/api/v1/profile/server", func(w http.ResponseWriter, r *http.Request) {
		if profileStore == nil {
			writeJSON(w, 503, map[string]string{"error": "profile store unavailable"})
			return
		}
		s := profileStore.Server()
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"name":       s.Name,
				"avatar_url": avatarURL(s.AvatarFile, "/api/v1/avatars/server"),
				"revision":   s.Revision,
			},
		})
	})

	// ---- device profile (device-authenticated) ----
	mux.HandleFunc("/api/v1/profile/device", func(w http.ResponseWriter, r *http.Request) {
		deviceID, ok := deviceFromRequest(r, auth)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "missing or invalid device credentials"})
			return
		}
		if profileStore == nil {
			writeJSON(w, 503, map[string]string{"error": "profile store unavailable"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": deviceProfileView(deviceID)})
		case http.MethodPost:
			var body struct {
				DisplayName string `json:"display_name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid body"})
				return
			}
			if err := profileStore.SetDeviceName(deviceID, body.DisplayName); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			audit.Log(deviceID, "profile_update", "display_name", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": deviceProfileView(deviceID)})
		default:
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		}
	})

	// ---- device avatar upload (multipart "avatar") ----
	mux.HandleFunc("/api/v1/profile/device/avatar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		deviceID, ok := deviceFromRequest(r, auth)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "missing or invalid device credentials"})
			return
		}
		if profileStore == nil {
			writeJSON(w, 503, map[string]string{"error": "profile store unavailable"})
			return
		}
		data, err := readAvatarUpload(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if _, err := profileStore.SetDeviceAvatar(deviceID, data); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		audit.Log(deviceID, "profile_avatar_update", "", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": deviceProfileView(deviceID)})
	})

	// ---- serve avatars ----
	mux.HandleFunc("/api/v1/avatars/server", func(w http.ResponseWriter, r *http.Request) {
		serveAvatar(w, r, profile.ServerOwner)
	})
	mux.HandleFunc("/api/v1/avatars/device/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/avatars/device/")
		if id == "" {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		serveAvatar(w, r, id)
	})

	// ---- admin: server profile ----
	mux.HandleFunc("/api/v1/admin/server-profile", adminGuard(func(w http.ResponseWriter, r *http.Request) {
		if profileStore == nil {
			writeJSON(w, 500, map[string]string{"error": "profile store not initialized"})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := profileStore.SetServerName(body.Name); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "server_profile_update", "name", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": serverProfileView()})
	}))

	mux.HandleFunc("/api/v1/admin/server-profile/avatar", adminGuard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if profileStore == nil {
			writeJSON(w, 500, map[string]string{"error": "profile store not initialized"})
			return
		}
		data, err := readAvatarUpload(r)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if _, err := profileStore.SetServerAvatar(data); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "server_profile_avatar_update", "", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": serverProfileView()})
	}))
}

// avatarURL returns the URL when an avatar exists, otherwise "".
func avatarURL(file, url string) string {
	if file == "" {
		return ""
	}
	return url
}

func deviceProfileView(deviceID string) map[string]interface{} {
	p := profileStore.Device(deviceID)
	return map[string]interface{}{
		"device_id":    deviceID,
		"display_name": p.DisplayName,
		"avatar_url":   avatarURL(p.AvatarFile, "/api/v1/avatars/device/"+deviceID),
	}
}

func serverProfileView() map[string]interface{} {
	s := profileStore.Server()
	return map[string]interface{}{
		"name":       s.Name,
		"avatar_url": avatarURL(s.AvatarFile, "/api/v1/avatars/server"),
		"revision":   s.Revision,
	}
}

// readAvatarUpload reads a multipart avatar field, enforcing the size cap.
func readAvatarUpload(r *http.Request) ([]byte, error) {
	if err := r.ParseMultipartForm(profile.MaxAvatarBytes + 1024); err != nil {
		return nil, err
	}
	file, _, err := r.FormFile("avatar")
	if err != nil {
		return nil, errOr("missing avatar field")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, profile.MaxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > profile.MaxAvatarBytes {
		return nil, errOr("avatar too large")
	}
	return data, nil
}

type avatarError string

func (e avatarError) Error() string { return string(e) }

func errOr(msg string) error { return avatarError(msg) }

// serveAvatar streams a stored avatar with its content type.
func serveAvatar(w http.ResponseWriter, r *http.Request, owner string) {
	if profileStore == nil {
		writeJSON(w, 503, map[string]string{"error": "profile store unavailable"})
		return
	}
	data, mime, ok := profileStore.AvatarData(owner)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "no avatar"})
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=60")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

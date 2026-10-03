package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"frostleaves/pkg/file_gateway"
	"frostleaves/pkg/policy"
)

// policyStore holds the three-level authorization policy (模块 3). It is
// created during start-up and shared by the file gateway and this admin API.
var policyStore *policy.Store

// setupPolicyRoutes registers the server-side policy / session / audit admin
// API. Every handler is guarded by adminAllowed (localhost or X-Admin-Token).
func setupPolicyRoutes(
	mux *http.ServeMux,
	cfg *ServerConfig,
	audit *file_gateway.AuditLogger,
	auth *file_gateway.Authenticator,
	dm *DeviceManager,
	quota *file_gateway.QuotaManager,
) {
	guard := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(w, r, cfg) {
				return
			}
			h(w, r)
		}
	}
	ready := func(w http.ResponseWriter) bool {
		if policyStore == nil {
			writeJSON(w, 500, map[string]string{"error": "policy store not initialized"})
			return false
		}
		return true
	}

	// ---- global policy ----
	mux.HandleFunc("/api/v1/admin/policy/global", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": policyStore.Global()})
		case http.MethodPost:
			var p policy.Policy
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid body"})
				return
			}
			if err := policyStore.SetGlobal(p); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			audit.Log("admin", "policy_change", "global", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": policyStore.Global()})
		default:
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		}
	}))

	// ---- account overrides ----
	mux.HandleFunc("/api/v1/admin/policy/accounts", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": policyStore.Accounts()})
	}))

	mux.HandleFunc("/api/v1/admin/policy/account", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			ID       string           `json:"id"`
			Override *policy.Override `json:"override"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id is required"})
			return
		}
		if err := policyStore.SetAccountOverride(body.ID, body.Override); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "policy_change", "account:"+body.ID, "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	}))

	// ---- device overrides ----
	mux.HandleFunc("/api/v1/admin/policy/devices", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": policyStore.Devices()})
	}))

	mux.HandleFunc("/api/v1/admin/policy/device", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			ID       string           `json:"id"`
			Override *policy.Override `json:"override"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "id is required"})
			return
		}
		if err := policyStore.SetDeviceOverride(body.ID, body.Override); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "policy_change", "device:"+body.ID, "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	}))

	// ---- device -> account assignment ----
	mux.HandleFunc("/api/v1/admin/policy/device-account", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			DeviceID  string `json:"device_id"`
			AccountID string `json:"account_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "device_id is required"})
			return
		}
		if err := policyStore.SetDeviceAccount(body.DeviceID, body.AccountID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "policy_change", "device-account:"+body.DeviceID, "success", body.AccountID, r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	}))

	// ---- effective policy for a device ----
	mux.HandleFunc("/api/v1/admin/policy/effective", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready(w) {
			return
		}
		deviceID := r.URL.Query().Get("device")
		if deviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "device query parameter is required"})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"device_id":  deviceID,
				"account_id": policyStore.AccountOf(deviceID),
				"policy":     policyStore.Resolve(deviceID),
			},
		})
	}))

	// ---- device sessions (模块 3-8) ----
	mux.HandleFunc("/api/v1/admin/sessions", guard(func(w http.ResponseWriter, r *http.Request) {
		items := auth.ActiveTokens()
		out := make([]map[string]interface{}, 0, len(items))
		for _, t := range items {
			accountID := ""
			if policyStore != nil {
				accountID = policyStore.AccountOf(t.DeviceID)
			}
			out = append(out, map[string]interface{}{
				"device_id":   t.DeviceID,
				"device_name": t.DeviceName,
				"platform":    t.Platform,
				"role":        t.Role,
				"expires_at":  t.ExpiresAt,
				"account_id":  accountID,
			})
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": map[string]interface{}{"items": out, "count": len(out)}})
	}))

	mux.HandleFunc("/api/v1/admin/sessions/logout", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			DeviceID string `json:"device_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DeviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "device_id is required"})
			return
		}
		auth.RevokeDevice(body.DeviceID)
		audit.Log("admin", "device_force_logout", body.DeviceID, "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	}))

	// ---- audit query + retention (模块 3-9) ----
	mux.HandleFunc("/api/v1/admin/audit", guard(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		days := atoiDefault(q.Get("days"), 7)
		page := atoiDefault(q.Get("page"), 1)
		size := atoiDefault(q.Get("page_size"), 100)
		end := time.Now()
		start := end.AddDate(0, 0, -days)
		entries, total := audit.Query(q.Get("device"), q.Get("action"), start, end, page, size)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"items":          entries,
				"total":          total,
				"retention_days": auditRetentionDays(),
			},
		})
	}))

	mux.HandleFunc("/api/v1/admin/audit/prune", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		days := auditRetentionDays()
		removed, err := audit.PruneRetention(days, time.Now())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "audit_prune", "", "success", "removed="+strconv.Itoa(removed), r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": map[string]interface{}{"removed": removed, "retention_days": days}})
	}))

	_ = dm
	_ = quota

	// 模块 7：离线数据保留状态/触发
	setupRetentionRoutes(mux, cfg, audit, auth, dm)
}

// auditRetentionDays returns the configured audit retention (模块 3-9).
func auditRetentionDays() int {
	if policyStore == nil {
		return 30
	}
	if d := policyStore.Global().AuditRetentionDays; d > 0 {
		return d
	}
	return 30
}

// atoiDefault parses s, returning def when empty or invalid.
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}

package main

import (
	"log"
	"net/http"
	"time"

	"frostleaves/pkg/file_gateway"
	"frostleaves/pkg/retention"
)

// activeGateway is the running file gateway, used by retention jobs/handlers.
var activeGateway *file_gateway.FileGateway

// offlineDeviceStates builds presence info for every known device.
func offlineDeviceStates(dm *DeviceManager, auth *file_gateway.Authenticator) []retention.DeviceState {
	online := map[string]bool{}
	if auth != nil {
		for _, t := range auth.ActiveTokens() {
			online[t.DeviceID] = true
		}
	}
	var states []retention.DeviceState
	if dm == nil {
		return states
	}
	for _, d := range dm.ListDevices("") {
		if d == nil {
			continue
		}
		st := retention.DeviceState{DeviceID: d.ID, Online: online[d.ID]}
		if d.LastSeenAt != nil {
			st.LastSeen = *d.LastSeenAt
		}
		states = append(states, st)
	}
	return states
}

// retentionDecisions returns the current purge decisions (admin display).
func retentionDecisions(dm *DeviceManager, auth *file_gateway.Authenticator) []retention.Decision {
	if policyStore == nil {
		return nil
	}
	return retention.Evaluate(policyStore, offlineDeviceStates(dm, auth), time.Now())
}

// runOfflineRetention evaluates the retention policy and purges the private
// data of expired offline devices, returning how many devices were purged.
func runOfflineRetention(dm *DeviceManager, auth *file_gateway.Authenticator, audit *file_gateway.AuditLogger) int {
	if policyStore == nil || activeGateway == nil {
		return 0
	}
	purged := 0
	for _, d := range retentionDecisions(dm, auth) {
		if !d.Purge {
			continue
		}
		if err := activeGateway.PurgeDeviceData(d.DeviceID); err != nil {
			log.Printf("[Retention] purge %s failed: %v", d.DeviceID, err)
			continue
		}
		if audit != nil {
			audit.Log("system", "offline_data_purge", d.DeviceID, "success", d.Reason, "")
		}
		log.Printf("[Retention] purged private data of offline device %s (%s)", d.DeviceID, d.Reason)
		purged++
	}
	return purged
}

// offlineRetentionLoop sweeps once at start-up and then daily (模块 7).
func offlineRetentionLoop(dm *DeviceManager, auth *file_gateway.Authenticator, audit *file_gateway.AuditLogger) {
	runOfflineRetention(dm, auth, audit)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		log.Printf("[Retention] running offline data retention sweep...")
		runOfflineRetention(dm, auth, audit)
	}
}

// setupRetentionRoutes registers the admin API for offline data retention.
func setupRetentionRoutes(mux *http.ServeMux, cfg *ServerConfig, audit *file_gateway.AuditLogger, auth *file_gateway.Authenticator, dm *DeviceManager) {
	guard := func(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !adminAllowed(w, r, cfg) {
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("/api/v1/admin/retention/status", guard(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": map[string]interface{}{"items": retentionDecisions(dm, auth)}})
	}))
	mux.HandleFunc("/api/v1/admin/retention/run", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		purged := runOfflineRetention(dm, auth, audit)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": map[string]interface{}{"purged": purged}})
	}))
}

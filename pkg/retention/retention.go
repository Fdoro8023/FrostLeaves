// Package retention decides when an offline device's private data must be
// purged under the configured retention policy (模块 7).
package retention

import (
	"time"

	"frostleaves/pkg/policy"
)

// DeviceState is the minimal presence information needed for a decision.
type DeviceState struct {
	DeviceID string
	Online   bool
	LastSeen time.Time
}

// Decision is the outcome for one device.
type Decision struct {
	DeviceID string `json:"device_id"`
	Purge    bool   `json:"purge"`
	Reason   string `json:"reason"`
}

// Evaluate returns the purge decisions for the given devices. A device is
// purged only when it is offline and its retention window has elapsed; an
// online (rejoined) device is always retained.
func Evaluate(pol *policy.Store, devices []DeviceState, now time.Time) []Decision {
	out := make([]Decision, 0, len(devices))
	for _, d := range devices {
		out = append(out, Decide(pol, d, now))
	}
	return out
}

// Decide evaluates a single device.
func Decide(pol *policy.Store, d DeviceState, now time.Time) Decision {
	if d.DeviceID == "" {
		return Decision{DeviceID: d.DeviceID, Purge: false, Reason: "missing device id"}
	}
	if d.Online {
		return Decision{DeviceID: d.DeviceID, Purge: false, Reason: "device online"}
	}

	p := policy.DefaultPolicy()
	if pol != nil {
		p = pol.Resolve(d.DeviceID)
	}

	if !p.RetainOfflineData {
		return Decision{DeviceID: d.DeviceID, Purge: true, Reason: "offline data retention disabled"}
	}
	if p.OfflineRetentionDays <= 0 {
		return Decision{DeviceID: d.DeviceID, Purge: false, Reason: "retention unlimited"}
	}
	if d.LastSeen.IsZero() {
		return Decision{DeviceID: d.DeviceID, Purge: false, Reason: "last seen unknown"}
	}
	deadline := d.LastSeen.AddDate(0, 0, p.OfflineRetentionDays)
	if now.After(deadline) {
		return Decision{DeviceID: d.DeviceID, Purge: true, Reason: "retention window expired"}
	}
	return Decision{DeviceID: d.DeviceID, Purge: false, Reason: "within retention window"}
}

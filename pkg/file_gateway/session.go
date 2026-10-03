package file_gateway

import "time"

// ActiveToken is a device with a currently valid token (在线设备，模块 3-8).
type ActiveToken struct {
	DeviceID   string    `json:"device_id"`
	DeviceName string    `json:"device_name"`
	Platform   string    `json:"platform"`
	Role       string    `json:"role"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ActiveTokens lists devices that currently hold a valid token.
func (a *Authenticator) ActiveTokens() []ActiveToken {
	a.mu.RLock()
	defer a.mu.RUnlock()
	now := time.Now()
	out := make([]ActiveToken, 0, len(a.tokens))
	for id, e := range a.tokens {
		if e == nil || now.After(e.ExpiresAt) {
			continue
		}
		out = append(out, ActiveToken{
			DeviceID:   id,
			DeviceName: e.DeviceName,
			Platform:   e.Platform,
			Role:       e.Role,
			ExpiresAt:  e.ExpiresAt,
		})
	}
	return out
}

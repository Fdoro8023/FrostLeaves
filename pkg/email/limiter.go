package email

import (
	"fmt"
	"sync"
	"time"
)

// Limiter enforces the anti-abuse rate limits: per email 1/minute and
// 5/hour, per IP 3/minute.
type Limiter struct {
	mu      sync.Mutex
	byEmail map[string][]time.Time
	byIP    map[string][]time.Time
	now     func() time.Time
}

// NewLimiter returns a limiter. now defaults to time.Now.
func NewLimiter(now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{byEmail: map[string][]time.Time{}, byIP: map[string][]time.Time{}, now: now}
}

// AllowAndRecord checks the limits and, when allowed, records the request.
// It returns a human-readable reason when denied.
func (l *Limiter) AllowAndRecord(emailAddr, ip string, cfg Config) (bool, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	hourAgo := now.Add(-time.Hour)
	minuteAgo := now.Add(-time.Minute)

	emails := prune(l.byEmail[emailAddr], hourAgo)
	l.byEmail[emailAddr] = emails
	if cfg.EmailPerHour > 0 && len(emails) >= cfg.EmailPerHour {
		return false, "email hourly limit reached"
	}
	if cfg.EmailPerMinute > 0 && countAfter(emails, minuteAgo) >= cfg.EmailPerMinute {
		return false, "email per-minute limit reached"
	}

	ips := prune(l.byIP[ip], hourAgo)
	l.byIP[ip] = ips
	if cfg.IPPerMinute > 0 && countAfter(ips, minuteAgo) >= cfg.IPPerMinute {
		return false, fmt.Sprintf("ip per-minute limit reached (%s)", ip)
	}

	l.byEmail[emailAddr] = append(l.byEmail[emailAddr], now)
	l.byIP[ip] = append(l.byIP[ip], now)
	return true, ""
}

func prune(ts []time.Time, cutoff time.Time) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	return out
}

func countAfter(ts []time.Time, cutoff time.Time) int {
	n := 0
	for _, t := range ts {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

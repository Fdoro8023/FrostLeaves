package retention

import (
	"testing"
	"time"

	"frostleaves/pkg/policy"
)

func pb(v bool) *bool { return &v }
func pi(v int) *int   { return &v }

func newStore(t *testing.T) *policy.Store {
	t.Helper()
	s := policy.NewStore()
	p := policy.DefaultPolicy()
	p.RetainOfflineData = true
	p.OfflineRetentionDays = 30
	if err := s.SetGlobal(p); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestOnlineDeviceIsAlwaysRetained(t *testing.T) {
	s := newStore(t)
	d := Decide(s, DeviceState{DeviceID: "d1", Online: true, LastSeen: time.Now().AddDate(0, 0, -400)}, time.Now())
	if d.Purge {
		t.Fatalf("online device must never be purged: %+v", d)
	}
}

func TestOfflineWithinWindowIsRetained(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	d := Decide(s, DeviceState{DeviceID: "d1", LastSeen: now.AddDate(0, 0, -10)}, now)
	if d.Purge {
		t.Fatalf("device within retention window must be kept: %+v", d)
	}
}

func TestOfflinePastWindowIsPurged(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	d := Decide(s, DeviceState{DeviceID: "d1", LastSeen: now.AddDate(0, 0, -45)}, now)
	if !d.Purge {
		t.Fatalf("device past retention window must be purged: %+v", d)
	}
}

func TestRejoinWithinWindowPreventsPurge(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	// rejoined: online again, so the old last-seen no longer matters
	d := Decide(s, DeviceState{DeviceID: "d1", Online: true, LastSeen: now.AddDate(0, 0, -45)}, now)
	if d.Purge {
		t.Fatalf("rejoined device must be kept: %+v", d)
	}
}

func TestRetentionDisabledPurgesOfflineDevice(t *testing.T) {
	s := newStore(t)
	if err := s.SetGlobal(func() policy.Policy {
		p := policy.DefaultPolicy()
		p.RetainOfflineData = false
		return p
	}()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d := Decide(s, DeviceState{DeviceID: "d1", LastSeen: now}, now)
	if !d.Purge {
		t.Fatalf("offline device must be purged when retention is disabled: %+v", d)
	}
}

func TestDeviceOverrideBeatsGlobal(t *testing.T) {
	s := newStore(t)
	if err := s.SetDeviceOverride("d1", &policy.Override{OfflineRetentionDays: pi(1)}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// global allows 30 days, device only 1 -> 10 days offline must purge
	d := Decide(s, DeviceState{DeviceID: "d1", LastSeen: now.AddDate(0, 0, -10)}, now)
	if !d.Purge {
		t.Fatalf("device override must tighten the window: %+v", d)
	}
}

func TestAccountOverrideRetainsForDeveloper(t *testing.T) {
	s := newStore(t)
	if err := s.SetAccountOverride("dev", &policy.Override{RetainOfflineData: pb(true), OfflineRetentionDays: pi(0)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDeviceAccount("d1", "dev"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	d := Decide(s, DeviceState{DeviceID: "d1", LastSeen: now.AddDate(0, 0, -999)}, now)
	if d.Purge {
		t.Fatalf("account override with unlimited retention must keep data: %+v", d)
	}
}

func TestUnknownLastSeenIsRetained(t *testing.T) {
	s := newStore(t)
	d := Decide(s, DeviceState{DeviceID: "d1"}, time.Now())
	if d.Purge {
		t.Fatalf("unknown last-seen must not trigger a purge: %+v", d)
	}
}

func TestEvaluateReturnsOneDecisionPerDevice(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	got := Evaluate(s, []DeviceState{
		{DeviceID: "a", Online: true, LastSeen: now},
		{DeviceID: "b", LastSeen: now.AddDate(0, 0, -60)},
	}, now)
	if len(got) != 2 {
		t.Fatalf("want 2 decisions, got %d", len(got))
	}
	if got[0].Purge || !got[1].Purge {
		t.Fatalf("unexpected decisions: %+v", got)
	}
}

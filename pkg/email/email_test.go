package email

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"frostleaves/pkg/captcha"
)

type fakeSender struct {
	mu   sync.Mutex
	sent []Message
	err  error
}

func (f *fakeSender) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakeSender) last() (Message, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return Message{}, false
	}
	return f.sent[len(f.sent)-1], true
}

type auditRec struct {
	action string
	result string
	detail string
	ip     string
}

type fakeAudit struct {
	mu   sync.Mutex
	recs []auditRec
}

func (a *fakeAudit) Log(_deviceID, action, _resourcePath, result, detail, ipAddress string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recs = append(a.recs, auditRec{action: action, result: result, detail: detail, ip: ipAddress})
}

func (a *fakeAudit) has(action string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range a.recs {
		if r.action == action {
			return true
		}
	}
	return false
}

func newTestService(t *testing.T) (*Service, *fakeSender, *fakeAudit) {
	t.Helper()
	store := NewStore()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Host = "smtp.example.com"
	cfg.Port = 465
	cfg.Security = "ssl"
	cfg.FromEmail = "noreply@example.com"
	if err := store.Set(cfg); err != nil {
		t.Fatal(err)
	}
	sender := &fakeSender{}
	audit := &fakeAudit{}
	svc := NewService(store, captcha.NewManager(time.Minute, 4), audit)
	svc.SetSenderFactory(func(Config) Sender { return sender })
	return svc, sender, audit
}

func TestPresetsContainRequiredProviders(t *testing.T) {
	want := map[string]struct {
		host string
		port int
		sec  string
	}{
		"qq":         {"smtp.qq.com", 465, "ssl"},
		"163":        {"smtp.163.com", 465, "ssl"},
		"126":        {"smtp.126.com", 465, "ssl"},
		"sina":       {"smtp.sina.com", 465, "ssl"},
		"wecom":      {"smtp.work.weixin.qq.com", 465, "ssl"},
		"aliyun":     {"smtp.mxhichina.com", 465, "ssl"},
		"icloud":     {"smtp.mail.me.com", 587, "starttls"},
		"outlook":    {"smtp.office365.com", 587, "starttls"},
		"gmail":      {"smtp.gmail.com", 587, "starttls"},
		"protonmail": {"smtp.protonmail.ch", 587, "starttls"},
		"yahoo":      {"smtp.mail.yahoo.com", 587, "starttls"},
		"zoho":       {"smtp.zoho.com", 587, "starttls"},
		"fastmail":   {"smtp.fastmail.com", 465, "ssl"},
	}
	for key, exp := range want {
		p, ok := LookupPreset(key)
		if !ok {
			t.Fatalf("missing preset %q", key)
		}
		if p.Host != exp.host || p.Port != exp.port || p.Security != exp.sec {
			t.Fatalf("preset %q = %s:%d/%s, want %s:%d/%s", key, p.Host, p.Port, p.Security, exp.host, exp.port, exp.sec)
		}
	}
}

func TestApplyPresetFillsTransport(t *testing.T) {
	c := DefaultConfig()
	if !c.ApplyPreset("gmail") {
		t.Fatal("ApplyPreset(gmail) should succeed")
	}
	if c.Host != "smtp.gmail.com" || c.Port != 587 || c.Security != "starttls" {
		t.Fatalf("unexpected transport: %+v", c)
	}
	if c.ApplyPreset("nope") {
		t.Fatal("unknown preset must fail")
	}
}

func TestRenderPlaceholders(t *testing.T) {
	subj, body := Render("验证码 {code}", "你的验证码是 {code}，{ttl} 分钟内有效，来自 {server}", "123456", 10, "FrostLeaves", "a@b.com")
	if subj != "验证码 123456" {
		t.Fatalf("subject: %q", subj)
	}
	if !strings.Contains(body, "123456") || !strings.Contains(body, "10") || !strings.Contains(body, "FrostLeaves") {
		t.Fatalf("body placeholders not replaced: %q", body)
	}
}

func TestConfigValidate(t *testing.T) {
	c := DefaultConfig()
	c.FromEmail = "noreply@example.com"
	c.Host = ""
	if err := c.Validate(); err == nil {
		t.Fatal("missing host must fail validation")
	}
	c = DefaultConfig()
	c.FromEmail = "noreply@example.com"
	c.Host = "smtp.example.com"
	c.Port = 465
	if err := c.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
}

func TestStorePersistRoundTripAndRedacted(t *testing.T) {
	path := t.TempDir() + "/email.json"
	s := NewStore()
	s.SetStore(path)
	c := DefaultConfig()
	c.Enabled = true
	c.Host = "smtp.example.com"
	c.Password = "super-secret"
	if err := s.Set(c); err != nil {
		t.Fatal(err)
	}
	s2 := NewStore()
	s2.SetStore(path)
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	got := s2.Get()
	if !got.Enabled || got.Password != "super-secret" {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.Redacted().Password != "" {
		t.Fatal("Redacted must blank the password")
	}
}

func TestLimiterEmailPerMinute(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLimiter(func() time.Time { return now })
	cfg := DefaultConfig()
	if ok, _ := l.AllowAndRecord("a@b.com", "1.2.3.4", cfg); !ok {
		t.Fatal("first request should pass")
	}
	if ok, reason := l.AllowAndRecord("a@b.com", "1.2.3.4", cfg); ok {
		t.Fatalf("second same-minute request must be denied, reason=%q", reason)
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.AllowAndRecord("a@b.com", "1.2.3.4", cfg); !ok {
		t.Fatal("after a minute the email limit should reset")
	}
}

func TestLimiterEmailPerHour(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLimiter(func() time.Time { return now })
	cfg := DefaultConfig()
	cfg.EmailPerMinute = 100
	cfg.IPPerMinute = 100
	for i := 0; i < 5; i++ {
		if ok, _ := l.AllowAndRecord("a@b.com", "9.9.9.9", cfg); !ok {
			t.Fatalf("request %d should pass", i)
		}
	}
	if ok, reason := l.AllowAndRecord("a@b.com", "9.9.9.9", cfg); ok {
		t.Fatalf("6th request in an hour must be denied, reason=%q", reason)
	}
}

func TestLimiterIPPerMinute(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLimiter(func() time.Time { return now })
	cfg := DefaultConfig()
	cfg.EmailPerMinute = 100
	for i := 0; i < 3; i++ {
		if ok, _ := l.AllowAndRecord("u"+string(rune('a'+i))+"@b.com", "5.5.5.5", cfg); !ok {
			t.Fatalf("ip request %d should pass", i)
		}
	}
	if ok, reason := l.AllowAndRecord("other@b.com", "5.5.5.5", cfg); ok {
		t.Fatalf("4th request from one IP must be denied, reason=%q", reason)
	}
}

func TestServiceDisabledByDefault(t *testing.T) {
	store := NewStore()
	svc := NewService(store, captcha.NewManager(time.Minute, 4), nil)
	cap, err := svc.NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(cap.ID, cap.Answer, "a@b.com", "1.2.3.4"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
}

func TestServiceRequestCodeRequiresCaptcha(t *testing.T) {
	svc, sender, audit := newTestService(t)
	cap, err := svc.NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(cap.ID, "0000", "a@b.com", "1.2.3.4"); !errors.Is(err, ErrCaptcha) {
		t.Fatalf("wrong captcha must fail with ErrCaptcha, got %v", err)
	}
	if _, ok := sender.last(); ok {
		t.Fatal("no email may be sent when captcha fails")
	}
	if !audit.has("captcha_failed") {
		t.Fatal("captcha failure must be audited")
	}
}

func TestServiceSendsCodeAndVerifies(t *testing.T) {
	svc, sender, audit := newTestService(t)
	cap, err := svc.NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(cap.ID, cap.Answer, "User@Example.com", "1.2.3.4"); err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	msg, ok := sender.last()
	if !ok {
		t.Fatal("an email should have been sent")
	}
	if msg.To != "user@example.com" {
		t.Fatalf("email should be normalized, got %q", msg.To)
	}
	code := extractCode(msg.Body)
	if len(code) != 6 {
		t.Fatalf("expected a 6-digit code in the body, got %q (body=%q)", code, msg.Body)
	}
	if err := svc.VerifyCode("user@example.com", code); err != nil {
		t.Fatalf("VerifyCode: %v", err)
	}
	if err := svc.VerifyCode("user@example.com", code); !errors.Is(err, ErrBadCode) {
		t.Fatalf("a code must be single-use, got %v", err)
	}
	if !audit.has("email_code_sent") || !audit.has("email_verified") {
		t.Fatal("successful send/verify must be audited")
	}
}

func TestServiceSendFailureIsAuditedAndCodeDiscarded(t *testing.T) {
	svc, sender, audit := newTestService(t)
	sender.err = errors.New("smtp down")
	cap, err := svc.NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(cap.ID, cap.Answer, "a@b.com", "1.2.3.4"); !errors.Is(err, ErrSendFailed) {
		t.Fatalf("expected ErrSendFailed, got %v", err)
	}
	if !audit.has("email_send_failed") {
		t.Fatal("send failure must be audited")
	}
	if err := svc.VerifyCode("a@b.com", "123456"); !errors.Is(err, ErrBadCode) {
		t.Fatalf("no code should remain after a failed send, got %v", err)
	}
}

func TestServiceRejectsInvalidEmail(t *testing.T) {
	svc, _, _ := newTestService(t)
	cap, err := svc.NewCaptcha()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestCode(cap.ID, cap.Answer, "not-an-email", "1.2.3.4"); err == nil {
		t.Fatal("invalid email must be rejected")
	}
}

func TestSMTPSenderBuildPayload(t *testing.T) {
	s := NewSMTPSender(DefaultConfig())
	s.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	payload := string(s.BuildPayload(Message{To: "a@b.com", Subject: "hi", Body: "hello world"}))
	for _, want := range []string{"To: a@b.com", "Subject: hi", "MIME-Version: 1.0", "hello world"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("payload missing %q:\n%s", want, payload)
		}
	}
}

func extractCode(body string) string {
	start := -1
	for i, r := range body {
		if r >= '0' && r <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if i-start >= 6 {
				return body[start:i]
			}
			start = -1
		}
	}
	if start >= 0 && len(body)-start >= 6 {
		return body[start : start+6]
	}
	return ""
}

package email

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"frostleaves/pkg/captcha"
)

// Sentinel errors for the registration flow.
var (
	ErrDisabled   = errors.New("email registration is disabled")
	ErrCaptcha    = errors.New("invalid or expired captcha")
	ErrSendFailed = errors.New("failed to send verification email")
	ErrBadCode    = errors.New("invalid or expired verification code")
)

// RateLimitedError reports a rejected request together with its reason.
type RateLimitedError struct{ Reason string }

func (e *RateLimitedError) Error() string { return "rate limited: " + e.Reason }

// AuditSink receives audit records (satisfied by file_gateway.AuditLogger).
type AuditSink interface {
	Log(deviceID, action, resourcePath, result, detail, ipAddress string)
}

type codeEntry struct {
	code      string
	expiresAt time.Time
	used      bool
}

// Service implements the email-registration verification flow.
type Service struct {
	store     *Store
	captcha   *captcha.Manager
	limiter   *Limiter
	audit     AuditSink
	newSender func(cfg Config) Sender
	now       func() time.Time

	mu    sync.Mutex
	codes map[string]codeEntry
}

// NewService builds the registration service.
func NewService(store *Store, captchaMgr *captcha.Manager, audit AuditSink) *Service {
	return &Service{
		store:     store,
		captcha:   captchaMgr,
		limiter:   NewLimiter(nil),
		audit:     audit,
		newSender: func(cfg Config) Sender { return NewSMTPSender(cfg) },
		now:       time.Now,
		codes:     map[string]codeEntry{},
	}
}

// SetSenderFactory overrides how senders are built (tests inject a stub).
func (s *Service) SetSenderFactory(f func(Config) Sender) {
	if f != nil {
		s.newSender = f
	}
}

// NewCaptcha returns a fresh image challenge for the registration form.
func (s *Service) NewCaptcha() (captcha.Captcha, error) { return s.captcha.Generate() }

func (s *Service) log(action, result, detail, ip string) {
	if s.audit != nil {
		s.audit.Log("", action, "", result, detail, ip)
	}
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// RequestCode validates the captcha, applies rate limits and emails a code.
// The captcha is consumed whether or not it matched.
func (s *Service) RequestCode(captchaID, captchaAnswer, emailAddr, ip string) error {
	cfg := s.store.Get()
	if !cfg.Enabled {
		s.log("email_code_request", "denied", "email registration disabled", ip)
		return ErrDisabled
	}
	emailAddr = strings.TrimSpace(strings.ToLower(emailAddr))
	if !emailRe.MatchString(emailAddr) {
		s.log("email_code_request", "denied", "invalid email address", ip)
		return fmt.Errorf("invalid email address")
	}
	if !s.captcha.Verify(captchaID, captchaAnswer) {
		s.log("captcha_failed", "denied", "captcha mismatch or expired", ip)
		return ErrCaptcha
	}
	if ok, reason := s.limiter.AllowAndRecord(emailAddr, ip, cfg); !ok {
		s.log("rate_limited", "denied", reason, ip)
		return &RateLimitedError{Reason: reason}
	}

	code, err := randomCode(cfg.CodeLength)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.codes[emailAddr] = codeEntry{code: code, expiresAt: s.now().Add(time.Duration(cfg.CodeTTLMinutes) * time.Minute)}
	s.mu.Unlock()

	server := cfg.FromName
	if server == "" {
		server = "FrostLeaves"
	}
	subject, body := Render(cfg.SubjectTemplate, cfg.BodyTemplate, code, cfg.CodeTTLMinutes, server, emailAddr)
	sender := s.newSender(cfg)
	if err := sender.Send(context.Background(), Message{To: emailAddr, Subject: subject, Body: body}); err != nil {
		s.mu.Lock()
		delete(s.codes, emailAddr)
		s.mu.Unlock()
		s.log("email_send_failed", "failed", err.Error(), ip)
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	s.log("email_code_sent", "success", "code emailed", ip)
	return nil
}

// VerifyCode validates a one-time verification code for the email.
func (s *Service) VerifyCode(emailAddr, code string) error {
	emailAddr = strings.TrimSpace(strings.ToLower(emailAddr))
	s.mu.Lock()
	e, ok := s.codes[emailAddr]
	if ok {
		delete(s.codes, emailAddr)
	}
	s.mu.Unlock()
	if !ok || e.used || s.now().After(e.expiresAt) || e.code != strings.TrimSpace(code) {
		s.log("email_verify", "denied", "invalid or expired code", "")
		return ErrBadCode
	}
	s.log("email_verified", "success", "", "")
	return nil
}

func randomCode(length int) (string, error) {
	if length <= 0 {
		length = 6
	}
	var b strings.Builder
	for i := 0; i < length; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}

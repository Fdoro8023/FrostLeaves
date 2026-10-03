package captcha

import (
	"bytes"
	"image/png"
	"testing"
	"time"
)

func TestGenerateProducesPNGAndVerifiesOnce(t *testing.T) {
	m := NewManager(2*time.Minute, 4)
	c, err := m.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Answer) != 4 {
		t.Fatalf("want 4 digits, got %d", len(c.Answer))
	}
	img, err := png.Decode(bytes.NewReader(c.Image))
	if err != nil {
		t.Fatalf("image must be valid PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() < 20 || b.Dy() < 20 {
		t.Fatalf("image too small: %v", b)
	}
	if !m.Verify(c.ID, c.Answer) {
		t.Fatal("correct answer must verify")
	}
	if m.Verify(c.ID, c.Answer) {
		t.Fatal("captcha must be single-use")
	}
}

func TestVerifyRejectsWrongAnswerAndUnknownID(t *testing.T) {
	m := NewManager(time.Minute, 4)
	c, err := m.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if m.Verify(c.ID, "999999") {
		t.Fatal("wrong answer must fail")
	}
	if m.Verify("does-not-exist", c.Answer) {
		t.Fatal("unknown id must fail")
	}
}

func TestVerifyTrimsWhitespace(t *testing.T) {
	m := NewManager(time.Minute, 5)
	c, err := m.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if !m.Verify(c.ID, "  "+c.Answer+" ") {
		t.Fatal("surrounding whitespace should be tolerated")
	}
}

func TestExpiredCaptchaFails(t *testing.T) {
	m := NewManager(time.Nanosecond, 4)
	c, err := m.Generate()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if m.Verify(c.ID, c.Answer) {
		t.Fatal("expired captcha must fail")
	}
}

func TestPendingPrunesExpired(t *testing.T) {
	m := NewManager(time.Nanosecond, 4)
	if _, err := m.Generate(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if got := m.Pending(); got != 0 {
		t.Fatalf("expired challenges must be pruned, got %d", got)
	}
}

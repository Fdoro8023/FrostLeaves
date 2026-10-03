// Package captcha implements a lightweight image CAPTCHA: digits drawn from a
// bitmap font, distorted with a per-column wave, plus interference lines and
// noise. Codes are single-use and validated server-side.
package captcha

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/big"
	"strings"
	"sync"
	"time"
)

// font5x7 holds a 5x7 bitmap for each digit.
var font5x7 = map[rune][]string{
	'0': {"01110", "10001", "10011", "10101", "11001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00010", "00100", "01000", "11111"},
	'3': {"11111", "00010", "00100", "00010", "00001", "10001", "01110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"00110", "01000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00010", "01100"},
}

// Captcha is a generated challenge. The answer is never sent to the client.
type Captcha struct {
	ID     string
	Image  []byte // PNG bytes
	Answer string
}

type entry struct {
	answer    string
	expiresAt time.Time
}

// Manager generates and verifies single-use CAPTCHAs.
type Manager struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	digits  int
}

// NewManager returns a Manager. ttl <= 0 defaults to 3 minutes; digits is
// clamped to [4, 6].
func NewManager(ttl time.Duration, digits int) *Manager {
	if ttl <= 0 {
		ttl = 3 * time.Minute
	}
	if digits < 4 {
		digits = 4
	}
	if digits > 6 {
		digits = 6
	}
	return &Manager{entries: map[string]entry{}, ttl: ttl, digits: digits}
}

// Generate creates a new challenge and its PNG image.
func (m *Manager) Generate() (Captcha, error) {
	code, err := randomDigits(m.digits)
	if err != nil {
		return Captcha{}, err
	}
	id, err := randomHex(16)
	if err != nil {
		return Captcha{}, err
	}
	img, err := render(code)
	if err != nil {
		return Captcha{}, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return Captcha{}, err
	}
	m.mu.Lock()
	m.pruneLocked(time.Now())
	m.entries[id] = entry{answer: code, expiresAt: time.Now().Add(m.ttl)}
	m.mu.Unlock()
	return Captcha{ID: id, Image: buf.Bytes(), Answer: code}, nil
}

// Verify checks an answer and consumes the challenge (single use). It returns
// false for missing, expired or already-used challenges.
func (m *Manager) Verify(id, answer string) bool {
	if id == "" || answer == "" {
		return false
	}
	m.mu.Lock()
	e, ok := m.entries[id]
	delete(m.entries, id) // single use, whether correct or not
	m.mu.Unlock()
	if !ok || time.Now().After(e.expiresAt) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(answer), e.answer)
}

// Pending reports how many challenges are outstanding (for tests/metrics).
func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	return len(m.entries)
}

func (m *Manager) pruneLocked(now time.Time) {
	for id, e := range m.entries {
		if now.After(e.expiresAt) {
			delete(m.entries, id)
		}
	}
}

func randomDigits(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

const (
	scale     = 5
	charWidth = 5 * scale
	charGap   = 2 * scale
	padX      = 8
	padY      = 8
	height    = 7*scale + padY*2
)

// render draws the digits with a wave distortion and interference lines.
func render(code string) (*image.RGBA, error) {
	width := padX*2 + len(code)*charWidth + (len(code)-1)*charGap
	if width <= 0 {
		return nil, fmt.Errorf("captcha: empty code")
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	bg := color.RGBA{R: 240, G: 244, B: 248, A: 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bg)
		}
	}

	ink := color.RGBA{R: 30, G: 40, B: 70, A: 255}
	for i, ch := range code {
		rows, ok := font5x7[ch]
		if !ok {
			continue
		}
		baseX := padX + i*(charWidth+charGap)
		phase := float64(i) * 1.1
		for col := 0; col < 5; col++ {
			wave := int(3 * math.Sin(phase+float64(col)*0.6))
			for row := 0; row < 7; row++ {
				if rows[row][col] != '1' {
					continue
				}
				x0 := baseX + col*scale
				y0 := padY + row*scale + wave
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.Set(x0+dx, y0+dy, ink)
					}
				}
			}
		}
	}

	// interference lines
	lineColor := color.RGBA{R: 120, G: 140, B: 200, A: 255}
	for n := 0; n < 4; n++ {
		x1, y1 := randInt(width), randInt(height)
		x2, y2 := randInt(width), randInt(height)
		drawLine(img, x1, y1, x2, y2, lineColor)
	}
	// noise dots
	dotColor := color.RGBA{R: 180, G: 200, B: 220, A: 255}
	for n := 0; n < width*height/120; n++ {
		img.Set(randInt(width), randInt(height), dotColor)
	}
	return img, nil
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.RGBA) {
	dx := abs(x2 - x1)
	dy := -abs(y2 - y1)
	sx, sy := -1, -1
	if x1 < x2 {
		sx = 1
	}
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy
	for {
		img.Set(x1, y1, c)
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

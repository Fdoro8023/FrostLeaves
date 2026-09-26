// Package security 提供安全加固所需的公共工具（路径穿越、输入清洗、
// 错误脱敏、上传类型校验、请求限流）。
package security

import (
	"log"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

var absPathRe = regexp.MustCompile(`[A-Za-z]:[\\/][^ ]*|/(?:home|root|Users|tmp|var|etc|opt)/[^ ]*`)

// SanitizeText 去掉绝对路径与堆栈痕迹（需求 6：错误信息脱敏）
func SanitizeText(s string) string {
	if s == "" {
		return s
	}
	if i := strings.Index(s, "goroutine "); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, ".go:"); i >= 0 {
		s = s[:i]
	}
	s = absPathRe.ReplaceAllString(s, "<path>")
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.TrimSpace(s)
}

func SanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeText(err.Error())
}

// SanitizeResponse 出口统一脱敏：清理响应里名为 error 的字段（需求 6）
func SanitizeResponse(data interface{}) interface{} {
	switch v := data.(type) {
	case map[string]string:
		if e, ok := v["error"]; ok {
			v["error"] = SanitizeText(e)
		}
	case map[string]interface{}:
		if e, ok := v["error"].(string); ok {
			v["error"] = SanitizeText(e)
		}
	}
	return data
}

// CleanInput 去掉控制字符并限长（需求 3：用户输入清洗）
func CleanInput(s string, max int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

// HasDotDot 路径是否含 .. 段（需求 1：路径穿越）
func HasDotDot(p string) bool {
	norm := strings.ReplaceAll(p, "\\", "/")
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// Contains 目标路径是否落在 base 之内（两者都已 Clean）
func ContainPath(base, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

// IsPublicTarget 分享目标必须在 shared/ 或 public/ 下（需求 8）
func IsPublicTarget(target string) bool {
	if HasDotDot(target) {
		return false
	}
	t := strings.TrimPrefix(strings.ReplaceAll(target, "\\", "/"), "/")
	if t == "" || t == "shared" || t == "public" {
		return true
	}
	return strings.HasPrefix(t, "shared/") || strings.HasPrefix(t, "public/")
}

// ---------- 需求 4：上传类型校验 ----------

var dangerousExt = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true, ".scr": true,
	".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true,
	".wsf": true, ".wsh": true, ".hta": true, ".dll": true, ".sys": true, ".cpl": true,
	".jar": true, ".sh": true, ".bash": true, ".py": true, ".pl": true, ".rb": true,
	".apk": true, ".app": true, ".deb": true, ".rpm": true, ".lnk": true, ".inf": true,
	".reg": true, ".gadget": true, ".msc": true, ".ocx": true, ".pif": true,
}

// SniffDangerous 扩展名 + 魔数双重判断（防止改名绕过）
func SniffDangerous(name string, head []byte) (bool, string) {
	lower := strings.ToLower(name)
	if ext := filepath.Ext(lower); ext != "" && dangerousExt[ext] {
		return true, "executable extension " + ext
	}
	if len(head) >= 2 && head[0] == 'M' && head[1] == 'Z' {
		return true, "PE executable magic"
	}
	if len(head) >= 4 && head[0] == 0x7F && head[1] == 'E' && head[2] == 'L' && head[3] == 'F' {
		return true, "ELF executable magic"
	}
	if len(head) >= 2 && head[0] == '#' && head[1] == '!' {
		return true, "script shebang"
	}
	return false, ""
}

// ---------- 需求 7：请求限流（令牌桶） ----------

type rateBucket struct {
	tokens   float64
	last     time.Time
	capacity float64
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	rate    float64
	burst   float64
}

// NewRateLimiter perSecond 每秒补充令牌数，burst 桶容量
func NewRateLimiter(perSecond float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: map[string]*rateBucket{},
		rate:    perSecond,
		burst:   float64(burst),
	}
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.cleanup()
		}
	}()
	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	if rl == nil {
		return true
	}
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	if !ok {
		rl.buckets[key] = &rateBucket{tokens: rl.burst - 1, last: now, capacity: rl.burst}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > b.capacity {
		b.tokens = b.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for k, b := range rl.buckets {
		if b.last.Before(cutoff) {
			delete(rl.buckets, k)
		}
	}
}

// statusRecorder 记录响应状态码
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// LogFailures 记录 4xx/5xx 请求（方法/路径/状态/IP），便于定位客户端报错
func LogFailures(tag string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if rec.status >= 400 {
			log.Printf("[%s] %d %s %s from %s query=%s", tag, rec.status, r.Method, r.URL.Path, ClientIP(r), r.URL.RawQuery)
		}
	})
}

// ClientIP 取客户端 IP（限流 key）
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

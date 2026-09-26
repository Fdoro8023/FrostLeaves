package file_gateway

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ========== 设备令牌认证（HMAC-SHA256 签名） ==========
//
// token 结构: hex(payload) + "." + hex(HMAC-SHA256(secret, payload))
// payload   : deviceID|deviceName|platform|role|expireUnix
//
// 相对旧实现的变化（见 CHANGES.md）：
//  1. 校验改为「解析 token → 重算签名 → 恒定时间比较」，HMAC 真正起校验作用；
//  2. 保留服务端注册表用于撤销：disconnect / reject / blacklist 后旧 token 立刻失效；
//  3. 新增 RevokeDevice（只撤销，不拉黑）；
//  4. 令牌与黑名单可持久化到 data/tokens.json，服务重启不掉线。

const defaultTokenTTL = 24 * time.Hour

type Authenticator struct {
	secret  []byte
	mu      sync.RWMutex
	tokens  map[string]*TokenEntry // deviceID -> entry
	blocked map[string]bool        // deviceID -> blocked

	storePath string
	storeMu   sync.Mutex
}

type TokenEntry struct {
	DeviceID   string    `json:"device_id"`
	DeviceName string    `json:"device_name"`
	Platform   string    `json:"platform"`
	Role       string    `json:"role"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func NewAuthenticator(secret string) *Authenticator {
	return &Authenticator{
		secret:  []byte(secret),
		tokens:  make(map[string]*TokenEntry),
		blocked: make(map[string]bool),
	}
}

// ========== 签名 / 编码 ==========

func (a *Authenticator) sign(payload string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func payloadOf(e *TokenEntry) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d",
		e.DeviceID, e.DeviceName, e.Platform, e.Role, e.ExpiresAt.Unix())
}

func (a *Authenticator) tokenFor(e *TokenEntry) string {
	p := payloadOf(e)
	return hex.EncodeToString([]byte(p)) + "." + a.sign(p)
}

// ========== 签发 ==========

// GenerateToken 为设备签发新令牌（同设备旧令牌自动作废）
func (a *Authenticator) GenerateToken(deviceID, deviceName, platform, role string) string {
	if role == "" {
		role = "client"
	}
	entry := &TokenEntry{
		DeviceID:   deviceID,
		DeviceName: deviceName,
		Platform:   platform,
		Role:       role,
		ExpiresAt:  time.Now().Add(defaultTokenTTL),
	}
	token := a.tokenFor(entry)

	a.mu.Lock()
	a.tokens[deviceID] = entry
	a.mu.Unlock()

	a.save()
	return token
}

// RegisterDevice 设备通过验证码校验后调用
func (a *Authenticator) RegisterDevice(deviceID, deviceName, platform string) string {
	return a.GenerateToken(deviceID, deviceName, platform, "client")
}

// ========== 校验 ==========

// Validate 校验设备令牌：签名 → 载荷 → 有效期 → 黑名单 → 注册表（撤销状态）
func (a *Authenticator) Validate(deviceID, token string) (*AuthContext, error) {
	if deviceID == "" || token == "" {
		return nil, fmt.Errorf("missing credentials")
	}

	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid token format")
	}

	rawPayload, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid token payload")
	}
	payload := string(rawPayload)

	// 1) 签名校验（恒定时间比较，避免时序侧信道）
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(a.sign(payload))) != 1 {
		return nil, fmt.Errorf("invalid token signature")
	}

	// 2) 载荷解析
	f := strings.Split(payload, "|")
	if len(f) != 5 {
		return nil, fmt.Errorf("malformed token payload")
	}
	if f[0] != deviceID {
		return nil, fmt.Errorf("token does not belong to this device")
	}
	expUnix, err := strconv.ParseInt(f[4], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed token expiry")
	}
	if time.Now().Unix() > expUnix {
		return nil, fmt.Errorf("token expired")
	}

	// 3) 黑名单 / 注册表
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.blocked[deviceID] {
		return nil, fmt.Errorf("device is blocked")
	}
	entry, ok := a.tokens[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not registered or token revoked")
	}
	if time.Now().After(entry.ExpiresAt) {
		return nil, fmt.Errorf("token expired")
	}

	return &AuthContext{
		DeviceID:   entry.DeviceID,
		DeviceName: entry.DeviceName,
		Platform:   entry.Platform,
		Role:       entry.Role,
		Token:      token,
	}, nil
}

// ========== 撤销 / 拉黑 ==========

// RevokeDevice 撤销设备令牌（不拉黑，设备可重新申请）
func (a *Authenticator) RevokeDevice(deviceID string) {
	a.mu.Lock()
	delete(a.tokens, deviceID)
	a.mu.Unlock()
	a.save()
}

// BlockDevice 拉黑设备并撤销其令牌
func (a *Authenticator) BlockDevice(deviceID string) {
	a.mu.Lock()
	a.blocked[deviceID] = true
	delete(a.tokens, deviceID)
	a.mu.Unlock()
	a.save()
}

// UnblockDevice 解除拉黑
func (a *Authenticator) UnblockDevice(deviceID string) {
	a.mu.Lock()
	delete(a.blocked, deviceID)
	a.mu.Unlock()
	a.save()
}

func (a *Authenticator) IsDeviceBlocked(deviceID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.blocked[deviceID]
}


// HasToken 该设备当前是否存在有效令牌（供客户端自查连接状态使用，需求 1）
func (a *Authenticator) HasToken(deviceID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	e, ok := a.tokens[deviceID]
	if !ok {
		return false
	}
	return time.Now().Before(e.ExpiresAt)
}

// ActiveTokenCount 当前有效令牌数量
func (a *Authenticator) ActiveTokenCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.tokens)
}

// ========== 持久化 ==========

type authStoreFile struct {
	Tokens  map[string]*TokenEntry `json:"tokens"`
	Blocked []string               `json:"blocked"`
}

// SetStore 指定持久化文件路径（空字符串 = 不持久化）
func (a *Authenticator) SetStore(path string) { a.storePath = path }

// Load 从磁盘恢复未过期的令牌与黑名单
func (a *Authenticator) Load() error {
	if a.storePath == "" {
		return nil
	}
	data, err := os.ReadFile(a.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var f authStoreFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("parse %s: %w", a.storePath, err)
	}

	now := time.Now()
	loaded := 0
	a.mu.Lock()
	for id, e := range f.Tokens {
		if e == nil || id == "" || now.After(e.ExpiresAt) {
			continue
		}
		a.tokens[id] = e
		loaded++
	}
	for _, id := range f.Blocked {
		if id != "" {
			a.blocked[id] = true
		}
	}
	blockedCount := len(a.blocked)
	a.mu.Unlock()

	log.Printf("[Auth] restored %d active token(s), %d blocked device(s)", loaded, blockedCount)
	return nil
}

// save 原子写入令牌与黑名单
func (a *Authenticator) save() {
	if a.storePath == "" {
		return
	}
	a.storeMu.Lock()
	defer a.storeMu.Unlock()

	a.mu.RLock()
	out := authStoreFile{
		Tokens:  make(map[string]*TokenEntry, len(a.tokens)),
		Blocked: make([]string, 0, len(a.blocked)),
	}
	for id, e := range a.tokens {
		cp := *e
		out.Tokens[id] = &cp
	}
	for id := range a.blocked {
		out.Blocked = append(out.Blocked, id)
	}
	a.mu.RUnlock()

	if err := writeJSONAtomic(a.storePath, out); err != nil {
		log.Printf("[Auth] save tokens failed: %v", err)
	}
}

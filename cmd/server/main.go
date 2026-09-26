// Package main implements the FrostLeaves server.
// It integrates the file gateway, auth service, and device management
// into a single Windows executable.
package main

import (
	"crypto/rand"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"frostleaves/pkg/file_gateway"
	"frostleaves/pkg/security"
)

// ========== Version ==========

const (
	AppVersion = "1.0.0 beta"
	AppName    = "FrostLeaves"
)

// ========== Device Management ==========

// shareStore 由 main 初始化；管理接口通过它读写分享记录
var shareStore *file_gateway.ShareStore

type DeviceStatus string

const (
	StatusPending     DeviceStatus = "pending"
	StatusApproved    DeviceStatus = "approved"
	StatusConnected   DeviceStatus = "connected"
	StatusRejected    DeviceStatus = "rejected"
	StatusBlacklisted DeviceStatus = "blacklisted"
)

type Device struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Platform   string       `json:"platform"`
	Model      string       `json:"model,omitempty"`
	Status     DeviceStatus `json:"status"`
	IPAddress  string       `json:"ip_address,omitempty"`
	Token      string       `json:"-"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
	LastSeenAt *time.Time   `json:"last_seen_at,omitempty"`
}

type VerificationCode struct {
	ID        string     `json:"id"`
	DeviceID  string     `json:"device_id"`
	Code      string     `json:"code"`
	ExpiresAt time.Time  `json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	IsValid   bool       `json:"is_valid"`
	CreatedAt time.Time  `json:"created_at"`
}

type DeviceManager struct {
	mu            sync.RWMutex
	devices       map[string]*Device
	codes         map[string]*VerificationCode // code -> entry
	codesByDevice map[string]*VerificationCode // deviceID -> latest code
	storePath     string                       // 设备持久化文件路径
}

func NewDeviceManager() *DeviceManager {
	return &DeviceManager{
		devices:       make(map[string]*Device),
		codes:         make(map[string]*VerificationCode),
		codesByDevice: make(map[string]*VerificationCode),
	}
}

func (dm *DeviceManager) ApplyDevice(deviceID, deviceName, platform, model string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if d, ok := dm.devices[deviceID]; ok {
		if d.Status == StatusBlacklisted {
			return fmt.Errorf("device is blacklisted")
		}
		if d.Status == StatusConnected {
			return fmt.Errorf("device already connected")
		}
		d.Status = StatusPending
		d.Name = deviceName
		d.Platform = platform
		d.Model = model
		d.UpdatedAt = time.Now()
		return nil
	}

	dm.devices[deviceID] = &Device{
		ID:        deviceID,
		Name:      deviceName,
		Platform:  platform,
		Model:     model,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return nil
}

func (dm *DeviceManager) ListDevices(status string) []*Device {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	var result []*Device
	for _, d := range dm.devices {
		if status != "" && string(d.Status) != status {
			continue
		}
		result = append(result, d)
	}
	return result
}

func (dm *DeviceManager) ApproveDevice(deviceID string) (*VerificationCode, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, ok := dm.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not found")
	}
	if d.Status != StatusPending && d.Status != StatusRejected {
		return nil, fmt.Errorf("cannot approve device in status: %s", d.Status)
	}

	// Generate 8-character verification code
	code := generateCode(8)
	expiresAt := time.Now().Add(5 * time.Minute)

	// Invalidate previous codes for this device
	for _, c := range dm.codes {
		if c.DeviceID == deviceID && c.IsValid {
			c.IsValid = false
		}
	}

	vc := &VerificationCode{
		ID:        generateCode(12),
		DeviceID:  deviceID,
		Code:      code,
		ExpiresAt: expiresAt,
		IsValid:   true,
		CreatedAt: time.Now(),
	}

	dm.codes[vc.ID] = vc
	dm.codesByDevice[deviceID] = vc
	d.Status = StatusApproved
	d.UpdatedAt = time.Now()

	return vc, nil
}

func (dm *DeviceManager) VerifyCode(deviceID, code string) (bool, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	vc, ok := dm.codesByDevice[deviceID]
	if !ok {
		return false, fmt.Errorf("no verification code found for device")
	}

	if vc.Code != code {
		return false, fmt.Errorf("invalid verification code")
	}

	if !vc.IsValid {
		return false, fmt.Errorf("verification code already used")
	}

	if time.Now().After(vc.ExpiresAt) {
		vc.IsValid = false
		return false, fmt.Errorf("verification code expired")
	}

	// Mark as used
	now := time.Now()
	vc.UsedAt = &now
	vc.IsValid = false

	// Update device status
	d := dm.devices[deviceID]
	d.Status = StatusConnected
	d.UpdatedAt = now

	return true, nil
}

func (dm *DeviceManager) RejectDevice(deviceID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, ok := dm.devices[deviceID]
	if !ok {
		return fmt.Errorf("device not found")
	}
	d.Status = StatusRejected
	d.UpdatedAt = time.Now()
	return nil
}

func (dm *DeviceManager) BlacklistDevice(deviceID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, ok := dm.devices[deviceID]
	if !ok {
		return fmt.Errorf("device not found")
	}
	d.Status = StatusBlacklisted
	d.UpdatedAt = time.Now()
	return nil
}

func (dm *DeviceManager) UnblockDevice(deviceID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	d, ok := dm.devices[deviceID]
	if !ok {
		return fmt.Errorf("device not found")
	}
	if d.Status != StatusBlacklisted {
		return fmt.Errorf("device is not blacklisted")
	}
	d.Status = StatusRejected
	d.UpdatedAt = time.Now()
	return nil
}

func (dm *DeviceManager) DisconnectDevice(deviceID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if _, ok := dm.devices[deviceID]; !ok {
		return fmt.Errorf("device not found")
	}
	// Remove device from the list entirely when disconnected
	delete(dm.devices, deviceID)
	return nil
}

// MarkSeen 记录设备最近活跃（客户端轮询状态即视为心跳）
func (dm *DeviceManager) MarkSeen(deviceID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	d, ok := dm.devices[deviceID]
	if !ok {
		return
	}
	now := time.Now()
	d.LastSeenAt = &now
	if d.Status == StatusApproved || d.Status == StatusConnected {
		d.Status = StatusConnected
	}
}

func (dm *DeviceManager) GetDevice(deviceID string) (*Device, error) {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	d, ok := dm.devices[deviceID]
	if !ok {
		return nil, fmt.Errorf("device not found")
	}
	return d, nil
}

func generateCode(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// ========== Server Config ==========

type ServerConfig struct {
	// File Gateway
	HTTPBindAddr    string `json:"http_bind_addr"`
	HTTPPort        int    `json:"http_port"`
	StorageRoot     string `json:"storage_root"`
	PublicSharedDir string `json:"public_shared_dir"`

	// Auth Service
	AuthPort   int    `json:"auth_port"`
	AuthSecret string `json:"auth_secret"`

	// Admin API protection
	AdminSecret      string `json:"admin_secret"`       // X-Admin-Token（非本机访问管理接口时校验）
	AdminAllowRemote bool   `json:"admin_allow_remote"` // true 时允许非本机携带 admin_secret 访问管理接口

	// 公网隧道（点对点组网 Funnel，见 docs/ARCHITECTURE.md）
	TunnelCommand string `json:"tunnel_command"` // 组网客户端命令（名称或绝对路径，默认 mesh）
	TunnelPort    int    `json:"tunnel_port"`    // 0 = 使用 web_port

	// 服务器名称（客户端展示，默认「服务器」）
	ServerName string `json:"server_name"`

	// 数据与启动行为
	DataDir            string `json:"data_dir"`
	CleanupFakeFolders bool   `json:"cleanup_fake_folders"`

	// Web Service
	WebPort int `json:"web_port"`

	// HTTPS
	BootstrapPort int `json:"bootstrap_port"` // 0 = web_port+1（仅提供 CA 证书的引导端口）

	// Quotas (global defaults)
	MaxUploadBytes    int64 `json:"max_upload_bytes"`
	UploadSpeedKBPS   int64 `json:"upload_speed_kbps"`
	DownloadSpeedKBPS int64 `json:"download_speed_kbps"`
	MaxFileSizeBytes  int64 `json:"max_file_size_bytes"`

	// TLS
	EnableTLS   bool   `json:"enable_tls"`
	TLSCertPath string `json:"tls_cert_path"`
	TLSKeyPath  string `json:"tls_key_path"`
}

func defaultConfig() ServerConfig {
	return ServerConfig{
		HTTPBindAddr:       "0.0.0.0",
		HTTPPort:           9090,
		StorageRoot:        "./storage",
		PublicSharedDir:    "shared",
		AuthPort:           9091,
		AuthSecret:         "",
		WebPort:            9092,
		BootstrapPort:      9093,
		MaxUploadBytes:     0,
		UploadSpeedKBPS:    0,
		DownloadSpeedKBPS:  0,
		MaxFileSizeBytes:   0,
		DataDir:            "./data",
		CleanupFakeFolders: false,
		TunnelCommand:      "mesh",
		TunnelPort:         0,
		ServerName:         "服务器",
	}
}

func loadConfig(path string) ServerConfig {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	json.Unmarshal(data, &cfg)
	return cfg
}

func saveConfig(path string, cfg ServerConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// quarantineFakeFolders moves directories whose names look like files (have known extensions)
// into a quarantine folder instead of deleting them (data-safety, see CHANGES.md)
func quarantineFakeFolders(root, quarantineRoot string) {
	log.Printf("[Server] Scanning for fake folders in %s...", root)

	fileExtensions := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
		".mp4": true, ".avi": true, ".mkv": true, ".mov": true, ".wmv": true,
		".mp3": true, ".wav": true, ".flac": true, ".aac": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
		".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
		".txt": true, ".md": true, ".json": true, ".xml": true,
	}

	var cleanupDir func(dir string)
	cleanupDir = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Check if directory name looks like a file (has extension)
			name := entry.Name()
			ext := filepath.Ext(name)
			if ext != "" && fileExtensions[strings.ToLower(ext)] {
				fakePath := filepath.Join(dir, name)
				log.Printf("[Server] Suspicious folder (name looks like a file), quarantining: %s", fakePath)
				dest := filepath.Join(quarantineRoot, time.Now().Format("20060102-150405")+"_"+name)
				if err := os.MkdirAll(quarantineRoot, 0755); err != nil {
					log.Printf("[Server] Cannot create quarantine dir %s: %v", quarantineRoot, err)
				} else if err := os.Rename(fakePath, dest); err != nil {
					log.Printf("[Server] Quarantine failed for %s: %v (left untouched)", fakePath, err)
				} else {
					log.Printf("[Server] Moved to quarantine: %s -> %s", fakePath, dest)
				}
			} else {
				// Recursively check subdirectories
				cleanupDir(filepath.Join(dir, name))
			}
		}
	}

	cleanupDir(root)
	log.Printf("[Server] Fake folder cleanup completed")
}

// ========== Main ==========

// 需求 7：鉴权类接口限流（按来源 IP）
var (
	applyLimiter  = security.NewRateLimiter(0.5, 20) // 设备申请：约 30/分钟
	verifyLimiter = security.NewRateLimiter(0.2, 10) // 验证码校验：约 12/分钟（防暴力猜码）
	loginLimiter  = security.NewRateLimiter(0.5, 20) // 登录：约 30/分钟
	shareLimiter  = security.NewRateLimiter(1.0, 60) // 访客分享：约 60/分钟
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	flag.Parse()

	log.Printf("%s v%s starting...", AppName, AppVersion)

	// Load config
	cfg := loadConfig(*configPath)
	activeConfigPath := *configPath

	// 首次运行生成随机密钥；确保数据目录存在
	ensureSecrets(&cfg, activeConfigPath)
	os.MkdirAll(cfg.DataDir, 0755)

	// Warn about 0.0.0.0 binding
	if cfg.HTTPBindAddr == "0.0.0.0" {
		log.Println("WARNING: Binding to 0.0.0.0 - server is accessible from all network interfaces")
	}

	// Ensure directories
	os.MkdirAll(cfg.StorageRoot, 0755)
	os.MkdirAll(filepath.Join(cfg.StorageRoot, "shared"), 0755)
	os.MkdirAll(filepath.Join(cfg.StorageRoot, "public"), 0755)
	os.MkdirAll(filepath.Join(cfg.StorageRoot, "devices"), 0755)
	os.MkdirAll("./logs", 0755)
	os.MkdirAll("./data", 0755)

	log.Printf("[Server] Storage directories initialized at: %s", cfg.StorageRoot)

	// Cleanup fake folders (directories that look like files)
	if cfg.CleanupFakeFolders {
		quarantineFakeFolders(cfg.StorageRoot, filepath.Join(cfg.DataDir, "quarantine"))
	} else {
		log.Printf("[Server] fake-folder cleanup disabled (set cleanup_fake_folders=true to enable)")
	}

	// Initialize components
	audit, err := file_gateway.NewAuditLogger("./logs")
	if err != nil {
		log.Fatalf("Failed to init audit logger: %v", err)
	}
	defer audit.Close()

	quota := file_gateway.NewQuotaManager(file_gateway.QuotaConfig{
		MaxUploadBytes:    cfg.MaxUploadBytes,
		UploadSpeedKBPS:   cfg.UploadSpeedKBPS,
		DownloadSpeedKBPS: cfg.DownloadSpeedKBPS,
		MaxFileSizeBytes:  cfg.MaxFileSizeBytes,
	})

	quota.SetStore(filepath.Join(cfg.DataDir, "quota.json"))
	if err := quota.Load(); err != nil {
		log.Printf("[Server] load quota failed: %v", err)
	}

	auth := file_gateway.NewAuthenticator(cfg.AuthSecret)
	auth.SetStore(filepath.Join(cfg.DataDir, "tokens.json"))
	if err := auth.Load(); err != nil {
		log.Printf("[Server] load tokens failed: %v", err)
	}

	// Device manager
	dm := NewDeviceManager()
	dm.SetStore(filepath.Join(cfg.DataDir, "devices.json"))
	if err := dm.Load(); err != nil {
		log.Printf("[Server] load devices failed: %v", err)
	}
	dm.StartAutoSave(15 * time.Second)

	// File gateway
	// 双击启动体验：若已有实例在运行，弹框说明并退出（避免控制台闪退）
	if checkAlreadyRunning(cfg) {
		return
	}

	// ===== HTTPS：本地 CA 自签（局域网加密；回环保留 HTTP）=====
	tlsMat, tlsErr := ensureTLSMaterial(cfg)
	if tlsErr != nil {
		log.Printf("[TLS] certificate setup failed: %v (LAN will stay HTTP)", tlsErr)
		tlsMat = nil
	} else {
		activeTLS = tlsMat
		cfg.EnableTLS = true
		cfg.TLSCertPath = tlsMat.CertPath
		cfg.TLSKeyPath = tlsMat.KeyPath
		log.Printf("[TLS] ready; CA fingerprint=%s", tlsMat.Fingerprint)
	}

	gwCfg := file_gateway.GatewayConfig{
		BindAddr:          cfg.HTTPBindAddr,
		Port:              cfg.HTTPPort,
		StorageRoot:       cfg.StorageRoot,
		PublicSharedDir:   cfg.PublicSharedDir,
		MaxFileSizeBytes:  cfg.MaxFileSizeBytes,
		UploadSpeedKBPS:   cfg.UploadSpeedKBPS,
		DownloadSpeedKBPS: cfg.DownloadSpeedKBPS,
		EnableTLS:         cfg.EnableTLS,
		TLSCertPath:       cfg.TLSCertPath,
		TLSKeyPath:        cfg.TLSKeyPath,
	}

	gw := file_gateway.NewFileGateway(gwCfg, quota, audit, auth)
	// 管理通道密钥（需求 3：服务端模式 GUI 经回环 + X-Admin-Token 访问公共文件）
	gw.SetAdminSecret(cfg.AdminSecret)

	// 分享码存储（访客链接，免设备 token 访问指定目录）
	shares := file_gateway.NewShareStore()
	shares.SetStore(filepath.Join(cfg.DataDir, "shares.json"))
	if err := shares.Load(); err != nil {
		log.Printf("[Server] load shares failed: %v", err)
	}
	gw.SetShareStore(shares)
	shareStore = shares

	// Web service (for browser clients)
	webMux := http.NewServeMux()
	setupWebRoutes(webMux, dm, auth, quota, audit, cfg, activeConfigPath)

	// 把访客分享 API 反代到本地文件网关：这样对外（隧道/网页访客）只需暴露一个端口
	if target, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", cfg.HTTPPort)); err != nil {
		log.Printf("[WebService] share proxy disabled: %v", err)
	} else {
		shareProxy := httputil.NewSingleHostReverseProxy(target)
		origDirector := shareProxy.Director
		shareProxy.Director = func(req *http.Request) {
			origDirector(req)
			// 安全加固：对外（隧道）通道不得携带管理令牌
			req.Header.Del("X-Admin-Token")
		}
		webMux.Handle("/api/v1/share/", shareProxy)
		log.Printf("[WebService] share API proxied to %s", target)
	}

	// 隧道管理接口（管理员）：网页端也能开关 Funnel
	setupTunnelRoutes(webMux, cfg)

	webServer := &http.Server{
		Handler: security.LogFailures("Web", webMux),
	}

	// HTTPS（对外：局域网/公网加密）
	if tlsMat != nil {
		if tlsCfg, terr := loadTLSConfig(tlsMat); terr == nil {
			if hln, lerr := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.HTTPBindAddr, cfg.WebPort)); lerr == nil {
				log.Printf("[WebService] HTTPS listening on %s:%d", cfg.HTTPBindAddr, cfg.WebPort)
				go func() {
					if serr := webServer.Serve(tls.NewListener(hln, tlsCfg)); serr != nil && serr != http.ErrServerClosed {
						log.Printf("[WebService] HTTPS error: %v", serr)
					}
				}()
			} else {
				log.Printf("[WebService] HTTPS listen failed: %v", lerr)
			}
		}
	}

	// HTTP（仅回环：桌面端与组网 Funnel 使用）
	webListener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.WebPort))
	if err != nil {
		log.Printf("[WebService] Warning: Failed to listen on %s:%d: %v", cfg.HTTPBindAddr, cfg.WebPort, err)
		log.Printf("[WebService] Web interface will not be available")
	} else {
		log.Printf("[WebService] HTTP (loopback) listening on 127.0.0.1:%d", cfg.WebPort)
		go func() {
			if err := webServer.Serve(webListener); err != nil && err != http.ErrServerClosed {
				log.Printf("[WebService] Error: %v", err)
			}
		}()
	}

	// Auth service HTTP API (runs on separate port)
	authMux := http.NewServeMux()
	setupAuthRoutes(authMux, dm, auth, quota, audit, cfg, activeConfigPath)
	setupTunnelRoutes(authMux, cfg)

	authServer := &http.Server{
		Handler: security.LogFailures("Auth", authMux),
	}

	// Create listener with SO_REUSEADDR for auth service
	// HTTPS（对外）
	if tlsMat != nil {
		if tlsCfg2, terr2 := loadTLSConfig(tlsMat); terr2 == nil {
			if hln2, lerr2 := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.HTTPBindAddr, cfg.AuthPort)); lerr2 == nil {
				log.Printf("[AuthService] HTTPS listening on %s:%d", cfg.HTTPBindAddr, cfg.AuthPort)
				go func() {
					if serr2 := authServer.Serve(tls.NewListener(hln2, tlsCfg2)); serr2 != nil && serr2 != http.ErrServerClosed {
						log.Printf("[AuthService] HTTPS error: %v", serr2)
					}
				}()
			} else {
				log.Printf("[AuthService] HTTPS listen failed: %v", lerr2)
			}
		}
	}

	// HTTP（仅回环）
	authListener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.AuthPort))
	if err != nil {
		log.Printf("[AuthService] Failed to listen on %s:%d: %v", cfg.HTTPBindAddr, cfg.AuthPort, err)
		showMessageBox("Frost Leaves 服务端", fmt.Sprintf("授权服务端口 %d 无法监听：\n%v\n\n端口可能被占用，或权限不足。", cfg.AuthPort, err), mbIconError)
		return
	}

	// Start file gateway in goroutine
	go func() {
		log.Printf("[FileGateway] Listening on %s:%d", cfg.HTTPBindAddr, cfg.HTTPPort)
		if err := gw.Start(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FileGateway] Error: %v", err)
		}
	}()

	// Start auth service
	go func() {
		log.Printf("[AuthService] Listening on %s:%d", cfg.HTTPBindAddr, cfg.AuthPort)
		if err := authServer.Serve(authListener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[AuthService] Error: %v", err)
		}
	}()

	// CA 引导端口（明文 HTTP，只提供 CA 证书，便于手机首次信任）
	if tlsMat != nil {
		bsPort := cfg.BootstrapPort
		if bsPort == 0 {
			bsPort = cfg.WebPort + 1
		}
		bsMux := http.NewServeMux()
		bsMux.HandleFunc("/ca.crt", handleCACert(tlsMat))
		bsMux.HandleFunc("/api/v1/tls/ca.crt", handleCACert(tlsMat))
		bsMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("FrostLeaves CA bootstrap. Download: /ca.crt"))
		})
		if bsLn, bsErr := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.HTTPBindAddr, bsPort)); bsErr == nil {
			log.Printf("[TLS] CA bootstrap listening on %s:%d (/ca.crt)", cfg.HTTPBindAddr, bsPort)
			go func() {
				bsServer := &http.Server{Handler: bsMux}
				_ = bsServer.Serve(bsLn)
			}()
		} else {
			log.Printf("[TLS] CA bootstrap listen failed: %v", bsErr)
		}
	}

	log.Printf("%s v%s started successfully", AppName, AppVersion)
	log.Printf("  File Gateway: %s:%d", cfg.HTTPBindAddr, cfg.HTTPPort)
	log.Printf("  Auth Service: %s:%d", cfg.HTTPBindAddr, cfg.AuthPort)
	log.Printf("  Web Service: %s:%d", cfg.HTTPBindAddr, cfg.WebPort)
	log.Printf("  Storage Root: %s", cfg.StorageRoot)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 退出前落盘，避免重启丢设备/配额
	if err := dm.Save(); err != nil {
		log.Printf("[Server] final device save failed: %v", err)
	}
	quota.Flush()
	log.Println("Shutting down...")
}

// ========== Auth Service Routes ==========

func setupAuthRoutes(mux *http.ServeMux, dm *DeviceManager, auth *file_gateway.Authenticator,
	quota *file_gateway.QuotaManager, audit *file_gateway.AuditLogger, cfg ServerConfig, configPath string) {

	// Device apply (client)
	mux.HandleFunc("/api/v1/device/apply", func(w http.ResponseWriter, r *http.Request) {
		if !applyLimiter.Allow(security.ClientIP(r)) {
			audit.Log("", "rate_limited", "/device/apply", "denied", "", r.RemoteAddr)
			writeJSON(w, 429, map[string]string{"error": "too many requests"})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID   string `json:"device_id"`
			DeviceName string `json:"device_name"`
			Platform   string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if req.DeviceID == "" {
			// 需求 5：设备型号在下方 ApplyDevice 调用前读取

			writeJSON(w, 400, map[string]string{"error": "device_id required"})
			return
		}

		deviceModel := strings.TrimSpace(r.Header.Get("X-Device-Model"))
		if err := dm.ApplyDevice(req.DeviceID, req.DeviceName, req.Platform, deviceModel); err != nil {
			writeJSON(w, 403, map[string]string{"error": err.Error()})
			return
		}

		audit.Log(req.DeviceID, "device_apply", "", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok", "data": map[string]string{"status": "pending"}})
	})

	// Device list (admin)
	mux.HandleFunc("/api/v1/device/list", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		status := r.URL.Query().Get("status")
		devices := dm.ListDevices(status)
		// 「已连接」= 令牌有效 且 60 秒内有过心跳；否则视为已断开（清掉僵尸记录）
		for _, d := range devices {
			if d.Status != StatusConnected {
				continue
			}
			stale := !auth.HasToken(d.ID)
			if !stale {
				if d.LastSeenAt == nil || time.Since(*d.LastSeenAt) > 60*time.Second {
					stale = true
				}
			}
			if stale {
				d.Status = DeviceStatus("disconnected")
			}
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"total": len(devices),
				"items": devices,
			},
		})
	})

	// Approve device (admin)
	mux.HandleFunc("/api/v1/device/", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		// Parse path: /api/v1/device/{device_id}/{action}
		path := r.URL.Path
		parts := splitPath(path)
		if len(parts) < 5 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		deviceID := parts[3]
		action := parts[4]

		switch action {
		case "approve":
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			vc, err := dm.ApproveDevice(deviceID)
			if err != nil {
				writeJSON(w, 409, map[string]string{"error": err.Error()})
				return
			}
			audit.Log(deviceID, "code_generate", "", "success", fmt.Sprintf("code=%s", vc.Code), r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{
				"code": 0,
				"msg":  "ok",
				"data": map[string]interface{}{
					"code":       vc.Code,
					"expires_in": 300,
				},
			})

		case "reject":
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			if err := dm.RejectDevice(deviceID); err != nil {
				writeJSON(w, 404, map[string]string{"error": err.Error()})
				return
			}
			auth.RevokeDevice(deviceID)
			audit.Log(deviceID, "device_reject", "", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})

		case "blacklist":
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			if err := dm.BlacklistDevice(deviceID); err != nil {
				writeJSON(w, 404, map[string]string{"error": err.Error()})
				return
			}
			auth.BlockDevice(deviceID)
			audit.Log(deviceID, "device_blacklist", "", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})

		case "unblock":
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			if err := dm.UnblockDevice(deviceID); err != nil {
				writeJSON(w, 404, map[string]string{"error": err.Error()})
				return
			}
			auth.UnblockDevice(deviceID)
			audit.Log(deviceID, "device_unblock", "", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})

		case "disconnect":
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "method not allowed"})
				return
			}
			if err := dm.DisconnectDevice(deviceID); err != nil {
				writeJSON(w, 404, map[string]string{"error": err.Error()})
				return
			}
			auth.RevokeDevice(deviceID)
			audit.Log(deviceID, "device_disconnect", "", "success", "", r.RemoteAddr)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})

		default:
			writeJSON(w, 404, map[string]string{"error": "unknown action"})
		}
	})

	// Verify code (client)
	mux.HandleFunc("/api/v1/device/verify", func(w http.ResponseWriter, r *http.Request) {
		if !verifyLimiter.Allow(security.ClientIP(r)) {
			audit.Log("", "rate_limited", "/device/verify", "denied", "", r.RemoteAddr)
			writeJSON(w, 429, map[string]string{"error": "too many requests"})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID string `json:"device_id"`
			Code     string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}

		ok, err := dm.VerifyCode(req.DeviceID, req.Code)
		if err != nil || !ok {
			audit.Log(req.DeviceID, "code_verify", "", "denied", err.Error(), r.RemoteAddr)
			writeJSON(w, 401, map[string]interface{}{"code": 3, "msg": err.Error()})
			return
		}

		// Generate auth token
		device, _ := dm.GetDevice(req.DeviceID)
		token := auth.RegisterDevice(req.DeviceID, device.Name, device.Platform)

		audit.Log(req.DeviceID, "device_connect", "", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"token":       token,
				"tailnet_ip":  "",
				"preauth_key": "",
			},
		})
	})

	// Auth login (for HTTP lightweight clients)
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if !loginLimiter.Allow(security.ClientIP(r)) {
			audit.Log("", "rate_limited", "/auth/login", "denied", "", r.RemoteAddr)
			writeJSON(w, 429, map[string]string{"error": "too many requests"})
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			DeviceID   string `json:"device_id"`
			DeviceName string `json:"device_name"`
			Platform   string `json:"platform"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}

		// Check device status
		device, err := dm.GetDevice(req.DeviceID)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "device not registered"})
			return
		}
		if device.Status != StatusConnected {
			writeJSON(w, 403, map[string]string{"error": "device not connected"})
			return
		}

		token := auth.RegisterDevice(req.DeviceID, device.Name, device.Platform)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]string{"token": token},
		})
	})

	// Quota management (admin)
	mux.HandleFunc("/api/v1/quota/list", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		configs := quota.GetAllConfigs()
		usage := quota.GetAllUsage()
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"configs": configs,
				"usage":   usage,
			},
		})
	})

	mux.HandleFunc("/api/v1/quota", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		switch r.Method {
		case http.MethodPost:
			var cfg file_gateway.QuotaConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid body"})
				return
			}
			quota.SetConfig(cfg)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
		case http.MethodDelete:
			deviceID := r.URL.Query().Get("device_id")
			quota.RemoveConfig(deviceID)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
		default:
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		}
	})

	mux.HandleFunc("/api/v1/quota/usage/", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		deviceID := r.URL.Path[len("/api/v1/quota/usage/"):]
		usage := quota.GetUsage(deviceID)
		remaining := quota.GetRemainingQuota(deviceID)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"usage":     usage,
				"remaining": remaining,
			},
		})
	})

	// Audit logs (admin)
	mux.HandleFunc("/api/v1/audit/logs", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		deviceID := r.URL.Query().Get("device_id")
		action := r.URL.Query().Get("action")
		entries, total := audit.Query(deviceID, action, time.Time{}, time.Time{}, 1, 100)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"total": total,
				"items": entries,
			},
		})
	})

	// ===== 分享管理（访客链接） =====
	// 管理员接口：/api/v1/share/create | list | revoke（仅本机或带 X-Admin-Token）
	mux.HandleFunc("/api/v1/share/create", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var req file_gateway.ShareCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if shareStore == nil {
			writeJSON(w, 500, map[string]string{"error": "share store not initialized"})
			return
		}
		sh, err := shareStore.Create(req)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "share_create", sh.Target, "success", "code="+sh.Code, r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"code":       sh.Code,
				"target":     sh.Target,
				"perm":       sh.Perm,
				"expires_at": sh.ExpiresAt,
				"path":       "/s/" + sh.Code,
				"lan_url":    shareLanURL(cfg, sh.Code),
				"public_url": sharePublicURL(cfg, sh.Code),
			},
		})
	})

	mux.HandleFunc("/api/v1/share/list", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		items := []*file_gateway.Share{}
		if shareStore != nil {
			items = shareStore.List()
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{"items": items},
		})
	})

	mux.HandleFunc("/api/v1/share/revoke", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
			writeJSON(w, 400, map[string]string{"error": "code is required"})
			return
		}
		if shareStore == nil {
			writeJSON(w, 500, map[string]string{"error": "share store not initialized"})
			return
		}
		if err := shareStore.Revoke(req.Code); err != nil {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		audit.Log("admin", "share_revoke", req.Code, "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	})

	// 设备自查状态（客户端轮询，需求 1）：无需管理员，仅返回该设备自身状态
	// CA 证书下载（回环管理端/桌面端使用；CA 是公开信息，无私钥）
	mux.HandleFunc("/api/v1/tls/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		if activeTLS == nil {
			writeJSON(w, 404, map[string]string{"error": "tls disabled"})
			return
		}
		handleCACert(activeTLS)(w, r)
	})

	// 系统指标（关于页）：CPU / 内存 / 磁盘 / 本程序占用
	mux.HandleFunc("/api/v1/system/metrics", handleSystemMetrics(cfg))

	// 证书信息（指纹 / SAN / CA 下载地址）
	mux.HandleFunc("/api/v1/tls/info", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		handleTLSInfo(activeTLS, cfg)(w, r)
	})

	// 二维码生成（桌面端展示分享链接，供手机相机扫描）
	mux.HandleFunc("/api/v1/qr", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		handleQRCode(w, r)
	})

	// 网络信息（需求：分享链接要用 LAN 地址，而不是 127.0.0.1）
	mux.HandleFunc("/api/v1/system/network", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"lan_ips":     lanIPv4List(),
				"file_port":   cfg.HTTPPort,
				"auth_port":   cfg.AuthPort,
				"web_port":    cfg.WebPort,
				"server_name": cfg.ServerName,
				"tunnel_url":  tunnelCurrentURL(cfg),
			},
		})
	})

	// 管理通道令牌（需求 3）：仅本机回环可取，供服务端 GUI 访问公共文件
	mux.HandleFunc("/api/v1/admin/token", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"admin_token": cfg.AdminSecret,
				"server_name": cfg.ServerName,
			},
		})
	})

	// 客户端主动退出服务器（设备令牌鉴权，无需管理员）：撤销令牌 + 移除设备记录（服务端计数随之变化）
	mux.HandleFunc("/api/v1/device/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		deviceID := security.CleanInput(r.Header.Get("X-Device-Id"), 64)
		if deviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "device_id required"})
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if _, err := auth.Validate(deviceID, token); err != nil {
			audit.Log(deviceID, "device_release", "", "denied", "invalid token", r.RemoteAddr)
			writeJSON(w, 401, map[string]string{"error": "invalid token"})
			return
		}
		auth.RevokeDevice(deviceID)
		if err := dm.DisconnectDevice(deviceID); err != nil {
			log.Printf("[Server] release: %v (设备可能已被服务端移除，视为已退出)", err)
		}
		audit.Log(deviceID, "device_release", "", "success", "", r.RemoteAddr)
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
	})

	mux.HandleFunc("/api/v1/device/status", func(w http.ResponseWriter, r *http.Request) {
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		if deviceID == "" {
			writeJSON(w, 400, map[string]string{"error": "device_id required"})
			return
		}

		status := "unknown"
		tokenValid := false
		if d, err := dm.GetDevice(deviceID); err == nil {
			status = string(d.Status)
			tokenValid = auth.HasToken(deviceID)
			if tokenValid {
				dm.MarkSeen(deviceID) // 轮询即心跳，供「已连接」判定
			}
		}

		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"status":         status,
				"token_valid":    tokenValid,
				"server_name":    cfg.ServerName,
				"server_version": AppVersion,
			},
		})
	})

	// System status
	mux.HandleFunc("/api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		// 只统计令牌仍有效的已连接设备（避免僵尸记录让计数虚高）
		liveConnectedCount := 0
		for _, d := range dm.ListDevices(string(StatusConnected)) {
			if auth.HasToken(d.ID) && d.LastSeenAt != nil && time.Since(*d.LastSeenAt) <= 60*time.Second {
				liveConnectedCount++
			}
		}
		// 需求 2/6：非本机调用只返回客户端必需字段，不暴露设备规模
		if ip := net.ParseIP(security.ClientIP(r)); ip == nil || !ip.IsLoopback() {
			writeJSON(w, 200, map[string]interface{}{
				"code": 0,
				"data": map[string]interface{}{
					"version":     AppVersion,
					"status":      "running",
					"server_name": cfg.ServerName,
				},
			})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"version":     AppVersion,
				"status":      "running",
				"devices":     len(dm.ListDevices("")),
				"connected":   liveConnectedCount,
				"server_name": cfg.ServerName,
			},
		})
	})

	// System config
	mux.HandleFunc("/api/v1/system/config", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			// 脱敏：密钥不通过配置接口回传（需求 3 的安全加固）
			safe := cfg
			safe.AuthSecret = ""
			safe.AdminSecret = ""
			writeJSON(w, 200, map[string]interface{}{"code": 0, "data": safe})
		case http.MethodPost:
			var newCfg ServerConfig
			if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
				writeJSON(w, 400, map[string]string{"error": "invalid body"})
				return
			}
			if err := validateNewConfig(newCfg); err != nil {
				writeJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			// 密钥不接受远程修改，避免改错后所有设备掉线
			newCfg.AuthSecret = cfg.AuthSecret
			newCfg.AdminSecret = cfg.AdminSecret
			cfg = newCfg
			log.Printf("[Server] config updated; restart required for port/bind changes to take effect")
			saveConfig(configPath, cfg)
			writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok"})
		default:
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
		}
	})
}

// ========== Web Service Routes ==========

//go:embed web/index.html
var webIndexHTML string

func setupWebRoutes(mux *http.ServeMux, dm *DeviceManager, auth *file_gateway.Authenticator,
	quota *file_gateway.QuotaManager, audit *file_gateway.AuditLogger, cfg ServerConfig, configPath string) {

	log.Printf("[WebService] Embedded web interface loaded (%d bytes)", len(webIndexHTML))

	// Serve static files (HTML/CSS/JS)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" || strings.HasPrefix(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(webIndexHTML))
		} else {
			http.NotFound(w, r)
		}
	})

	// Reuse auth routes for API
	setupAuthRoutes(mux, dm, auth, quota, audit, cfg, configPath)
}

// ========== Helpers ==========

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// 需求 6：出口统一脱敏（错误信息不泄露本地路径/堆栈）
	json.NewEncoder(w).Encode(security.SanitizeResponse(data))
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range filepath.SplitList(path) {
		if p != "" {
			parts = append(parts, p)
		}
	}
	// Split by /
	result := []string{}
	for _, p := range split(path, '/') {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func split(s string, sep byte) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if i > start {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}

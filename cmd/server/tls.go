package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ========== 本地 CA + 服务器证书（HTTPS，无需联网、无需第三方 CA） ==========
//
// 设计：
//   - 首次启动生成一棵自签根 CA（10 年），存放在 data/tls/
//   - 用 CA 签发服务器证书，SAN 覆盖 localhost / 局域网 IP / 组网 IP / 主机名
//   - IP 变化或证书 30 天内到期时自动重签
//   - 局域网端口走 HTTPS；回环地址保留 HTTP（桌面端与 组网公网访问 不受影响）

// activeTLS 启动时注入，供 /api/v1/tls/info 使用
var activeTLS *tlsMaterial

type tlsMaterial struct {
	CertPath    string
	KeyPath     string
	CAPath      string
	CAPEM       []byte
	Fingerprint string
	SANs        []string
	NotAfter    time.Time
}

func tlsDir(cfg ServerConfig) string { return filepath.Join(cfg.DataDir, "tls") }

func newSerial() *big.Int {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}

// allIPv4 返回所有非回环 IPv4（含 组网 的 100.x）
func allIPv4() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

// desiredSANs 计算证书应包含的域名/IP
func desiredSANs() []string {
	set := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	if h, err := os.Hostname(); err == nil && h != "" {
		set[h] = true
		set[strings.ToLower(h)] = true
		set[strings.ToLower(h)+".local"] = true
	}
	for _, ip := range allIPv4() {
		set[ip] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	if cb == nil {
		return nil, nil, fmt.Errorf("bad ca cert pem")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	kb, _ := pem.Decode(keyPEM)
	if kb == nil {
		return nil, nil, fmt.Errorf("bad ca key pem")
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	return cert, key, nil
}

func parseLeaf(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	return parseCA(certPEM, keyPEM)
}

func certSANSet(c *x509.Certificate) []string {
	set := map[string]bool{}
	for _, d := range c.DNSNames {
		set[strings.ToLower(d)] = true
	}
	for _, ip := range c.IPAddresses {
		set[ip.String()] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ensureTLSMaterial 生成或复用 CA/服务器证书
func ensureTLSMaterial(cfg ServerConfig) (*tlsMaterial, error) {
	dir := tlsDir(cfg)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	caCertPath := filepath.Join(dir, "ca.crt")
	caKeyPath := filepath.Join(dir, "ca.key")
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	// ---- 1) 根 CA ----
	var caCert *x509.Certificate
	var caKey *ecdsa.PrivateKey
	if cb, err := os.ReadFile(caCertPath); err == nil {
		if kb, err2 := os.ReadFile(caKeyPath); err2 == nil {
			caCert, caKey, _ = parseCA(cb, kb)
		}
	}
	if caCert == nil || caKey == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		tmpl := &x509.Certificate{
			SerialNumber:          newSerial(),
			Subject:               pkix.Name{CommonName: "FrostLeaves Local CA", Organization: []string{"FrostLeaves"}},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().AddDate(10, 0, 0),
			KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
			BasicConstraintsValid: true,
			IsCA:                  true,
			MaxPathLenZero:        true,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		if err := writePEM(caCertPath, "CERTIFICATE", der); err != nil {
			return nil, err
		}
		kb, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := writePEM(caKeyPath, "EC PRIVATE KEY", kb); err != nil {
			return nil, err
		}
		caCert, err = x509.ParseCertificate(der)
		if err != nil {
			return nil, err
		}
		caKey = key
	}

	// ---- 2) 服务器证书 ----
	want := desiredSANs()
	needSign := true
	if cb, err := os.ReadFile(certPath); err == nil {
		if kb, err2 := os.ReadFile(keyPath); err2 == nil {
			if leaf, _, err3 := parseLeaf(cb, kb); err3 == nil {
				if sameStrings(certSANSet(leaf), want) && time.Now().AddDate(0, 0, 30).Before(leaf.NotAfter) {
					needSign = false
				}
			}
		}
	}
	if needSign {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		tmpl := &x509.Certificate{
			SerialNumber:          newSerial(),
			Subject:               pkix.Name{CommonName: "FrostLeaves Server", Organization: []string{"FrostLeaves"}},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().AddDate(2, 0, 0),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			BasicConstraintsValid: true,
		}
		for _, s := range want {
			if ip := net.ParseIP(s); ip != nil {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			} else {
				tmpl.DNSNames = append(tmpl.DNSNames, s)
			}
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			return nil, err
		}
		if err := writePEM(certPath, "CERTIFICATE", der); err != nil {
			return nil, err
		}
		kb, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := writePEM(keyPath, "EC PRIVATE KEY", kb); err != nil {
			return nil, err
		}
	}

	caPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(caCert.Raw)
	fp := strings.ToUpper(hex.EncodeToString(sum[:]))
	var parts []string
	for i := 0; i+2 <= len(fp); i += 2 {
		parts = append(parts, fp[i:i+2])
	}

	return &tlsMaterial{
		CertPath:    certPath,
		KeyPath:     keyPath,
		CAPath:      caCertPath,
		CAPEM:       caPEM,
		Fingerprint: strings.Join(parts, ":"),
		SANs:        want,
		NotAfter:    caCert.NotAfter,
	}, nil
}

func writePEM(path, blockType string, der []byte) error {
	buf := pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
	return os.WriteFile(path, buf, 0600)
}

func loadTLSConfig(m *tlsMaterial) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(m.CertPath, m.KeyPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// handleCACert 提供 CA 证书下载（公开信息，无密钥）
func handleCACert(m *tlsMaterial) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", "attachment; filename=frostleaves-ca.crt")
		_, _ = w.Write(m.CAPEM)
	}
}

// handleTLSInfo 返回证书指纹等信息（管理员）
func handleTLSInfo(m *tlsMaterial, cfg ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bsPort := cfg.BootstrapPort
		if bsPort == 0 {
			bsPort = cfg.WebPort + 1
		}
		lan := ""
		if ips := lanIPv4List(); len(ips) > 0 {
			lan = ips[0]
		}
		caURL := ""
		if lan != "" && m != nil {
			caURL = fmt.Sprintf("http://%s:%d/ca.crt", lan, bsPort)
		}
		data := map[string]interface{}{
			"enabled":      m != nil,
			"fingerprint":  "",
			"sans":         []string{},
			"ca_url":       caURL,
			"bootstrap_port": bsPort,
			"https_web":    "",
		}
		if m != nil {
			data["fingerprint"] = m.Fingerprint
			data["sans"] = m.SANs
			data["not_after"] = m.NotAfter.Format(time.RFC3339)
			if lan != "" {
				data["https_web"] = fmt.Sprintf("https://%s:%d", lan, cfg.WebPort)
			}
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": data})
	}
}
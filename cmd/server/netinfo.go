package main

import (
	"fmt"
	"net"
	"strings"
)

// lanIPv4List 返回可用于局域网访问的本机 IPv4（供分享链接使用，避免给出 127.0.0.1）
func lanIPv4List() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		skip := false
		for _, k := range []string{"mesh", "wsl", "docker", "veth", "vmware", "hyper-v", "vethernet", "virtualbox", "loopback", "bluetooth"} {
			if strings.Contains(name, k) {
				skip = true
				break
			}
		}
		if skip {
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
			if isPrivateIPv4(ip) {
				out = append(out, ip.String())
			}
		}
	}
	return out
}

func isPrivateIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	switch {
	case ip4[0] == 10:
		return true
	case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
		return true
	case ip4[0] == 192 && ip4[1] == 168:
		return true
	}
	return false
}

// shareLanURL 生成分享的内网访问地址（局域网已启用 HTTPS）
func shareLanURL(cfg ServerConfig, code string) string {
	if ips := lanIPv4List(); len(ips) > 0 {
		return fmt.Sprintf("https://%s:%d/s/%s", ips[0], cfg.WebPort, code)
	}
	return fmt.Sprintf("http://127.0.0.1:%d/s/%s", cfg.WebPort, code)
}

// sharePublicURL 生成分享的外网访问地址（未开隧道返回空串）
func sharePublicURL(cfg ServerConfig, code string) string {
	u := tunnelCurrentURL(cfg)
	if u == "" {
		return ""
	}
	return strings.TrimRight(u, "/") + "/s/" + code
}

// tunnelCurrentURL 读取当前 Funnel 公网地址（未开启返回空串）
func tunnelCurrentURL(cfg ServerConfig) string {
	enabled, url, _ := tunnelStatus(cfg)
	if !enabled {
		return ""
	}
	return url
}

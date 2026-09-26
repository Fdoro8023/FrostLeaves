package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ========== 点对点组网公网访问（Funnel）集成 ==========
//
// 原实现的问题：直接执行组网 CLI，一旦它不在 PATH（Windows 上通常装在独立
// 安装目录，路径不在 PATH 里）就整体报错，且错误信息笼统。
//
// 本版改进：
//  1. 可执行文件解析：配置绝对路径 > PATH > Windows 常见安装目录
//  2. 新增 /api/v1/tunnel/detect：先探测 CLI 是否可用，界面可明确提示
//  3. 开启时依次尝试多种命令变体（--yes / --https=443），把 CLI 原始输出回传
//  4. 状态解析同时支持 --json 与文本输出

var tunnelURLRe = regexp.MustCompile(`https://[A-Za-z0-9._-]+`)

// windowsMeshPaths：Windows 上组网客户端的常见安装位置。
// 如你的客户端不在 PATH 中，请在此补充它的绝对路径（或直接填到「组网命令」配置里）。
var windowsMeshPaths = []string{}

func tunnelCmdName(cfg ServerConfig) string {
	c := strings.TrimSpace(cfg.TunnelCommand)
	if c == "" {
		return "mesh"
	}
	return c
}

// resolveTunnelExe 解析组网客户端可执行文件
func resolveTunnelExe(cfg ServerConfig) (string, error) {
	cmd := tunnelCmdName(cfg)

	if filepath.IsAbs(cmd) {
		if _, err := os.Stat(cmd); err != nil {
			return "", fmt.Errorf("配置的隧道命令不存在：%s", cmd)
		}
		return cmd, nil
	}

	if p, err := exec.LookPath(cmd); err == nil {
		return p, nil
	}

	for _, candidate := range windowsMeshPaths {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("未找到 %s 可执行文件：请先安装点对点私有组网客户端；若已安装，请在系统配置的「组网命令」里填绝对路径（例如 C:/Program Files/YourMeshClient/mesh.exe）", cmd)
}

func runTunnel(exe string, args ...string) (string, error) {
	// 单条 CLI 命令 12 秒上限：避免总耗时超过客户端超时（需求 7 修正）
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("命令执行失败")
	}
	return text, nil
}

func tunnelTargetPort(cfg ServerConfig) int {
	if cfg.TunnelPort > 0 {
		return cfg.TunnelPort
	}
	return cfg.WebPort
}

// ========== 探测 ==========

// tunnelDetect 探测 CLI 是否可用（供界面显示明确提示）
func tunnelDetect(cfg ServerConfig) map[string]interface{} {
	exe, err := resolveTunnelExe(cfg)
	if err != nil {
		return map[string]interface{}{
			"available": false,
			"exe":       "",
			"version":   "",
			"message":   err.Error(),
		}
	}

	version, verr := runTunnel(exe, "version")
	if verr != nil {
		return map[string]interface{}{
			"available": false,
			"exe":       exe,
			"version":   "",
			"message":   "已找到 " + exe + "，但执行 version 失败：" + version,
		}
	}

	return map[string]interface{}{
		"available": true,
		"exe":       exe,
		"version":   strings.TrimSpace(version),
		"message":   "组网客户端可用",
	}
}

// ========== 状态 ==========

func tunnelStatus(cfg ServerConfig) (bool, string, string) {
	exe, err := resolveTunnelExe(cfg)
	if err != nil {
		return false, "", err.Error()
	}

	outJSON, _ := runTunnel(exe, "funnel", "status", "--json")
	if url := findFunnelURL(outJSON); url != "" {
		return true, url, "Funnel 已开启"
	}

	out, err2 := runTunnel(exe, "funnel", "status")
	if err2 != nil {
		return false, "", "读取状态失败：" + out
	}
	if url := tunnelURLRe.FindString(out); url != "" {
		return true, url, "Funnel 已开启"
	}

	low := strings.ToLower(out)
	if strings.TrimSpace(out) == "" || strings.Contains(low, "no funnel") || strings.Contains(low, "not enabled") {
		return false, "", "Funnel 未开启"
	}
	return false, "", strings.TrimSpace(out)
}

func findFunnelURL(v interface{}) string {
	switch t := v.(type) {
	case string:
		if u := tunnelURLRe.FindString(t); u != "" {
			return u
		}
	case []interface{}:
		for _, it := range t {
			if u := findFunnelURL(it); u != "" {
				return u
			}
		}
	case map[string]interface{}:
		for _, it := range t {
			if u := findFunnelURL(it); u != "" {
				return u
			}
		}
	}
	return ""
}

// funnelURLFromJSON 先做一次 JSON 解析，失败则直接正则兜底
func funnelURLFromJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[") {
		var v interface{}
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			if u := findFunnelURL(v); u != "" {
				return u
			}
		}
	}
	return tunnelURLRe.FindString(s)
}

// ========== 开启 / 关闭 ==========

// tunnelEnable 依次尝试多种命令变体，返回 (公网地址, 诊断输出, error)
func tunnelEnable(cfg ServerConfig) (string, string, error) {
	exe, err := resolveTunnelExe(cfg)
	if err != nil {
		return "", err.Error(), err
	}

	port := tunnelTargetPort(cfg)
	portStr := strconv.Itoa(port)
	local := "http://127.0.0.1:" + portStr

	variants := [][]string{
		{"funnel", "--bg", "--yes", portStr},
		{"funnel", "--bg", portStr},
		{"funnel", "--bg", "--yes", "--https=443", local},
		{"funnel", "--bg", "--https=443", local},
		{"funnel", portStr},
	}

	// 预检：tailnet 前置条件不满足时直接返回可操作提示，不再逐个变体空跑
	if pf := tunnelPreflight(cfg); pf["ok"] != true {
		if m, ok := pf["message"].(string); ok && m != "" {
			return "", m, fmt.Errorf("组网前置条件未满足")
		}
	}

	var logs []string
	for _, args := range variants {
		out, runErr := runTunnel(exe, args...)
		logs = append(logs, "$ "+exe+" "+strings.Join(args, " ")+"  =>  "+out)
		if runErr == nil {
			enabled, url, msg := tunnelStatus(cfg)
			if enabled {
				log.Printf("[Tunnel] funnel enabled via: %s", strings.Join(args, " "))
				return url, strings.Join(logs, ""), nil
			}
			return "", strings.Join(logs, ""), fmt.Errorf("命令执行成功，但未取到公网地址（%s）。请确认已在 组网服务管理台为组网启用公网访问", msg)
		}
	}

	return "", strings.Join(logs, ""), fmt.Errorf("所有命令变体均失败，请查看下方原始输出")
}

func tunnelDisable(cfg ServerConfig) (string, error) {
	exe, err := resolveTunnelExe(cfg)
	if err != nil {
		return "", err
	}

	out, err := runTunnel(exe, "funnel", "reset")
	if err == nil {
		log.Printf("[Tunnel] funnel disabled")
		return out, nil
	}

	out2, err2 := runTunnel(exe, "funnel", "--bg", "off")
	if err2 == nil {
		log.Printf("[Tunnel] funnel disabled (legacy)")
		return out2, nil
	}
	return out + "\n" + out2, fmt.Errorf("关闭失败")
}

// tunnelPreflight 预检 tailnet 前置条件（需求 7 增强）
// Funnel 需要：1) tailnet 启用 HTTPS Certificates；2) 节点被授予 funnel 能力
func tunnelPreflight(cfg ServerConfig) map[string]interface{} {
	exe, err := resolveTunnelExe(cfg)
	if err != nil {
		return map[string]interface{}{"ok": false, "message": err.Error()}
	}

	out, err := runTunnel(exe, "status", "--json")
	if err != nil {
		return map[string]interface{}{"ok": false, "message": "读取组网状态失败：" + out}
	}

	funnelGranted := strings.Contains(out, "funnel")
	msg := ""
	if !funnelGranted {
		msg = "组网尚未满足公网访问前置条件：请到组网服务管理台 1) 启用 HTTPS Certificates（DNS 页面）；2) 在 Access Controls 里给该节点授予 funnel 属性（nodeAttrs: {\"target\": [\"*\"], \"attr\": [\"funnel\"]}）。两处都打开后重试。"
	}
	return map[string]interface{}{
		"ok":             funnelGranted,
		"funnel_granted": funnelGranted,
		"message":        msg,
	}
}

// tunnelHint 针对已知的组网 CLI 报错给出可操作提示
func tunnelHint(out string) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "already in use") || strings.Contains(low, "401"):
		return "检测到组网客户端本地实例冲突（401 Unauthorized: already in use by <用户>）。" +
			"常见原因：组网客户端以某个 Windows 用户身份运行，而服务进程用另一个身份访问本地 API。" +
			"建议：1) 管理员 PowerShell 执行组网客户端的状态命令看是否同样报错；" +
			"2) 在托盘退出重复实例，或重启组网客户端服务后重试；" +
			"3) 确认组网客户端已登录且状态为已连接。"
	case strings.Contains(low, "https") && (strings.Contains(low, "not enabled") || strings.Contains(low, "certificate") || strings.Contains(low, "cert")):
		return "组网未启用 HTTPS 证书：请到 组网服务管理台 → DNS → 打开 HTTPS Certificates，然后重试。"
	case strings.Contains(low, "funnel") && strings.Contains(low, "acl"):
		return "ACL 未授予 funnel 属性：请在管理台 Access Controls 里为节点加上 nodeAttrs 的 funnel。"
	case strings.Contains(low, "not enabled"):
		return "组网尚未启用公网访问：请先到 组网服务管理台为该组网打开公网访问，再重试。"
	case strings.Contains(low, "logged out") || strings.Contains(low, "not logged in"):
		return "组网客户端尚未登录：请在托盘图标完成登录后重试。"
	}
	return ""
}

// ========== 路由 ==========

func setupTunnelRoutes(mux *http.ServeMux, cfg ServerConfig) {
	mux.HandleFunc("/api/v1/tunnel/status", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		enabled, url, msg := tunnelStatus(cfg)
		if !enabled {
			if h := tunnelHint(msg); h != "" {
				msg = h
			}
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": map[string]interface{}{
				"enabled":   enabled,
				"url":       url,
				"port":      tunnelTargetPort(cfg),
				"command":   tunnelCmdName(cfg),
				"message":   msg,
				"share_url": sharePageURL(url),
			},
		})
	})

	mux.HandleFunc("/api/v1/tunnel/detect", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		detected := tunnelDetect(cfg)
		if detected["available"] == true {
			detected["preflight"] = tunnelPreflight(cfg)
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"data": detected,
		})
	})

	mux.HandleFunc("/api/v1/tunnel/enable", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		url, diag, err := tunnelEnable(cfg)
		if err != nil {
			if h := tunnelHint(diag); h != "" {
				diag = diag + "\n\n【建议】" + h
			}
			writeJSON(w, 500, map[string]interface{}{
				"error":  err.Error(),
				"output": diag,
			})
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"code": 0,
			"msg":  "ok",
			"data": map[string]interface{}{
				"url":       url,
				"share_url": sharePageURL(url),
				"output":    diag,
			},
		})
	})

	mux.HandleFunc("/api/v1/tunnel/disable", func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		out, err := tunnelDisable(cfg)
		if err != nil {
			writeJSON(w, 500, map[string]interface{}{"error": err.Error(), "output": out})
			return
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "msg": "ok", "data": map[string]string{"output": out}})
	})
}

// sharePageURL 拼接访客页地址前缀
func sharePageURL(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/s/"
}
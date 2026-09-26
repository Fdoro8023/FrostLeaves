//go:build windows

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

// ========== 双击启动的友好提示（避免控制台一闪而过看不到原因） ==========

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x00000000
	mbIconError     = 0x00000010
	mbIconWarning   = 0x00000030
	mbIconInfo      = 0x00000040
	mbSetForeground = 0x00010000
)

// showMessageBox 弹系统对话框（无控制台时也能看到原因）
func showMessageBox(title, text string, flags uintptr) {
	// 自动化/服务方式启动时跳过弹窗，避免阻塞
	if os.Getenv("FROSTLEAVES_NO_MSGBOX") != "" {
		log.Printf("[MsgBox] %s: %s", title, strings.ReplaceAll(text, "\n", " | "))
		return
	}
	t, err1 := syscall.UTF16PtrFromString(title)
	m, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	procMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		flags|mbSetForeground)
}

// openBrowser 打开系统浏览器
func openBrowser(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// firstBusyPort 返回第一个已被占用的服务端口（0 = 全部可用）
func firstBusyPort(cfg ServerConfig) int {
	ports := []int{cfg.HTTPPort, cfg.AuthPort, cfg.WebPort}
	if cfg.BootstrapPort > 0 {
		ports = append(ports, cfg.BootstrapPort)
	}
	for _, p := range ports {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			return p
		}
		_ = ln.Close()
	}
	return 0
}

// checkAlreadyRunning 双击启动时给出可读提示（而不是闪退）
// 返回 true 表示已经有一个实例在运行，本进程应当退出
func checkAlreadyRunning(cfg ServerConfig) bool {
	busy := firstBusyPort(cfg)
	if busy == 0 {
		return false
	}
	lan := ""
	if ips := lanIPv4List(); len(ips) > 0 {
		lan = ips[0]
	}
	msg := fmt.Sprintf("Frost Leaves 服务端已经在运行。\n\n"+
		"端口 %d 被占用，无法启动第二个实例。\n\n"+
		"怎样找到它：任务管理器（Ctrl+Shift+Esc）\n"+
		"  ·「详细信息」标签页 → 找 FrostLeaves_Server.exe\n"+
		"  · 或在「进程」标签页的「后台进程」分组里找\n"+
		"结束它即可重新启动；也可直接使用正在运行的服务（即将为你打开页面）。", busy)
	log.Printf("[Server] port %d already in use; another instance is probably running", busy)
	showMessageBox("Frost Leaves 服务端", msg, mbIconInfo)
	openBrowser(fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort))
	if lan != "" {
		log.Printf("[Server] LAN page: https://%s:%d", lan, cfg.WebPort)
	}
	return true
}
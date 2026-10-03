//go:build windows

package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"frostleaves/pkg/metrics"
)

// ========== 系统指标（关于页用；纯标准库 + Windows API，无第三方依赖） ==========

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	psapi    = syscall.NewLazyDLL("psapi.dll")

	procGlobalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
	procGetSystemTimes       = kernel32.NewProc("GetSystemTimes")
	procGetDiskFreeSpaceExW  = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetProcessMemoryInfo = psapi.NewProc("GetProcessMemoryInfo")
	procGetProcessTimes      = kernel32.NewProc("GetProcessTimes")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func systemMemory() (total, used uint64) {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms)))
	if r == 0 {
		return 0, 0
	}
	return ms.TotalPhys, ms.TotalPhys - ms.AvailPhys
}

func diskUsage(path string) (total, free uint64) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0
	}
	var freeAvailable, totalBytes, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvailable)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0
	}
	return totalBytes, totalFree
}

type processMemoryCounters struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

type processMemoryCountersEx struct {
	CB                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
	PrivateUsage               uintptr
}

// currentProcessMemoryEx returns (working set, private bytes) in bytes.
// Private bytes is the Windows process private working set proxy used for the
// dual-view memory panel (模块 6).
func currentProcessMemoryEx() (uint64, uint64) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, 0
	}
	var pmc processMemoryCountersEx
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
	if r == 0 {
		return 0, 0
	}
	return uint64(pmc.WorkingSetSize), uint64(pmc.PrivateUsage)
}

func currentProcessMemory() uint64 {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0
	}
	var pmc processMemoryCounters
	pmc.CB = uint32(unsafe.Sizeof(pmc))
	r, _, _ := procGetProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&pmc)), uintptr(pmc.CB))
	if r == 0 {
		return 0
	}
	return uint64(pmc.WorkingSetSize)
}

var (
	cpuMu      sync.Mutex
	lastIdle   uint64
	lastKernel uint64
	lastUser   uint64
	lastSample time.Time
)

func filetimeToUint64(ft syscall.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}

// cpuUsage 返回距上次调用之间的 CPU 使用率（0-100）；首次调用建立基线返回 0
func cpuUsage() float64 {
	var idle, kernel, user syscall.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return 0
	}
	i := filetimeToUint64(idle)
	k := filetimeToUint64(kernel)
	u := filetimeToUint64(user)

	cpuMu.Lock()
	defer cpuMu.Unlock()
	if lastSample.IsZero() {
		lastIdle, lastKernel, lastUser, lastSample = i, k, u, time.Now()
		return 0
	}
	dIdle := i - lastIdle
	dKernel := k - lastKernel
	dUser := u - lastUser
	lastIdle, lastKernel, lastUser, lastSample = i, k, u, time.Now()
	total := dKernel + dUser
	if total == 0 {
		return 0
	}
	busy := total - dIdle
	pct := float64(busy) / float64(total) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

var (
	lastProcKernel uint64
	lastProcUser   uint64
	lastProcWall   time.Time
)

// processCPUPercent 本进程 CPU 占用（占全部核心的百分比）
func processCPUPercent(cores int) float64 {
	h, err := syscall.GetCurrentProcess()
	if err != nil || cores <= 0 {
		return 0
	}
	var creation, exit, kernel, user syscall.Filetime
	r, _, _ := procGetProcessTimes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return 0
	}
	k := filetimeToUint64(kernel)
	u := filetimeToUint64(user)
	now := time.Now()

	cpuMu.Lock()
	defer cpuMu.Unlock()
	if lastProcWall.IsZero() {
		lastProcKernel, lastProcUser, lastProcWall = k, u, now
		return 0
	}
	dCPU := (k - lastProcKernel) + (u - lastProcUser)
	dWall100ns := uint64(now.Sub(lastProcWall).Nanoseconds() / 100)
	lastProcKernel, lastProcUser, lastProcWall = k, u, now
	if dWall100ns == 0 {
		return 0
	}
	pct := float64(dCPU) / (float64(dWall100ns) * float64(cores)) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

// cpuCount 逻辑核心数
func cpuCount() int {
	return runtime.NumCPU()
}

// handleSystemMetrics 返回 CPU/内存/磁盘与 FrostLeaves 自身占用（管理员）
func handleSystemMetrics(cfg ServerConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !adminAllowed(w, r, &cfg) {
			return
		}

		memTotal, memUsed := systemMemory()
		diskPath := cfg.DataDir
		if abs, err := filepath.Abs(diskPath); err == nil {
			diskPath = abs
		}
		if _, err := os.Stat(diskPath); err != nil {
			diskPath = "."
		}
		diskTotal, diskFree := diskUsage(diskPath)
		workingSet, procMem := currentProcessMemoryEx()
		goMem := metrics.GoMemStats()
		storage := metrics.ScanStorage(cfg.StorageRoot)
		cpuApp, cpu := currentCPUReadings()
		cores := cpuCount()

		// FrostLeaves 占用：服务端进程内存（磁盘占用按 storage 目录大小近似）
		diskUsed := uint64(0)
		if diskTotal > diskFree {
			diskUsed = diskTotal - diskFree
		}
		storageBytes := storage.TotalBytes

		data := map[string]interface{}{
			"version":   AppVersion,
			"cpu_cores": cores,
			// 进程 CPU 以单核为 100%，多核并发可超 100%（1 秒间隔两次采样差值）
			"cpu_app":        cpuApp,
			"cpu_percent":    cpu,
			"cpu_multi_core": cores > 1,
			"cpu_note":       "CPU 为 1 秒间隔两次采样的瞬时值；进程占用以单核为 100%，多核并发时可超过 100%（最高 " + strconv.Itoa(cores*100) + "%）。",
			// 内存双视角：专用工作集（对照任务管理器）与 Go 堆
			"mem_total":           memTotal,
			"mem_used":            memUsed,
			"mem_app":             procMem,
			"mem_app_private":     procMem,
			"mem_app_working_set": workingSet,
			"mem_app_heap":        goMem.HeapAllocBytes,
			"mem_app_heap_sys":    goMem.HeapSysBytes,
			"mem_app_sys":         goMem.SysBytes,
			"mem_app_stack":       goMem.StackBytes,
			// 存储：真实磁盘文件大小；回收站单列，合计与配额口径一致
			"disk_total":       diskTotal,
			"disk_used":        diskUsed,
			"disk_free":        diskFree,
			"disk_app":         storageBytes,
			"disk_app_storage": storage.StorageBytes,
			"disk_app_recycle": storage.RecycleBytes,
			"disk_path":        diskPath,
			"timestamp":        time.Now().Unix(),
		}
		writeJSON(w, 200, map[string]interface{}{"code": 0, "data": data})
	}
}

// dirSize 递归统计目录大小（用于展示 FrostLeaves 的存储占用）
func dirSize(root string) uint64 {
	var total uint64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total
}

// ========== 模块 6：1 秒间隔动态采样 ==========

var (
	metricsMu        sync.RWMutex
	metricsCPUApp    float64
	metricsCPUSystem float64
)

var (
	sysCPUSampler = metrics.NewCPUSampler(readSystemCPU)
	appCPUSampler = metrics.NewProcessSampler(readProcessCPU, time.Now)
)

// startMetricsSampler runs the 1-second sampling loop consumed by the panel.
func startMetricsSampler() {
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			app, _ := appCPUSampler.Sample()
			sys, _ := sysCPUSampler.Sample()
			metricsMu.Lock()
			metricsCPUApp = app
			metricsCPUSystem = sys
			metricsMu.Unlock()
		}
	}()
}

// currentCPUReadings returns the latest instantaneous CPU readings.
func currentCPUReadings() (float64, float64) {
	metricsMu.RLock()
	defer metricsMu.RUnlock()
	return metricsCPUApp, metricsCPUSystem
}

func readSystemCPU() (metrics.CPUCounter, error) {
	var idle, kernel, user syscall.Filetime
	r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return metrics.CPUCounter{}, syscall.EINVAL
	}
	return metrics.CPUCounter{
		Idle:   filetimeToUint64(idle),
		Kernel: filetimeToUint64(kernel),
		User:   filetimeToUint64(user),
	}, nil
}

func readProcessCPU() (uint64, error) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, err
	}
	var creation, exit, kernel, user syscall.Filetime
	r, _, _ := procGetProcessTimes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return 0, syscall.EINVAL
	}
	return filetimeToUint64(kernel) + filetimeToUint64(user), nil
}

// Package metrics provides dynamic, delta-based sampling for the monitoring
// panel (模块 6). All values are computed from successive samples so the panel
// reflects current load instead of a lifetime average.
package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// CPUCounter is a cumulative CPU-time reading. Values are in the platform
// counter unit (Windows FILETIME ticks on Windows).
type CPUCounter struct {
	Idle   uint64
	Kernel uint64
	User   uint64
}

// CPUReader returns the current cumulative system CPU counters.
type CPUReader func() (CPUCounter, error)

// CPUSampler computes the instantaneous system-wide CPU usage (0-100) from
// the delta between two consecutive reads. The first call only establishes a
// baseline and reports ready=false.
type CPUSampler struct {
	read CPUReader
	mu   sync.Mutex
	last CPUCounter
	ok   bool
}

// NewCPUSampler builds a sampler over read.
func NewCPUSampler(read CPUReader) *CPUSampler { return &CPUSampler{read: read} }

// Sample returns the CPU usage since the previous call.
func (s *CPUSampler) Sample() (float64, bool) {
	cur, err := s.read()
	if err != nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ok {
		s.last = cur
		s.ok = true
		return 0, false
	}
	dIdle := cur.Idle - s.last.Idle
	dKernel := cur.Kernel - s.last.Kernel
	dUser := cur.User - s.last.User
	s.last = cur
	total := dKernel + dUser
	if total == 0 {
		return 0, true
	}
	busy := total - dIdle
	pct := float64(busy) / float64(total) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// ProcessSampler computes process CPU usage as a percentage of a single core.
// A multi-threaded process running on several cores can therefore exceed 100%
// (up to NumCPU*100), which is what the panel shows.
type ProcessSampler struct {
	readBusy func() (uint64, error) // cumulative process CPU time (100ns ticks)
	now      func() time.Time
	mu       sync.Mutex
	lastBusy uint64
	lastWall time.Time
	ok       bool
}

// NewProcessSampler builds a process-CPU sampler.
func NewProcessSampler(readBusy func() (uint64, error), now func() time.Time) *ProcessSampler {
	if now == nil {
		now = time.Now
	}
	return &ProcessSampler{readBusy: readBusy, now: now}
}

// Sample returns the process CPU percentage since the previous call.
func (p *ProcessSampler) Sample() (float64, bool) {
	busy, err := p.readBusy()
	if err != nil {
		return 0, false
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ok {
		p.lastBusy, p.lastWall, p.ok = busy, now, true
		return 0, false
	}
	dBusy := busy - p.lastBusy
	dWall := now.Sub(p.lastWall)
	p.lastBusy, p.lastWall = busy, now
	if dBusy > busy { // counter reset
		return 0, true
	}
	if dWall <= 0 {
		return 0, true
	}
	// dBusy is in 100ns ticks; dWall in ns; both scaled to the same unit.
	pct := float64(dBusy) / float64(dWall.Nanoseconds()/100) * 100
	if pct < 0 {
		pct = 0
	}
	return pct, true
}

// MemoryView is the dual-view memory snapshot (模块 6).
type MemoryView struct {
	// Windows process private working set (comparable with Task Manager).
	PrivateBytes uint64 `json:"private_bytes"`
	// Windows process working set.
	WorkingSetBytes uint64 `json:"working_set_bytes"`
	// Go runtime heap.
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	StackBytes     uint64 `json:"stack_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
}

// GoMemStats reads the Go runtime heap view.
func GoMemStats() MemoryView {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return MemoryView{
		HeapAllocBytes: ms.HeapAlloc,
		HeapSysBytes:   ms.HeapSys,
		StackBytes:     ms.StackInuse,
		SysBytes:       ms.Sys,
	}
}

// DirUsage sums the real on-disk file sizes under root.
func DirUsage(root string) (uint64, error) {
	var total uint64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total, err
}

// StorageUsage breaks storage usage into payload and recycle bin (模块 2/6).
type StorageUsage struct {
	StorageBytes uint64 `json:"storage_bytes"`
	RecycleBytes uint64 `json:"recycle_bytes"`
	TotalBytes   uint64 `json:"total_bytes"`
}

// ScanStorage measures the storage root, splitting out the recycle bin so the
// panel can stay consistent with quota accounting.
func ScanStorage(storageRoot string) StorageUsage {
	recycleDir := filepath.Join(storageRoot, "recycle_bin")
	recycle, _ := DirUsage(recycleDir)
	all, _ := DirUsage(storageRoot)
	var payload uint64
	if all > recycle {
		payload = all - recycle
	}
	return StorageUsage{StorageBytes: payload, RecycleBytes: recycle, TotalBytes: all}
}

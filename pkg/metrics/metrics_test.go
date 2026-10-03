package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCPUSamplerFirstCallIsBaseline(t *testing.T) {
	s := NewCPUSampler(func() (CPUCounter, error) { return CPUCounter{Idle: 100, Kernel: 50, User: 50}, nil })
	if _, ready := s.Sample(); ready {
		t.Fatal("first sample must only establish a baseline")
	}
}

func TestCPUSamplerComputesDelta(t *testing.T) {
	readings := []CPUCounter{
		{Idle: 500, Kernel: 1000, User: 500},  // baseline
		{Idle: 1000, Kernel: 1800, User: 700}, // dIdle=500 dTotal=1000 -> 50%
	}
	i := 0
	s := NewCPUSampler(func() (CPUCounter, error) {
		c := readings[i]
		if i < len(readings)-1 {
			i++
		}
		return c, nil
	})
	s.Sample()
	pct, ready := s.Sample()
	if !ready {
		t.Fatal("second sample must be ready")
	}
	if pct < 49 || pct > 51 {
		t.Fatalf("want ~50%%, got %.2f", pct)
	}
}

func TestCPUSamplerRisesWithLoad(t *testing.T) {
	// idle-only first, then increasingly busy windows
	seq := []CPUCounter{
		{Idle: 0, Kernel: 0, User: 0},
		{Idle: 900, Kernel: 1000, User: 0},  // dIdle=900 dT=1000 -> 10%
		{Idle: 1300, Kernel: 2000, User: 0}, // dIdle=400 dT=1000 -> 60%
		{Idle: 1500, Kernel: 3000, User: 0}, // dIdle=200 dT=1000 -> 80%
	}
	i := 0
	s := NewCPUSampler(func() (CPUCounter, error) {
		c := seq[i]
		if i < len(seq)-1 {
			i++
		}
		return c, nil
	})
	s.Sample()
	var got []float64
	for n := 0; n < 3; n++ {
		p, _ := s.Sample()
		got = append(got, p)
	}
	if !(got[0] < got[1] && got[1] < got[2]) {
		t.Fatalf("CPU must follow rising load, got %v", got)
	}

	// and fall back after the load ends
	seq = []CPUCounter{{Idle: 2500, Kernel: 4000, User: 0}}
	i = 0
	p, _ := s.Sample()
	if p > 20 {
		t.Fatalf("CPU must fall after load ends, got %.2f", p)
	}
}

func TestProcessSamplerCanExceed100OnMultiCore(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	var busy uint64
	now := base
	p := NewProcessSampler(func() (uint64, error) { return busy, nil }, func() time.Time { return now })
	// baseline
	p.Sample()
	// one second wall, 4 seconds of CPU (4 cores busy) -> ~400%
	now = base.Add(1 * time.Second)
	busy = 4 * 1000 * 1000 * 10 // 1s = 1e7 ticks of 100ns
	pct, ready := p.Sample()
	if !ready {
		t.Fatal("expected ready")
	}
	if pct < 390 || pct > 410 {
		t.Fatalf("multi-core process CPU must exceed 100%%: got %.1f", pct)
	}
}

func TestDirUsageAndScanStorage(t *testing.T) {
	root := t.TempDir()
	storage := filepath.Join(root, "storage")
	recycle := filepath.Join(storage, "recycle_bin", "dev1")
	if err := os.MkdirAll(recycle, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "a.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recycle, "b.bin"), make([]byte, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ScanStorage(storage)
	if got.StorageBytes != 100 {
		t.Fatalf("storage payload = %d, want 100", got.StorageBytes)
	}
	if got.RecycleBytes != 40 {
		t.Fatalf("recycle = %d, want 40", got.RecycleBytes)
	}
	if got.TotalBytes != 140 {
		t.Fatalf("total = %d, want 140", got.TotalBytes)
	}
}

func TestGoMemStatsReportsHeap(t *testing.T) {
	// allocate to make the heap non-trivial
	blob := make([]byte, 4<<20)
	_ = blob
	mv := GoMemStats()
	if mv.HeapAllocBytes == 0 {
		t.Fatal("Go heap must be reported")
	}
	if mv.HeapSysBytes < mv.HeapAllocBytes {
		t.Fatalf("HeapSys (%d) must be >= HeapAlloc (%d)", mv.HeapSysBytes, mv.HeapAllocBytes)
	}
}

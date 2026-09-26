package file_gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ========== 审计日志 ==========
//
// 变化（见 CHANGES.md）：
//  1. 内存驻留条目上限 maxMemoryEntries，超出丢弃最旧的（防止长跑内存增长）；
//  2. 启动时从当日日志恢复最近 maxLoadEntries 条；
//  3. 文件写入使用缓冲 + 独立互斥锁。

const (
	maxMemoryEntries = 50000
	maxLoadEntries   = 20000
)

// Action constants
const (
	ActionFileUpload       = "file_upload"
	ActionFileDownload     = "file_download"
	ActionFileDelete       = "file_delete"
	ActionFileRename       = "file_rename"
	ActionFileList         = "file_list"
	ActionQuotaExceeded    = "quota_exceeded"
	ActionPermissionDenied = "permission_denied"
	ActionDeviceConnect    = "device_connect"
	ActionDeviceDisconnect = "device_disconnect"
	ActionDeviceReject     = "device_reject"
	ActionDeviceBlacklist  = "device_blacklist"
	ActionCodeGenerate     = "code_generate"
	ActionCodeVerify       = "code_verify"
	ActionCodeExpire       = "code_expire"
)

type AuditEntry struct {
	ID           int64     `json:"id"`
	DeviceID     string    `json:"device_id"`
	Action       string    `json:"action"`
	ResourcePath string    `json:"resource_path,omitempty"`
	Result       string    `json:"result"`
	Detail       string    `json:"detail,omitempty"`
	IPAddress    string    `json:"ip_address,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AuditLogger struct {
	mu      sync.Mutex
	entries []AuditEntry
	nextID  int64

	filePath string
	file     *os.File
	writer   *bufio.Writer
	fileMu   sync.Mutex
}

func NewAuditLogger(logDir string) (*AuditLogger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("audit_%s.log", time.Now().Format("2006-01-02")))

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}

	al := &AuditLogger{
		filePath: logPath,
		file:     file,
		writer:   bufio.NewWriterSize(file, 32*1024),
		entries:  make([]AuditEntry, 0, 1024),
		nextID:   1,
	}
	al.loadRecent(logPath)
	return al, nil
}

func (al *AuditLogger) loadRecent(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	var loaded []AuditEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e AuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		loaded = append(loaded, e)
	}
	if len(loaded) == 0 {
		return
	}
	if len(loaded) > maxLoadEntries {
		loaded = loaded[len(loaded)-maxLoadEntries:]
	}

	al.mu.Lock()
	al.entries = loaded
	maxID := int64(0)
	for _, e := range loaded {
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	al.nextID = maxID + 1
	al.mu.Unlock()

	log.Printf("[Audit] restored %d recent entries from %s", len(loaded), filepath.Base(path))
}

// Log 写入一条审计记录（内存 + 异步落盘）
func (al *AuditLogger) Log(deviceID, action, resourcePath, result, detail, ipAddress string) {
	entry := AuditEntry{
		DeviceID:     deviceID,
		Action:       action,
		ResourcePath: resourcePath,
		Result:       result,
		Detail:       detail,
		IPAddress:    ipAddress,
		CreatedAt:    time.Now(),
	}

	al.mu.Lock()
	entry.ID = al.nextID
	al.nextID++
	al.entries = append(al.entries, entry)
	if len(al.entries) > maxMemoryEntries {
		trim := len(al.entries) - maxMemoryEntries
		kept := make([]AuditEntry, len(al.entries)-trim)
		copy(kept, al.entries[trim:])
		al.entries = kept
	}
	al.mu.Unlock()

	go al.writeEntry(entry)
}

func (al *AuditLogger) writeEntry(entry AuditEntry) {
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[Audit] marshal error: %v", err)
		return
	}
	b = append(b, '\n')

	al.fileMu.Lock()
	defer al.fileMu.Unlock()

	if al.writer == nil || al.file == nil {
		return
	}
	if _, err := al.writer.Write(b); err != nil {
		log.Printf("[Audit] write error: %v", err)
		return
	}
	al.writer.Flush()
}

// Query 按条件过滤并分页
func (al *AuditLogger) Query(deviceID, action string, start, end time.Time, page, pageSize int) ([]AuditEntry, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	al.mu.Lock()
	defer al.mu.Unlock()

	filtered := make([]AuditEntry, 0, len(al.entries))
	for _, e := range al.entries {
		if deviceID != "" && e.DeviceID != deviceID {
			continue
		}
		if action != "" && e.Action != action {
			continue
		}
		if !start.IsZero() && e.CreatedAt.Before(start) {
			continue
		}
		if !end.IsZero() && e.CreatedAt.After(end) {
			continue
		}
		filtered = append(filtered, e)
	}

	total := len(filtered)
	startIdx := (page - 1) * pageSize
	if startIdx >= total {
		return nil, total
	}
	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}
	return filtered[startIdx:endIdx], total
}

// Close 关闭日志文件
func (al *AuditLogger) Close() error {
	al.fileMu.Lock()
	defer al.fileMu.Unlock()

	var err error
	if al.writer != nil {
		err = al.writer.Flush()
		al.writer = nil
	}
	if al.file != nil {
		if cerr := al.file.Close(); err == nil {
			err = cerr
		}
		al.file = nil
	}
	return err
}

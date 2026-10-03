// Package policy implements FrostLeaves' three-level authorization model.
//
// Every request resolves to an effective policy:
//
//	global default  <  account override  <  device override
//
// All decisions are made server-side; clients only render the outcome.
package policy

// Op is an operation gated by policy.
type Op string

const (
	OpUpload   Op = "upload"
	OpDownload Op = "download"
	OpDelete   Op = "delete"
	OpRename   Op = "rename"
	OpMkdir    Op = "mkdir"
	OpList     Op = "list"
	OpShare    Op = "share"
)

// Policy is a fully resolved, concrete policy (no inheritance).
type Policy struct {
	// 1) per account/device total storage quota, 0 = unlimited
	StorageQuotaBytes int64 `json:"storage_quota_bytes"`
	// 2) single file upload limit, 0 = unlimited
	MaxFileSizeBytes int64 `json:"max_file_size_bytes"`
	// 3) bandwidth limits in KB/s, 0 = unlimited
	UploadSpeedKBPS   int64 `json:"upload_speed_kbps"`
	DownloadSpeedKBPS int64 `json:"download_speed_kbps"`
	// 4) file type allow/deny lists (lower-case extensions incl. dot)
	AllowedExts []string `json:"allowed_exts,omitempty"`
	DeniedExts  []string `json:"denied_exts,omitempty"`
	// 4) content (magic number) verification switch
	MagicCheck bool `json:"magic_check"`
	// security override: allow executables for developer accounts/devices
	AllowExecutable bool `json:"allow_executable"`
	// 5) max number of stored files, 0 = unlimited
	MaxFileCount int64 `json:"max_file_count"`
	// 6) basic operation switches
	AllowUpload   bool `json:"allow_upload"`
	AllowDownload bool `json:"allow_download"`
	AllowDelete   bool `json:"allow_delete"`
	AllowRename   bool `json:"allow_rename"`
	AllowMkdir    bool `json:"allow_mkdir"`
	// 7) share links
	AllowShare        bool  `json:"allow_share"`
	ShareMaxDownloads int64 `json:"share_max_downloads"`
	ShareExpiryHours  int   `json:"share_expiry_hours"`
	// 8) device sessions
	MaxDevicesPerAccount int `json:"max_devices_per_account"`
	// 9) audit log retention (days)
	AuditRetentionDays int `json:"audit_retention_days"`
	// 10) whether recycle-bin usage counts towards the storage quota
	RecycleBinCountsQuota bool `json:"recycle_bin_counts_quota"`
	// recycle-bin retention in days (模块 2), 0 = keep forever
	RecycleBinRetentionDays int `json:"recycle_bin_retention_days"`
	// 模块 7) whether an offline device's private data is retained
	RetainOfflineData bool `json:"retain_offline_data"`
	// 模块 7) how long offline private data is kept (days), 0 = unlimited
	OfflineRetentionDays int `json:"offline_retention_days"`
}

// DefaultPolicy is the global default: safe by default, unlimited by default.
func DefaultPolicy() Policy {
	return Policy{
		MagicCheck:              true,
		AllowUpload:             true,
		AllowDownload:           true,
		AllowDelete:             true,
		AllowRename:             true,
		AllowMkdir:              true,
		AllowShare:              true,
		ShareExpiryHours:        24,
		AuditRetentionDays:      30,
		RecycleBinCountsQuota:   true,
		RecycleBinRetentionDays: 30,
		RetainOfflineData:       true,
		OfflineRetentionDays:    30,
	}
}

// Override is a partial policy: nil fields inherit from the level below.
type Override struct {
	StorageQuotaBytes       *int64    `json:"storage_quota_bytes,omitempty"`
	MaxFileSizeBytes        *int64    `json:"max_file_size_bytes,omitempty"`
	UploadSpeedKBPS         *int64    `json:"upload_speed_kbps,omitempty"`
	DownloadSpeedKBPS       *int64    `json:"download_speed_kbps,omitempty"`
	AllowedExts             *[]string `json:"allowed_exts,omitempty"`
	DeniedExts              *[]string `json:"denied_exts,omitempty"`
	MagicCheck              *bool     `json:"magic_check,omitempty"`
	AllowExecutable         *bool     `json:"allow_executable,omitempty"`
	MaxFileCount            *int64    `json:"max_file_count,omitempty"`
	AllowUpload             *bool     `json:"allow_upload,omitempty"`
	AllowDownload           *bool     `json:"allow_download,omitempty"`
	AllowDelete             *bool     `json:"allow_delete,omitempty"`
	AllowRename             *bool     `json:"allow_rename,omitempty"`
	AllowMkdir              *bool     `json:"allow_mkdir,omitempty"`
	AllowShare              *bool     `json:"allow_share,omitempty"`
	ShareMaxDownloads       *int64    `json:"share_max_downloads,omitempty"`
	ShareExpiryHours        *int      `json:"share_expiry_hours,omitempty"`
	MaxDevicesPerAccount    *int      `json:"max_devices_per_account,omitempty"`
	AuditRetentionDays      *int      `json:"audit_retention_days,omitempty"`
	RecycleBinCountsQuota   *bool     `json:"recycle_bin_counts_quota,omitempty"`
	RecycleBinRetentionDays *int      `json:"recycle_bin_retention_days,omitempty"`
	RetainOfflineData       *bool     `json:"retain_offline_data,omitempty"`
	OfflineRetentionDays    *int      `json:"offline_retention_days,omitempty"`
}

// apply overlays every non-nil field of o onto p.
func (o *Override) apply(p *Policy) {
	if o == nil || p == nil {
		return
	}
	if o.StorageQuotaBytes != nil {
		p.StorageQuotaBytes = *o.StorageQuotaBytes
	}
	if o.MaxFileSizeBytes != nil {
		p.MaxFileSizeBytes = *o.MaxFileSizeBytes
	}
	if o.UploadSpeedKBPS != nil {
		p.UploadSpeedKBPS = *o.UploadSpeedKBPS
	}
	if o.DownloadSpeedKBPS != nil {
		p.DownloadSpeedKBPS = *o.DownloadSpeedKBPS
	}
	if o.AllowedExts != nil {
		p.AllowedExts = append([]string(nil), (*o.AllowedExts)...)
	}
	if o.DeniedExts != nil {
		p.DeniedExts = append([]string(nil), (*o.DeniedExts)...)
	}
	if o.MagicCheck != nil {
		p.MagicCheck = *o.MagicCheck
	}
	if o.AllowExecutable != nil {
		p.AllowExecutable = *o.AllowExecutable
	}
	if o.MaxFileCount != nil {
		p.MaxFileCount = *o.MaxFileCount
	}
	if o.AllowUpload != nil {
		p.AllowUpload = *o.AllowUpload
	}
	if o.AllowDownload != nil {
		p.AllowDownload = *o.AllowDownload
	}
	if o.AllowDelete != nil {
		p.AllowDelete = *o.AllowDelete
	}
	if o.AllowRename != nil {
		p.AllowRename = *o.AllowRename
	}
	if o.AllowMkdir != nil {
		p.AllowMkdir = *o.AllowMkdir
	}
	if o.AllowShare != nil {
		p.AllowShare = *o.AllowShare
	}
	if o.ShareMaxDownloads != nil {
		p.ShareMaxDownloads = *o.ShareMaxDownloads
	}
	if o.ShareExpiryHours != nil {
		p.ShareExpiryHours = *o.ShareExpiryHours
	}
	if o.MaxDevicesPerAccount != nil {
		p.MaxDevicesPerAccount = *o.MaxDevicesPerAccount
	}
	if o.AuditRetentionDays != nil {
		p.AuditRetentionDays = *o.AuditRetentionDays
	}
	if o.RecycleBinCountsQuota != nil {
		p.RecycleBinCountsQuota = *o.RecycleBinCountsQuota
	}
	if o.RecycleBinRetentionDays != nil {
		p.RecycleBinRetentionDays = *o.RecycleBinRetentionDays
	}
	if o.RetainOfflineData != nil {
		p.RetainOfflineData = *o.RetainOfflineData
	}
	if o.OfflineRetentionDays != nil {
		p.OfflineRetentionDays = *o.OfflineRetentionDays
	}
}

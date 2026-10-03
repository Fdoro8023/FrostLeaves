package policy

import (
	"path"
	"strings"

	"frostleaves/pkg/security"
)

// Denied describes a server-side policy rejection.
//
// Code is a stable machine-readable identifier so clients can render a
// specific, user-readable message instead of a generic "permission denied".
type Denied struct {
	Op     Op
	Reason string
	Code   string
}

// Rejection codes returned to clients (item 2: distinguish limit types).
const (
	CodePolicyDenied   = "policy_denied"    // any other permission problem
	CodeFileSizeLimit  = "limit_file_size"  // single file over the limit
	CodeStorageQuota   = "limit_storage"    // account total storage full
	CodeFileCountLimit = "limit_file_count" // account file count reached
)

func (d *Denied) Error() string {
	return "policy denied [" + string(d.Op) + "]: " + d.Reason
}

// ErrorCode returns the denied code, defaulting to the generic one.
func (d *Denied) ErrorCode() string {
	if d == nil || d.Code == "" {
		return CodePolicyDenied
	}
	return d.Code
}

// opAllowed reports whether the resolved policy enables a plain operation.
func (p *Policy) opAllowed(op Op) bool {
	switch op {
	case OpUpload:
		return p.AllowUpload
	case OpDownload:
		return p.AllowDownload
	case OpDelete:
		return p.AllowDelete
	case OpRename:
		return p.AllowRename
	case OpMkdir:
		return p.AllowMkdir
	case OpShare:
		return p.AllowShare
	case OpList:
		return true
	default:
		return true
	}
}

// AuthorizeOp gates a plain operation (no payload) for a device.
func (s *Store) AuthorizeOp(deviceID string, op Op) error {
	p := s.Resolve(deviceID)
	if !p.opAllowed(op) {
		return &Denied{Op: op, Reason: string(op) + " disabled by policy"}
	}
	return nil
}

// UploadRequest carries the data needed for a full upload decision.
type UploadRequest struct {
	Name      string
	Size      int64
	Head      []byte // first bytes of the file, for magic-number checking
	UsedBytes int64  // bytes currently stored (incl. recycle bin when configured)
	FileCount int64  // files currently stored
}

// AuthorizeUpload applies the resolved upload policy. It rejects disabled
// uploads, executable/script uploads (extension + optional magic-number scan),
// allow/deny lists, and size/quota/count limits.
func (s *Store) AuthorizeUpload(deviceID string, req UploadRequest) error {
	p := s.Resolve(deviceID)
	if !p.AllowUpload {
		return &Denied{Op: OpUpload, Reason: "upload disabled by policy"}
	}

	name := strings.ReplaceAll(req.Name, "\\", "/")
	ext := strings.ToLower(path.Ext(name))

	// custom allow/deny lists
	if len(p.AllowedExts) > 0 && !containsExt(p.AllowedExts, ext) {
		return &Denied{Op: OpUpload, Reason: "extension not in allow list: " + ext}
	}
	if containsExt(p.DeniedExts, ext) {
		return &Denied{Op: OpUpload, Reason: "extension in deny list: " + ext}
	}

	// default executable protection (disabled per account/device for developers)
	if !p.AllowExecutable {
		if seg, bad := security.DangerousExt(req.Name); bad {
			return &Denied{Op: OpUpload, Reason: "executable/script name segment: " + seg}
		}
		if p.MagicCheck {
			if bad, reason := security.SniffDangerous(req.Name, req.Head); bad {
				return &Denied{Op: OpUpload, Reason: reason}
			}
		}
	}

	// size / quota / count limits (item 2: each gets its own code)
	if p.MaxFileSizeBytes > 0 && req.Size > p.MaxFileSizeBytes {
		return &Denied{Op: OpUpload, Reason: "file exceeds max size", Code: CodeFileSizeLimit}
	}
	if p.StorageQuotaBytes > 0 && req.UsedBytes+req.Size > p.StorageQuotaBytes {
		return &Denied{Op: OpUpload, Reason: "storage quota exceeded", Code: CodeStorageQuota}
	}
	if p.MaxFileCount > 0 && req.FileCount >= p.MaxFileCount {
		return &Denied{Op: OpUpload, Reason: "file count limit reached", Code: CodeFileCountLimit}
	}
	return nil
}

func containsExt(list []string, ext string) bool {
	for _, e := range list {
		if strings.EqualFold(strings.TrimSpace(e), ext) {
			return true
		}
	}
	return false
}

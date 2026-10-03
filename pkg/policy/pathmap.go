package policy

import "strings"

// OpForPath maps an API route to the operation it performs.
func OpForPath(p string) (Op, bool) {
	switch {
	case strings.HasPrefix(p, "/api/v1/files/upload"):
		return OpUpload, true
	case strings.HasPrefix(p, "/api/v1/files/download"):
		return OpDownload, true
	case strings.HasPrefix(p, "/api/v1/files/delete"):
		return OpDelete, true
	case strings.HasPrefix(p, "/api/v1/files/rename"):
		return OpRename, true
	case strings.HasPrefix(p, "/api/v1/files/mkdir"):
		return OpMkdir, true
	case strings.HasPrefix(p, "/api/v1/files/list"):
		return OpList, true
	case strings.HasPrefix(p, "/api/v1/recycle/restore"):
		return OpUpload, true
	case strings.HasPrefix(p, "/api/v1/recycle/delete"), strings.HasPrefix(p, "/api/v1/recycle/clear"):
		return OpDelete, true
	case strings.HasPrefix(p, "/api/v1/recycle/list"):
		return OpList, true
	}
	return "", false
}

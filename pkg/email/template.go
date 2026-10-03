package email

import (
	"fmt"
	"strconv"
	"strings"
)

// Render substitutes the supported placeholders in the subject and body.
// Supported: {code} {ttl} {server} {email}.
func Render(subjectTemplate, bodyTemplate, code string, ttlMinutes int, server, to string) (string, string) {
	subject := DefaultConfig().SubjectTemplate
	if strings.TrimSpace(subjectTemplate) != "" {
		subject = subjectTemplate
	}
	body := DefaultConfig().BodyTemplate
	if strings.TrimSpace(bodyTemplate) != "" {
		body = bodyTemplate
	}
	replace := func(s string) string {
		s = strings.ReplaceAll(s, "{code}", code)
		s = strings.ReplaceAll(s, "{ttl}", strconv.Itoa(ttlMinutes))
		s = strings.ReplaceAll(s, "{server}", server)
		s = strings.ReplaceAll(s, "{email}", to)
		return s
	}
	return replace(subject), replace(body)
}

// FormatFrom renders an RFC 5322 From header value.
func FormatFrom(name, addr string) string {
	if name == "" {
		return addr
	}
	return fmt.Sprintf("%s <%s>", name, addr)
}

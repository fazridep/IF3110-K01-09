package obs

import (
	"log/slog"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "sid", "cookie",
	"authorization", "credential", "credentials", "api_key", "apikey", "dsn",
}

var (
	dsnPassword = regexp.MustCompile(`(://[^:/@\s]+:)[^@\s]+@`)
	kvPassword  = regexp.MustCompile(`(?i)(\bpassword=)('[^']*'|\S+)`)
	sqlPassword = regexp.MustCompile(`(?i)(password\s+)'(?:[^']|'')*'`)
)

func isSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeys {
		if k == s || strings.HasPrefix(k, s+"_") || strings.HasSuffix(k, "_"+s) {
			return true
		}
	}
	return false
}

// RedactString nyamarin password di DSN, connection string, dan SQL.
func RedactString(s string) string {
	if strings.Contains(s, "://") {
		s = dsnPassword.ReplaceAllString(s, "${1}"+Redacted+"@")
	}
	if strings.Contains(strings.ToLower(s), "password") {
		s = kvPassword.ReplaceAllString(s, "${1}"+Redacted)
		s = sqlPassword.ReplaceAllString(s, "${1}'"+Redacted+"'")
	}
	return s
}

func redactAttr(_ []string, a slog.Attr) slog.Attr {
	if isSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindString {
		if v := a.Value.String(); strings.Contains(v, "://") || strings.Contains(strings.ToLower(v), "password") {
			return slog.String(a.Key, RedactString(v))
		}
	}
	return a
}
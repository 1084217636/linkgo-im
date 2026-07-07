package ai

import "regexp"

var (
	bearerPattern = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._\-]+`)
	secretPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret)\s*[:=]\s*[^,\s;]+`)
)

func RedactSensitive(value string) string {
	if value == "" {
		return ""
	}
	value = bearerPattern.ReplaceAllString(value, "Bearer [REDACTED]")
	return secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := regexp.MustCompile(`[:=]`).Split(match, 2)
		if len(parts) == 0 {
			return "[REDACTED]"
		}
		return parts[0] + "=[REDACTED]"
	})
}

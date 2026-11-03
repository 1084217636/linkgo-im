package logic

import (
	"sort"
	"strings"
)

func buildSessionID(from, to, toType string) string {
	if toType == "group" {
		return "group:" + normalizeGroupID(to)
	}
	ids := []string{from, to}
	sort.Strings(ids)
	return "c2c:" + strings.Join(ids, ":")
}

func normalizeGroupID(target string) string {
	return strings.TrimPrefix(target, "group:")
}

func targetType(target string) string {
	if strings.HasPrefix(strings.ToUpper(target), "G") || strings.HasPrefix(target, "group:") || strings.HasPrefix(target, "c-") {
		return "group"
	}
	return "user"
}

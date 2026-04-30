package worker

import (
	"regexp"
	"strings"
)

var codexThreadIDPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func canDeriveResumeSessionKey(provider string) bool {
	provider = strings.ToLower(strings.TrimSpace(provider))
	return strings.Contains(provider, "codex") || strings.Contains(provider, "gemini")
}

func derivedResumeSessionKey(provider, providerSessionID string) string {
	providerSessionID = strings.TrimSpace(providerSessionID)
	if providerSessionID == "" {
		return ""
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(provider, "codex"):
		matches := codexThreadIDPattern.FindAllString(providerSessionID, -1)
		if len(matches) == 0 {
			return ""
		}
		return matches[len(matches)-1]
	case strings.Contains(provider, "gemini"):
		return providerSessionID
	default:
		return ""
	}
}

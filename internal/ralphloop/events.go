package ralphloop

import (
	"regexp"
	"strings"
)

var prURLPattern = regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/pull/\d+`)

func notificationToAgentMessage(notification jsonRPCNotification) string {
	if strings.TrimSpace(notification.Method) != "item/completed" {
		return ""
	}
	item, ok := asRecord(notification.Params["item"])
	if !ok {
		return ""
	}
	if valueString(item["type"]) != "agentMessage" {
		return ""
	}
	return normalizeAgentMessage(item)
}

func extractPRURL(agentOutput string) string {
	return prURLPattern.FindString(agentOutput)
}

func normalizeAgentMessage(item map[string]any) string {
	direct := valueString(item["text"])
	content := ""
	if rawList, ok := item["content"].([]any); ok {
		parts := make([]string, 0, len(rawList))
		for _, rawPart := range rawList {
			part, ok := asRecord(rawPart)
			if !ok {
				continue
			}
			if text := valueString(part["text"]); text != "" {
				parts = append(parts, text)
			}
		}
		content = strings.Join(parts, "\n")
	}
	combined := strings.TrimSpace(direct)
	if combined == "" {
		combined = strings.TrimSpace(content)
	}
	return stripCompletionSignal(combined)
}

func asRecord(value any) (map[string]any, bool) {
	record, ok := value.(map[string]any)
	return record, ok
}

func valueString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

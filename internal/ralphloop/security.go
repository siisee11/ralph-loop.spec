package ralphloop

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	encodedTraversalPattern = regexp.MustCompile(`(?i)%2e|%2f|%5c|%3f|%23|%25`)
	injectionPattern        = regexp.MustCompile(`(?i)(ignore\s+previous\s+instructions|system\s+prompt|developer\s+message|<\s*/?\s*(system|assistant|user)\s*>|you\s+are\s+chatgpt|tool\s+call)`)
)

type sanitizationResult struct {
	Text      string
	Sanitized bool
}

func validateSelector(name string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if containsControlChars(trimmed) {
		return fmt.Errorf("invalid %s: control characters are not allowed", name)
	}
	if strings.Contains(trimmed, "../") || strings.Contains(trimmed, `..\`) {
		return fmt.Errorf("invalid %s: path traversal is not allowed", name)
	}
	if strings.ContainsAny(trimmed, "?#") {
		return fmt.Errorf("invalid %s: query and fragment characters are not allowed", name)
	}
	if encodedTraversalPattern.MatchString(trimmed) {
		return fmt.Errorf("invalid %s: encoded path or query delimiters are not allowed", name)
	}
	return nil
}

func validateOutputFilePath(cwd string, rawPath string) (string, error) {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return "", nil
	}
	if containsControlChars(trimmed) {
		return "", fmt.Errorf("invalid --output-file: control characters are not allowed")
	}
	if strings.Contains(trimmed, "\x00") {
		return "", fmt.Errorf("invalid --output-file: null bytes are not allowed")
	}
	if strings.Contains(trimmed, "../") || strings.Contains(trimmed, `..\`) || encodedTraversalPattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid --output-file: path traversal is not allowed")
	}

	base, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	target := trimmed
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", err
	}
	baseResolved, err := evalSymlinkRoot(base)
	if err != nil {
		return "", err
	}
	targetParent := filepath.Dir(target)
	targetParentResolved, err := evalSymlinkRoot(targetParent)
	if err != nil {
		return "", err
	}
	if targetParentResolved != baseResolved && !strings.HasPrefix(targetParentResolved, baseResolved+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid --output-file: path must stay under %s", base)
	}
	return target, nil
}

func evalSymlinkRoot(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Abs(resolved)
	}
	return filepath.Abs(path)
}

func sanitizeUntrustedText(text string) sanitizationResult {
	original := text
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	cleaned = injectionPattern.ReplaceAllString(cleaned, "[sanitized]")
	cleaned = strings.TrimSpace(cleaned)
	return sanitizationResult{
		Text:      cleaned,
		Sanitized: cleaned != strings.TrimSpace(original),
	}
}

func containsControlChars(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func encodeTransportSegment(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}

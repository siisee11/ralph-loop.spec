package ralphloop

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

type pageMetadata struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	TotalItems int `json:"total_items"`
	TotalPages int `json:"total_pages"`
}

type pagedItemsResult struct {
	Command string       `json:"command"`
	Status  string       `json:"status"`
	Page    pageMetadata `json:"page"`
	Items   any          `json:"items"`
}

type pageEnvelope struct {
	Command string       `json:"command"`
	Status  string       `json:"status"`
	Page    pageMetadata `json:"page"`
	Items   any          `json:"items"`
}

func parseFieldMask(value string) FieldMask {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	mask := make(FieldMask, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		mask = append(mask, part)
	}
	return mask
}

func normalizePaging(page int, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	return page, pageSize
}

func paginateItems[T any](command string, status string, items []T, page int, pageSize int, pageAll bool) []pageEnvelope {
	page, pageSize = normalizePaging(page, pageSize)
	totalItems := len(items)
	totalPages := 1
	if totalItems > 0 {
		totalPages = int(math.Ceil(float64(totalItems) / float64(pageSize)))
	}

	if !pageAll {
		start, end := pageBounds(totalItems, page, pageSize)
		return []pageEnvelope{{
			Command: command,
			Status:  status,
			Page:    pageMetadata{Page: page, PageSize: pageSize, TotalItems: totalItems, TotalPages: totalPages},
			Items:   items[start:end],
		}}
	}

	envelopes := make([]pageEnvelope, 0, totalPages)
	for current := 1; current <= totalPages; current++ {
		start, end := pageBounds(totalItems, current, pageSize)
		envelopes = append(envelopes, pageEnvelope{
			Command: command,
			Status:  status,
			Page:    pageMetadata{Page: current, PageSize: pageSize, TotalItems: totalItems, TotalPages: totalPages},
			Items:   items[start:end],
		})
	}
	return envelopes
}

func pageBounds(totalItems int, page int, pageSize int) (int, int) {
	if totalItems == 0 {
		return 0, 0
	}
	start := (page - 1) * pageSize
	if start > totalItems {
		start = totalItems
	}
	end := start + pageSize
	if end > totalItems {
		end = totalItems
	}
	return start, end
}

func applyFieldMask(value any, fields FieldMask) (any, error) {
	if len(fields) == 0 {
		return value, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, err
	}
	projected, kept := projectFieldMask(decoded, fieldPrefixes(fields))
	if !kept {
		return map[string]any{}, nil
	}
	return projected, nil
}

func fieldPrefixes(fields FieldMask) []string {
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		normalized = append(normalized, field)
	}
	return normalized
}

func projectFieldMask(value any, fields []string) (any, bool) {
	if len(fields) == 0 {
		return value, true
	}
	switch converted := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range converted {
			childFields, includeWhole := childFieldMask(fields, key)
			if !includeWhole && len(childFields) == 0 {
				continue
			}
			if includeWhole {
				result[key] = child
				continue
			}
			projected, kept := projectFieldMask(child, childFields)
			if kept {
				result[key] = projected
			}
		}
		return result, len(result) > 0
	case []any:
		result := make([]any, 0, len(converted))
		for _, item := range converted {
			projected, kept := projectFieldMask(item, fields)
			if kept {
				result = append(result, projected)
			}
		}
		return result, true
	default:
		return converted, true
	}
}

func childFieldMask(fields []string, key string) ([]string, bool) {
	child := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == key {
			return nil, true
		}
		prefix := key + "."
		if strings.HasPrefix(field, prefix) {
			child = append(child, strings.TrimPrefix(field, prefix))
		}
	}
	return child, false
}

func renderPagedResult(stdoutWriter jsonWriter, format OutputFormat, command string, status string, items any, fields FieldMask, page int, pageSize int, pageAll bool) error {
	switch slice := items.(type) {
	case []listSessionRecord:
		return renderTypedPages(stdoutWriter, format, command, status, slice, fields, page, pageSize, pageAll)
	case []tailLineRecord:
		return renderTypedPages(stdoutWriter, format, command, status, slice, fields, page, pageSize, pageAll)
	case []map[string]any:
		return renderTypedPages(stdoutWriter, format, command, status, slice, fields, page, pageSize, pageAll)
	default:
		return fmt.Errorf("unsupported paged result type %T", items)
	}
}

type jsonWriter interface {
	WriteJSON(any) error
	WriteJSONLine(any) error
}

func renderTypedPages[T any](writer jsonWriter, format OutputFormat, command string, status string, items []T, fields FieldMask, page int, pageSize int, pageAll bool) error {
	pages := paginateItems(command, status, items, page, pageSize, pageAll)
	switch format {
	case OutputNDJSON:
		for _, entry := range pages {
			projected, err := applyFieldMask(entry, fields)
			if err != nil {
				return err
			}
			if err := writer.WriteJSONLine(projected); err != nil {
				return err
			}
		}
		return nil
	default:
		if pageAll {
			projected, err := applyFieldMask(pages, fields)
			if err != nil {
				return err
			}
			return writer.WriteJSON(projected)
		}
		projected, err := applyFieldMask(pages[0], fields)
		if err != nil {
			return err
		}
		return writer.WriteJSON(projected)
	}
}

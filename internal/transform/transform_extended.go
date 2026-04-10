// Copyright 2026 Soap Bucket LLC. Licensed under the Apache License, Version 2.0.
package transform

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// applySort sorts an array of objects by a field.
func applySort(field, order string, data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	if order == "" {
		order = "asc"
	}

	sorted := make([]any, len(arr))
	copy(sorted, arr)

	sort.SliceStable(sorted, func(i, j int) bool {
		vi := getFieldValue(sorted[i], field)
		vj := getFieldValue(sorted[j], field)
		cmp := compareValues(vi, vj)
		if order == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	return sorted, nil
}

// applyFilter filters array items using a simple comparison expression.
// Supports: .field > N, .field < N, .field >= N, .field <= N, .field == V, .field != V
func applyFilter(filter string, data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}

	field, op, value, err := parseFilterExpr(filter)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}

	var result []any
	for _, item := range arr {
		fv := getFieldValue(item, field)
		if matchesFilter(fv, op, value) {
			result = append(result, item)
		}
	}
	if result == nil {
		result = []any{}
	}
	return result, nil
}

// applyUnique deduplicates an array of objects by a field (first occurrence kept).
func applyUnique(field string, data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}

	seen := make(map[string]bool)
	var result []any
	for _, item := range arr {
		key := fmt.Sprintf("%v", getFieldValue(item, field))
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	if result == nil {
		result = []any{}
	}
	return result, nil
}

// applyGroup groups an array of objects by field value.
func applyGroup(field string, data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}

	groups := make(map[string]any)
	order := []string{} // preserve insertion order
	for _, item := range arr {
		key := fmt.Sprintf("%v", getFieldValue(item, field))
		if _, exists := groups[key]; !exists {
			groups[key] = []any{}
			order = append(order, key)
		}
		groups[key] = append(groups[key].([]any), item)
	}

	// Return as map
	result := make(map[string]any, len(groups))
	for _, k := range order {
		result[k] = groups[k]
	}
	return result, nil
}

// applyCount returns the count of items in an array.
func applyCount(data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	return float64(len(arr)), nil
}

// applyJoin joins an array of strings into one string.
func applyJoin(separator string, data any) (any, error) {
	if separator == "" {
		separator = ", "
	}
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	parts := make([]string, len(arr))
	for i, item := range arr {
		parts[i] = fmt.Sprintf("%v", item)
	}
	return strings.Join(parts, separator), nil
}

// applySplit splits a string into an array by separator.
func applySplit(separator string, data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		return data, nil
	}
	parts := strings.Split(s, separator)
	result := make([]any, len(parts))
	for i, p := range parts {
		result[i] = p
	}
	return result, nil
}

// applyFlatten flattens nested arrays one level deep.
func applyFlatten(data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	var result []any
	for _, item := range arr {
		if inner, ok := item.([]any); ok {
			result = append(result, inner...)
		} else {
			result = append(result, item)
		}
	}
	if result == nil {
		result = []any{}
	}
	return result, nil
}

// applyUnwrap unwraps single-item arrays.
func applyUnwrap(data any) (any, error) {
	arr, ok := data.([]any)
	if !ok {
		return data, nil
	}
	if len(arr) == 1 {
		return arr[0], nil
	}
	return data, nil
}

// applyFormat applies simple {field} string interpolation. For arrays, produces one line per item.
func applyFormat(tmpl string, data any) (any, error) {
	switch v := data.(type) {
	case []any:
		var lines []string
		for _, item := range v {
			line := interpolateFields(tmpl, item)
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n"), nil
	case map[string]any:
		return interpolateFields(tmpl, v), nil
	default:
		return data, nil
	}
}

// applyPrompt appends agent guidance text to the output.
func applyPrompt(value string, data any) (any, error) {
	if value == "" {
		return data, nil
	}

	// Convert data to string representation if needed
	var dataStr string
	switch v := data.(type) {
	case string:
		dataStr = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return data, nil
		}
		dataStr = string(b)
	}

	return dataStr + "\n\n" + value, nil
}

// applyDateFormat reformats date strings in objects or arrays of objects.
func applyDateFormat(field, fromLayout, toLayout string, data any) (any, error) {
	switch v := data.(type) {
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			obj, ok := item.(map[string]any)
			if !ok {
				result[i] = item
				continue
			}
			result[i] = reformatDate(obj, field, fromLayout, toLayout)
		}
		return result, nil
	case map[string]any:
		return reformatDate(v, field, fromLayout, toLayout), nil
	default:
		return data, nil
	}
}

// applyXMLToJSON parses an XML string to a JSON-compatible structure.
func applyXMLToJSON(data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		return data, nil
	}
	decoder := xml.NewDecoder(strings.NewReader(s))
	result, err := xmlToMap(decoder)
	if err != nil {
		return nil, fmt.Errorf("xml_to_json: %w", err)
	}
	return result, nil
}

// applyCSVToJSON parses a CSV string to a JSON array.
func applyCSVToJSON(headers bool, data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		return data, nil
	}

	reader := csv.NewReader(strings.NewReader(s))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("csv_to_json: %w", err)
	}

	if len(records) == 0 {
		return []any{}, nil
	}

	if headers && len(records) > 1 {
		headerRow := records[0]
		var result []any
		for _, row := range records[1:] {
			obj := make(map[string]any, len(headerRow))
			for j, h := range headerRow {
				if j < len(row) {
					obj[h] = row[j]
				}
			}
			result = append(result, obj)
		}
		if result == nil {
			result = []any{}
		}
		return result, nil
	}

	// No headers: return array of arrays
	var result []any
	for _, row := range records {
		rowArr := make([]any, len(row))
		for j, cell := range row {
			rowArr[j] = cell
		}
		result = append(result, rowArr)
	}
	return result, nil
}

// applyBase64Decode decodes base64 content.
func applyBase64Decode(field string, data any) (any, error) {
	if field != "" {
		// Decode specific field in object or array of objects
		switch v := data.(type) {
		case map[string]any:
			return decodeFieldBase64(v, field)
		case []any:
			result := make([]any, len(v))
			for i, item := range v {
				obj, ok := item.(map[string]any)
				if !ok {
					result[i] = item
					continue
				}
				decoded, err := decodeFieldBase64(obj, field)
				if err != nil {
					return nil, err
				}
				result[i] = decoded
			}
			return result, nil
		default:
			return data, nil
		}
	}

	// Decode entire input string
	s, ok := data.(string)
	if !ok {
		return data, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Try URL-safe encoding
		decoded, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			// Try without padding
			decoded, err = base64.RawStdEncoding.DecodeString(s)
			if err != nil {
				return nil, fmt.Errorf("base64_decode: %w", err)
			}
		}
	}
	return string(decoded), nil
}

// applyMarkdownToText strips markdown formatting to produce plain text.
func applyMarkdownToText(data any) (any, error) {
	s, ok := data.(string)
	if !ok {
		return data, nil
	}
	return stripMarkdown(s), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getFieldValue(item any, field string) any {
	obj, ok := item.(map[string]any)
	if !ok {
		return nil
	}
	// Support dotted paths
	parts := strings.Split(field, ".")
	var current any = obj
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func compareValues(a, b any) int {
	// Try numeric comparison first
	na, aOk := toFloat64(a)
	nb, bOk := toFloat64(b)
	if aOk && bOk {
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
		return 0
	}

	// Fall back to string comparison
	sa := fmt.Sprintf("%v", a)
	sb := fmt.Sprintf("%v", b)
	if sa < sb {
		return -1
	}
	if sa > sb {
		return 1
	}
	return 0
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// parseFilterExpr parses expressions like ".stars > 100", ".name == \"foo\""
func parseFilterExpr(expr string) (field, op, value string, err error) {
	expr = strings.TrimSpace(expr)

	// Match: .field op value
	re := regexp.MustCompile(`^\.(\S+)\s+(>=|<=|!=|==|>|<)\s+(.+)$`)
	matches := re.FindStringSubmatch(expr)
	if matches == nil {
		return "", "", "", fmt.Errorf("unsupported filter expression: %q (expected .field op value)", expr)
	}

	field = matches[1]
	op = matches[2]
	value = strings.TrimSpace(matches[3])
	// Strip quotes from string values
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return field, op, value, nil
}

func matchesFilter(fieldVal any, op, target string) bool {
	// Try numeric comparison
	fv, fOk := toFloat64(fieldVal)
	tv, tOk := toFloat64(target)

	if fOk && tOk {
		switch op {
		case ">":
			return fv > tv
		case "<":
			return fv < tv
		case ">=":
			return fv >= tv
		case "<=":
			return fv <= tv
		case "==":
			return fv == tv
		case "!=":
			return fv != tv
		}
	}

	// String comparison
	fs := fmt.Sprintf("%v", fieldVal)
	switch op {
	case "==":
		return fs == target
	case "!=":
		return fs != target
	case ">":
		return fs > target
	case "<":
		return fs < target
	case ">=":
		return fs >= target
	case "<=":
		return fs <= target
	}
	return false
}

func interpolateFields(tmpl string, data any) string {
	obj, ok := data.(map[string]any)
	if !ok {
		return tmpl
	}
	result := tmpl
	for key, val := range obj {
		placeholder := "{" + key + "}"
		result = strings.ReplaceAll(result, placeholder, formatValue(val))
	}
	return result
}

// formatValue converts a value to a human-readable string.
// Numbers are formatted without scientific notation. Large numbers get comma separators.
func formatValue(val any) string {
	switch v := val.(type) {
	case float64:
		if v == float64(int64(v)) {
			// Whole number - format with commas
			return formatInt(int64(v))
		}
		// Decimal - use fixed notation with up to 2 decimal places
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return formatInt(int64(v))
	case int64:
		return formatInt(v)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatInt formats an integer with comma separators (e.g., 1000000 -> "1,000,000").
// Numbers under 10,000 are not comma-formatted (avoids "1,965" for years).
func formatInt(n int64) string {
	if n < 0 {
		return "-" + formatInt(-n)
	}
	if n < 10000 {
		return strconv.FormatInt(n, 10)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func reformatDate(obj map[string]any, field, fromLayout, toLayout string) map[string]any {
	result := make(map[string]any, len(obj))
	for k, v := range obj {
		result[k] = v
	}

	val, ok := result[field]
	if !ok {
		return result
	}
	s, ok := val.(string)
	if !ok {
		return result
	}

	t, err := time.Parse(fromLayout, s)
	if err != nil {
		return result
	}
	result[field] = t.Format(toLayout)
	return result
}

// xmlToMap recursively parses XML into maps. Simple recursive approach.
// Called after a StartElement has been consumed; reads until the matching EndElement.
func xmlToMap(decoder *xml.Decoder) (any, error) {
	result := make(map[string]any)
	var textContent strings.Builder

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := token.(type) {
		case xml.StartElement:
			child, err := xmlToMap(decoder)
			if err != nil {
				return nil, err
			}

			name := t.Name.Local

			// Add attributes
			if obj, ok := child.(map[string]any); ok && len(t.Attr) > 0 {
				for _, attr := range t.Attr {
					obj["@"+attr.Name.Local] = attr.Value
				}
				child = obj
			} else if len(t.Attr) > 0 {
				// Wrap string value in map to attach attributes
				obj := make(map[string]any)
				for _, attr := range t.Attr {
					obj["@"+attr.Name.Local] = attr.Value
				}
				if s, ok := child.(string); ok && s != "" {
					obj["#text"] = s
				}
				child = obj
			}

			// Handle repeated elements by collecting into array
			if existing, exists := result[name]; exists {
				if arr, ok := existing.([]any); ok {
					result[name] = append(arr, child)
				} else {
					result[name] = []any{existing, child}
				}
			} else {
				result[name] = child
			}

		case xml.CharData:
			text := strings.TrimSpace(string(t))
			if text != "" {
				textContent.WriteString(text)
			}

		case xml.EndElement:
			// If no child elements were found, return text content
			if len(result) == 0 {
				return textContent.String(), nil
			}
			// If there is mixed content, add text
			if textContent.Len() > 0 {
				result["#text"] = textContent.String()
			}
			return result, nil
		}
	}
	if len(result) == 0 {
		return textContent.String(), nil
	}
	return result, nil
}

func decodeFieldBase64(obj map[string]any, field string) (map[string]any, error) {
	result := make(map[string]any, len(obj))
	for k, v := range obj {
		result[k] = v
	}

	val, ok := result[field]
	if !ok {
		return result, nil
	}
	s, ok := val.(string)
	if !ok {
		return result, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("base64_decode field %q: %w", field, err)
		}
	}
	result[field] = string(decoded)
	return result, nil
}

// stripMarkdown removes common markdown formatting.
func stripMarkdown(s string) string {
	// Remove headers
	re := regexp.MustCompile(`(?m)^#{1,6}\s+`)
	s = re.ReplaceAllString(s, "")

	// Remove bold/italic markers
	s = regexp.MustCompile(`\*\*\*(.+?)\*\*\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`\*\*(.+?)\*\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`\*(.+?)\*`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`___(.+?)___`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`__(.+?)__`).ReplaceAllString(s, "$1")
	s = regexp.MustCompile(`_(.+?)_`).ReplaceAllString(s, "$1")

	// Remove code blocks (before inline code to avoid partial matches)
	s = regexp.MustCompile("(?s)```[\\s\\S]*?```").ReplaceAllString(s, "")

	// Remove inline code
	s = regexp.MustCompile("`([^`]+)`").ReplaceAllString(s, "$1")

	// Remove links: [text](url) -> text
	s = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`).ReplaceAllString(s, "$1")

	// Remove images: ![alt](url) -> alt
	s = regexp.MustCompile(`!\[([^\]]*)\]\([^)]+\)`).ReplaceAllString(s, "$1")

	// Remove horizontal rules
	s = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`).ReplaceAllString(s, "")

	// Remove blockquote markers
	s = regexp.MustCompile(`(?m)^>\s?`).ReplaceAllString(s, "")

	// Clean up list markers but keep text
	s = regexp.MustCompile(`(?m)^[\s]*[-*+]\s+`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?m)^[\s]*\d+\.\s+`).ReplaceAllString(s, "")

	// Clean up extra whitespace
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

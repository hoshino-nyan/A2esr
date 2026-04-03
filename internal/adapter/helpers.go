package adapter

import (
	"fmt"
	"strings"
)

func flattenText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		var parts []string
		for _, raw := range arr {
			if s, ok := raw.(string); ok {
				parts = append(parts, s)
			} else if m, ok := raw.(map[string]interface{}); ok {
				if toString(m["type"]) == "text" {
					parts = append(parts, toString(m["text"]))
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", content)
}

func toSlice(v interface{}) []interface{} {
	if arr, ok := v.([]interface{}); ok {
		return arr
	}
	return nil
}

func toMap(v interface{}) J {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return J{}
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

func orDefault(v interface{}, def interface{}) interface{} {
	if v == nil || v == "" {
		return def
	}
	return v
}

func toBlocks(content interface{}) []J {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []J{{"type": "text", "text": s}}
	}
	if arr, ok := content.([]interface{}); ok {
		var blocks []J
		for _, raw := range arr {
			if m, ok := raw.(map[string]interface{}); ok {
				blocks = append(blocks, m)
			}
		}
		return blocks
	}
	if arr, ok := content.([]J); ok {
		return arr
	}
	return nil
}

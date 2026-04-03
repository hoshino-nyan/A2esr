package adapter

import (
	"encoding/json"

	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

func NormalizeRequest(payload J, upstreamModel string) J {
	if upstreamModel != "" {
		payload["model"] = upstreamModel
	}
	if msgs, ok := payload["messages"]; ok {
		payload["messages"] = normalizeMessages(convertAnthropicMessages(msgs))
	}
	if _, ok := payload["tools"]; !ok {
		return payload
	}
	tools := toSlice(payload["tools"])
	var normalized []interface{}
	for _, raw := range tools {
		normalized = append(normalized, normalizeToolDefinition(raw))
	}
	payload["tools"] = normalized
	normalizeToolChoice(payload)
	return payload
}

func normalizeMessages(msgs interface{}) interface{} {
	arr := toSlice(msgs)
	if arr == nil {
		return msgs
	}
	for _, raw := range arr {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		normalizeMessageToolCalls(msg)
	}
	return arr
}

func normalizeMessageToolCalls(message J) {
	convertLegacyFunctionCall(message, J{})
	toolCalls := toSlice(message["tool_calls"])
	if len(toolCalls) == 0 {
		return
	}
	for i, raw := range toolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn := toMap(tc["function"])
		if len(fn) == 0 {
			fn = J{}
			if name := toString(tc["name"]); name != "" {
				fn["name"] = name
			}
			if args, ok := tc["arguments"]; ok {
				fn["arguments"] = args
			}
			tc["function"] = fn
		}
		if fn["name"] == nil || toString(fn["name"]) == "" {
			if name := toString(tc["name"]); name != "" {
				fn["name"] = name
			}
		}
		if fn["arguments"] == nil {
			if args, ok := tc["arguments"]; ok {
				fn["arguments"] = args
			}
		}
		if tc["id"] == nil || tc["id"] == "" {
			tc["id"] = proxy.GenID("call_")
		}
		if _, ok := tc["index"]; !ok {
			tc["index"] = i
		}
		tc["type"] = "function"
		normalizeToolCallArguments(tc)
		delete(tc, "name")
		delete(tc, "arguments")
	}
}

func FixResponse(data J) J {
	choices := toSlice(data["choices"])
	for _, raw := range choices {
		choice, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fixResponseChoice(choice)
	}
	return data
}

func FixStreamChunk(data J) J {
	choices := toSlice(data["choices"])
	for _, raw := range choices {
		choice, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fixStreamChoice(choice)
	}
	return data
}

func fixResponseChoice(choice J) {
	message := toMap(choice["message"])
	if len(message) == 0 {
		return
	}
	promoteReasoningField(message)
	extractReasoningFromContent(message)
	convertLegacyFunctionCall(message, choice)
	fixToolCalls(message, choice)
}

func fixStreamChoice(choice J) {
	delta := toMap(choice["delta"])
	if len(delta) == 0 {
		return
	}
	promoteReasoningField(delta)
	convertLegacyDeltaFunctionCall(delta, choice)
	sanitizeToolCallDeltas(delta)
	ensureStreamToolCalls(delta)
	rewriteFunctionCallFinishReason(choice)
}

func promoteReasoningField(container J) {
	if rc, ok := container["reasoningContent"]; ok {
		if _, exists := container["reasoning_content"]; !exists {
			container["reasoning_content"] = rc
		}
		delete(container, "reasoningContent")
	}
	if rc, ok := container["reasoning"]; ok {
		if s, isStr := rc.(string); isStr && s != "" {
			if _, exists := container["reasoning_content"]; !exists {
				container["reasoning_content"] = s
			}
			delete(container, "reasoning")
		}
	}
}

func extractReasoningFromContent(message J) {
	content := toString(message["content"])
	if content == "" {
		return
	}
	if _, ok := message["reasoning_content"]; ok {
		return
	}
	// Simple <think> tag extraction
	start := indexOf(content, "<think>")
	if start < 0 {
		return
	}
	end := indexOf(content, "</think>")
	if end < 0 {
		return
	}
	reasoning := content[start+7 : end]
	cleaned := content[:start] + content[end+8:]
	if reasoning != "" {
		message["reasoning_content"] = reasoning
		message["content"] = cleaned
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func convertLegacyFunctionCall(message J, choice J) {
	fc, ok := message["function_call"]
	if !ok {
		return
	}
	if _, ok := message["tool_calls"]; ok {
		return
	}
	delete(message, "function_call")
	fcMap := toMap(fc)
	message["tool_calls"] = []J{{
		"id":   proxy.GenID("call_"),
		"type": "function",
		"function": J{
			"name":      fcMap["name"],
			"arguments": orDefault(fcMap["arguments"], "{}"),
		},
	}}
	rewriteFunctionCallFinishReason(choice)
}

func convertLegacyDeltaFunctionCall(delta J, choice J) {
	fc, ok := delta["function_call"]
	if !ok {
		return
	}
	if _, ok := delta["tool_calls"]; ok {
		return
	}
	delete(delta, "function_call")
	fcMap := toMap(fc)
	tc := J{"index": 0, "type": "function", "function": J{}}
	fn := tc["function"].(J)
	if name, ok := fcMap["name"]; ok {
		tc["id"] = proxy.GenID("call_")
		fn["name"] = name
	}
	if args, ok := fcMap["arguments"]; ok {
		fn["arguments"] = args
	}
	delta["tool_calls"] = []J{tc}
	rewriteFunctionCallFinishReason(choice)
}

func sanitizeToolCallDeltas(delta J) {
	for _, raw := range toSlice(delta["tool_calls"]) {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := tc["id"]; ok && toString(id) == "" {
			delete(tc, "id")
		}
		if t, ok := tc["type"]; ok && toString(t) == "" {
			delete(tc, "type")
		}
		if fn, ok := tc["function"].(map[string]interface{}); ok {
			if name, ok := fn["name"]; ok && toString(name) == "" {
				delete(fn, "name")
			}
		}
	}
}

func ensureStreamToolCalls(delta J) {
	for _, raw := range toSlice(delta["tool_calls"]) {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := tc["index"]; !ok {
			tc["index"] = 0
		}
		fn := toMap(tc["function"])
		if _, hasID := tc["id"]; hasID {
			if tc["id"] == nil || tc["id"] == "" {
				tc["id"] = proxy.GenID("call_")
			}
			tc["type"] = "function"
		} else if _, hasName := fn["name"]; hasName {
			if _, hasID := tc["id"]; !hasID || tc["id"] == "" {
				tc["id"] = proxy.GenID("call_")
			}
			tc["type"] = "function"
		}
	}
}

func fixToolCalls(message J, choice J) {
	toolCalls := toSlice(message["tool_calls"])
	if len(toolCalls) == 0 {
		return
	}
	for i, raw := range toolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if tc["id"] == nil || tc["id"] == "" {
			tc["id"] = proxy.GenID("call_")
		}
		if _, ok := tc["index"]; !ok {
			tc["index"] = i
		}
		tc["type"] = "function"
		normalizeToolCallArguments(tc)
	}
	if fr := toString(choice["finish_reason"]); fr != "tool_calls" && fr != "function_call" {
		choice["finish_reason"] = "tool_calls"
	}
}

func normalizeToolCallArguments(tc J) {
	fn := toMap(tc["function"])
	rawArgs := fn["arguments"]
	var args J
	if s, ok := rawArgs.(string); ok {
		if err := json.Unmarshal([]byte(s), &args); err != nil {
			args = J{}
		}
	} else if m, ok := rawArgs.(map[string]interface{}); ok {
		args = m
	} else {
		args = J{}
	}
	b, _ := json.Marshal(args)
	fn["arguments"] = string(b)
}

func rewriteFunctionCallFinishReason(choice J) {
	if toString(choice["finish_reason"]) == "function_call" {
		choice["finish_reason"] = "tool_calls"
	}
}

func normalizeToolDefinition(tool interface{}) interface{} {
	t, ok := tool.(map[string]interface{})
	if !ok {
		return tool
	}
	if toString(t["type"]) == "function" && t["function"] != nil {
		return t
	}
	if _, ok := t["name"]; !ok {
		return t
	}
	return J{
		"type": "function",
		"function": J{
			"name":        t["name"],
			"description": t["description"],
			"parameters":  orDefault(orDefault(t["input_schema"], t["parameters"]), J{"type": "object", "properties": J{}}),
		},
	}
}

func normalizeToolChoice(payload J) {
	tc, ok := payload["tool_choice"].(map[string]interface{})
	if !ok {
		return
	}
	switch toString(tc["type"]) {
	case "auto":
		payload["tool_choice"] = "auto"
	case "any":
		payload["tool_choice"] = "required"
	}
}

// ThinkTagStreamExtractor handles streaming extraction of <think>...</think> tags
// from content into reasoning_content for OpenAI-compatible providers (e.g. DeepSeek).
type ThinkTagStreamExtractor struct {
	inThink   bool
	tagBuffer string
}

func (e *ThinkTagStreamExtractor) ProcessChunk(chunk J) []J {
	choices := toSlice(chunk["choices"])
	if len(choices) == 0 {
		return []J{chunk}
	}
	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return []J{chunk}
	}
	delta := toMap(choice["delta"])
	if _, hasRC := delta["reasoning_content"]; hasRC {
		return []J{chunk}
	}
	content := toString(delta["content"])
	if content == "" {
		return []J{chunk}
	}

	normalText, thinkingText := e.extractThinkTags(content)

	var results []J
	if thinkingText != "" {
		rc := cloneChunk(chunk)
		setDeltaField(rc, "reasoning_content", thinkingText)
		removeDeltaField(rc, "content")
		results = append(results, rc)
	}
	if normalText != "" {
		nc := cloneChunk(chunk)
		setDeltaField(nc, "content", normalText)
		results = append(results, nc)
	}
	if len(results) == 0 {
		removeDeltaField(chunk, "content")
		return []J{chunk}
	}
	return results
}

func (e *ThinkTagStreamExtractor) extractThinkTags(content string) (normalText, thinkingText string) {
	full := e.tagBuffer + content
	e.tagBuffer = ""

	for len(full) > 0 {
		if e.inThink {
			idx := indexOf(full, "</think>")
			if idx >= 0 {
				thinkingText += full[:idx]
				full = full[idx+8:]
				e.inThink = false
			} else {
				tail := partialSuffix(full, "</think>")
				if tail > 0 {
					e.tagBuffer = full[len(full)-tail:]
					thinkingText += full[:len(full)-tail]
				} else {
					thinkingText += full
				}
				return
			}
		} else {
			idx := indexOf(full, "<think>")
			if idx >= 0 {
				normalText += full[:idx]
				full = full[idx+7:]
				e.inThink = true
			} else {
				tail := partialSuffix(full, "<think>")
				if tail > 0 {
					e.tagBuffer = full[len(full)-tail:]
					normalText += full[:len(full)-tail]
				} else {
					normalText += full
				}
				return
			}
		}
	}
	return
}

func partialSuffix(s, tag string) int {
	maxCheck := len(tag) - 1
	if maxCheck > len(s) {
		maxCheck = len(s)
	}
	for i := maxCheck; i >= 1; i-- {
		if s[len(s)-i:] == tag[:i] {
			return i
		}
	}
	return 0
}

func cloneChunk(chunk J) J {
	out := J{}
	for k, v := range chunk {
		out[k] = v
	}
	choices := toSlice(out["choices"])
	if len(choices) > 0 {
		oldChoice, _ := choices[0].(map[string]interface{})
		newChoice := J{}
		for k, v := range oldChoice {
			newChoice[k] = v
		}
		oldDelta := toMap(oldChoice["delta"])
		newDelta := J{}
		for k, v := range oldDelta {
			newDelta[k] = v
		}
		newChoice["delta"] = newDelta
		out["choices"] = []interface{}{newChoice}
	}
	return out
}

func setDeltaField(chunk J, key string, value interface{}) {
	choices := toSlice(chunk["choices"])
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]interface{})
	delta := toMap(choice["delta"])
	delta[key] = value
}

func removeDeltaField(chunk J, key string) {
	choices := toSlice(chunk["choices"])
	if len(choices) == 0 {
		return
	}
	choice, _ := choices[0].(map[string]interface{})
	delta := toMap(choice["delta"])
	delete(delta, key)
}

func convertAnthropicMessages(msgs interface{}) interface{} {
	arr := toSlice(msgs)
	if arr == nil {
		return msgs
	}
	var converted []interface{}
	for _, raw := range arr {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			converted = append(converted, raw)
			continue
		}
		converted = append(converted, convertSingleMessage(msg)...)
	}
	return converted
}

func convertSingleMessage(msg J) []interface{} {
	content := msg["content"]
	arr, ok := content.([]interface{})
	if !ok {
		return []interface{}{msg}
	}

	hasToolUse := false
	hasToolResult := false
	for _, raw := range arr {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		t := toString(block["type"])
		if t == "tool_use" {
			hasToolUse = true
		}
		if t == "tool_result" {
			hasToolResult = true
		}
	}

	if !hasToolUse && !hasToolResult {
		return []interface{}{msg}
	}

	role := toString(msg["role"])
	if role == "assistant" && hasToolUse {
		return []interface{}{convertAssistantToolUseMessage(arr)}
	}
	if hasToolResult {
		return convertToolResultMessage(role, arr)
	}
	return []interface{}{msg}
}

func convertAssistantToolUseMessage(content []interface{}) J {
	var textParts []string
	var toolCalls []J
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch toString(block["type"]) {
		case "text":
			textParts = append(textParts, toString(block["text"]))
		case "tool_use":
			argsBytes, _ := json.Marshal(orDefault(block["input"], J{}))
			toolCalls = append(toolCalls, J{
				"id":   orDefault(block["id"], proxy.GenID("call_")),
				"type": "function",
				"function": J{
					"name":      block["name"],
					"arguments": string(argsBytes),
				},
			})
		}
	}
	result := J{
		"role":    "assistant",
		"content": nilIfEmpty(join(textParts, "\n")),
	}
	if len(toolCalls) > 0 {
		result["tool_calls"] = toolCalls
	}
	return result
}

func convertToolResultMessage(role string, content []interface{}) []interface{} {
	var converted []interface{}
	var otherParts []interface{}
	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if toString(block["type"]) == "tool_result" {
			c := block["content"]
			text := ""
			if s, ok := c.(string); ok {
				text = s
			} else if arr, ok := c.([]interface{}); ok {
				var parts []string
				for _, p := range arr {
					pm, ok := p.(map[string]interface{})
					if ok && toString(pm["type"]) == "text" {
						parts = append(parts, toString(pm["text"]))
					}
				}
				text = join(parts, "\n")
			}
			converted = append(converted, J{
				"role":         "tool",
				"tool_call_id": block["tool_use_id"],
				"content":      text,
			})
		} else {
			otherParts = append(otherParts, raw)
		}
	}
	if len(otherParts) > 0 {
		converted = append(converted, J{"role": role, "content": otherParts})
	}
	return converted
}

package adapter

import (
	"encoding/json"
	"strings"

	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

var geminiFinishReasonMap = map[string]string{
	"STOP":       "stop",
	"MAX_TOKENS": "length",
	"SAFETY":     "content_filter",
	"RECITATION": "content_filter",
}

func CCToGeminiRequest(payload J) J {
	messages := toSlice(payload["messages"])
	var systemParts []string
	var contents []J

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role := toString(msg["role"])
		if role == "system" || role == "developer" {
			systemParts = append(systemParts, flattenText(msg["content"]))
			continue
		}
		converted := convertMessageToGemini(msg)
		if converted != nil {
			contents = append(contents, converted)
		}
	}

	contents = mergeGeminiSameRole(contents)

	result := J{
		"contents":         contents,
		"generationConfig": buildGenerationConfig(payload),
	}
	if len(systemParts) > 0 {
		result["systemInstruction"] = J{
			"parts": []J{{"text": strings.Join(systemParts, "\n\n")}},
		}
	}
	if tools := convertToolsToGemini(payload["tools"]); tools != nil {
		result["tools"] = tools
	}
	return result
}

func GeminiToCCResponse(data J) J {
	requestID := proxy.GenID("chatcmpl-")
	candidates := toSlice(data["candidates"])
	var candidate J
	if len(candidates) > 0 {
		candidate, _ = candidates[0].(map[string]interface{})
	}
	if candidate == nil {
		candidate = J{}
	}

	content := toMap(candidate["content"])
	parts := toSlice(content["parts"])
	contentText, reasoningText, toolCalls := extractGeminiParts(parts)

	finish := toString(candidate["finishReason"])
	finishReason := "stop"
	if len(toolCalls) > 0 && finish == "STOP" {
		finishReason = "tool_calls"
	} else if fr, ok := geminiFinishReasonMap[finish]; ok {
		finishReason = fr
	}

	message := J{"role": "assistant", "content": nilIfEmpty(contentText)}
	if reasoningText != "" {
		message["reasoning_content"] = reasoningText
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	usage := convertGeminiUsage(toMap(data["usageMetadata"]))

	return J{
		"id":      requestID,
		"object":  "chat.completion",
		"model":   data["modelVersion"],
		"choices": []J{{"index": 0, "message": message, "finish_reason": finishReason}},
		"usage":   usage,
	}
}

type GeminiStreamConverter struct {
	id            string
	toolCallIndex int
	started       bool
}

func NewGeminiStreamConverter() *GeminiStreamConverter {
	return &GeminiStreamConverter{id: proxy.GenID("chatcmpl-")}
}

func (c *GeminiStreamConverter) ProcessChunk(data J) []J {
	var results []J
	candidates := toSlice(data["candidates"])
	if len(candidates) == 0 {
		return results
	}
	candidate, _ := candidates[0].(map[string]interface{})
	if candidate == nil {
		return results
	}
	content := toMap(candidate["content"])
	parts := toSlice(content["parts"])

	if !c.started {
		c.started = true
		results = append(results, c.makeChunk(J{"role": "assistant", "content": ""}, ""))
	}

	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		thought, _ := part["thought"].(bool)
		if thought {
			if text := toString(part["text"]); text != "" {
				results = append(results, c.makeChunk(J{"reasoning_content": text}, ""))
			}
		} else if text := toString(part["text"]); text != "" {
			results = append(results, c.makeChunk(J{"content": text}, ""))
		} else if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			argsBytes, _ := json.Marshal(fc["args"])
			results = append(results, c.makeChunk(J{
				"tool_calls": []J{{
					"index": c.toolCallIndex,
					"id":    orDefault(fc["id"], proxy.GenID("call_")),
					"type":  "function",
					"function": J{
						"name":      fc["name"],
						"arguments": string(argsBytes),
					},
				}},
			}, ""))
			c.toolCallIndex++
		}
	}

	finish := toString(candidate["finishReason"])
	if finish != "" {
		fr := "stop"
		if c.toolCallIndex > 0 && finish == "STOP" {
			fr = "tool_calls"
		} else if mapped, ok := geminiFinishReasonMap[finish]; ok {
			fr = mapped
		}
		chunk := c.makeChunk(J{}, fr)
		if meta := toMap(data["usageMetadata"]); len(meta) > 0 {
			chunk["usage"] = convertGeminiUsage(meta)
		}
		results = append(results, chunk)
	}
	return results
}

func (c *GeminiStreamConverter) makeChunk(delta J, finishReason string) J {
	choice := J{"index": 0, "delta": delta}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	return J{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"model":   "gemini",
		"choices": []J{choice},
	}
}

func convertMessageToGemini(msg J) J {
	role := toString(msg["role"])
	geminiRole := "user"
	if role == "assistant" {
		geminiRole = "model"
	}

	if role == "tool" {
		return J{
			"role": "user",
			"parts": []J{{
				"functionResponse": J{
					"name":     orDefault(msg["name"], msg["tool_call_id"]),
					"response": parseJSONSafe(msg["content"]),
				},
			}},
		}
	}

	var parts []J
	if rc := toString(msg["reasoning_content"]); rc != "" {
		parts = append(parts, J{"text": rc, "thought": true})
	}

	content := msg["content"]
	if s, ok := content.(string); ok && s != "" {
		parts = append(parts, J{"text": s})
	} else if arr, ok := content.([]interface{}); ok {
		for _, raw := range arr {
			block, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if toString(block["type"]) == "text" {
				parts = append(parts, J{"text": block["text"]})
			}
		}
	}

	for _, raw := range toSlice(msg["tool_calls"]) {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn := toMap(tc["function"])
		parts = append(parts, J{
			"functionCall": J{
				"name": fn["name"],
				"args": parseJSONSafe(fn["arguments"]),
			},
		})
	}

	if len(parts) == 0 {
		return nil
	}
	return J{"role": geminiRole, "parts": parts}
}

func buildGenerationConfig(payload J) J {
	config := J{}
	if v, ok := payload["max_tokens"]; ok {
		config["maxOutputTokens"] = v
	} else if v, ok := payload["max_completion_tokens"]; ok {
		config["maxOutputTokens"] = v
	}
	if v, ok := payload["temperature"]; ok {
		config["temperature"] = v
	}
	if v, ok := payload["top_p"]; ok {
		config["topP"] = v
	}
	if stop, ok := payload["stop"]; ok {
		if arr, ok := stop.([]interface{}); ok {
			config["stopSequences"] = arr
		} else {
			config["stopSequences"] = []interface{}{stop}
		}
	}
	return config
}

func convertToolsToGemini(tools interface{}) []J {
	arr := toSlice(tools)
	if len(arr) == 0 {
		return nil
	}
	var declarations []J
	for _, raw := range arr {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn := tool
		if toString(tool["type"]) == "function" {
			if f, ok := tool["function"].(map[string]interface{}); ok {
				fn = f
			}
		}
		if _, ok := fn["name"]; !ok {
			continue
		}
		decl := J{
			"name":        fn["name"],
			"description": fn["description"],
		}
		if p, ok := fn["parameters"]; ok {
			decl["parameters"] = p
		}
		declarations = append(declarations, decl)
	}
	if len(declarations) == 0 {
		return nil
	}
	return []J{{"functionDeclarations": declarations}}
}

func extractGeminiParts(parts []interface{}) (string, string, []J) {
	var text, reasoning string
	var toolCalls []J
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		thought, _ := part["thought"].(bool)
		if thought && part["text"] != nil {
			reasoning += toString(part["text"])
		} else if part["text"] != nil {
			text += toString(part["text"])
		} else if fc, ok := part["functionCall"].(map[string]interface{}); ok {
			argsBytes, _ := json.Marshal(fc["args"])
			toolCalls = append(toolCalls, J{
				"index": len(toolCalls),
				"id":    orDefault(fc["id"], proxy.GenID("call_")),
				"type":  "function",
				"function": J{
					"name":      fc["name"],
					"arguments": string(argsBytes),
				},
			})
		}
	}
	return text, reasoning, toolCalls
}

func convertGeminiUsage(meta J) J {
	prompt := toInt(meta["promptTokenCount"])
	candidates := toInt(meta["candidatesTokenCount"])
	thoughts := toInt(meta["thoughtsTokenCount"])
	completion := candidates + thoughts
	return J{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      prompt + completion,
	}
}

func mergeGeminiSameRole(contents []J) []J {
	if len(contents) == 0 {
		return contents
	}
	merged := []J{contents[0]}
	for _, c := range contents[1:] {
		last := merged[len(merged)-1]
		if toString(c["role"]) == toString(last["role"]) {
			lastParts := toSlice(last["parts"])
			curParts := toSlice(c["parts"])
			last["parts"] = append(lastParts, curParts...)
			merged[len(merged)-1] = last
		} else {
			merged = append(merged, c)
		}
	}
	return merged
}

func parseJSONSafe(v interface{}) interface{} {
	if v == nil {
		return J{}
	}
	if s, ok := v.(string); ok {
		var m J
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
		if s != "" {
			return J{"result": s}
		}
		return J{}
	}
	return v
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

package adapter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

type J = map[string]interface{}

var anthropicStopReasonMap = map[string]string{
	"end_turn":      "stop",
	"max_tokens":    "length",
	"tool_use":      "tool_calls",
	"stop_sequence": "stop",
}

// ─── CC → Anthropic Messages ──────────────────

func CCToMessagesRequest(payload J) J {
	messages := toSlice(payload["messages"])
	var anthropicMessages []J
	var systemParts []string

	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		converted, systemText := convertRequestMessage(msg)
		if systemText != "" {
			systemParts = append(systemParts, systemText)
			continue
		}
		if converted != nil {
			anthropicMessages = append(anthropicMessages, converted)
		}
	}

	anthropicMessages = mergeSameRole(anthropicMessages)

	result := J{
		"model":      payload["model"],
		"messages":   anthropicMessages,
		"max_tokens": maxTokens(payload),
	}
	if len(systemParts) > 0 {
		result["system"] = strings.Join(systemParts, "\n\n")
	}
	if tools, ok := payload["tools"]; ok {
		result["tools"] = convertToolsToAnthropic(tools)
	}
	for _, key := range []string{"temperature", "top_p", "stream"} {
		if v, ok := payload[key]; ok {
			result[key] = v
		}
	}
	return result
}

// ─── Anthropic Messages → CC ──────────────────

func MessagesToCCResponse(data J) J {
	requestID := proxy.GenID("chatcmpl-")
	contentBlocks := toSlice(data["content"])
	contentText, reasoningText, toolCalls := collectResponseParts(contentBlocks)
	message := buildCCMessage(contentText, reasoningText, toolCalls)

	usage := toMap(data["usage"])
	stopReason := toString(data["stop_reason"])
	finishReason := "stop"
	if fr, ok := anthropicStopReasonMap[stopReason]; ok {
		finishReason = fr
	}

	return J{
		"id":     requestID,
		"object": "chat.completion",
		"model":  data["model"],
		"choices": []J{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": buildCCUsage(toInt(usage["input_tokens"]), toInt(usage["output_tokens"])),
	}
}

// ─── Anthropic Stream Converter ────────────────

type AnthropicStreamConverter struct {
	id           string
	toolIndex    int
	inputTokens  int
	outputTokens int
}

func NewAnthropicStreamConverter() *AnthropicStreamConverter {
	return &AnthropicStreamConverter{
		id:        proxy.GenID("chatcmpl-"),
		toolIndex: -1,
	}
}

func (c *AnthropicStreamConverter) ProcessEvent(eventType string, eventData J) []J {
	switch eventType {
	case "message_start":
		return c.handleMessageStart(eventData)
	case "content_block_start":
		return c.handleContentBlockStart(eventData)
	case "content_block_delta":
		return c.handleContentBlockDelta(eventData)
	case "message_delta":
		return c.handleMessageDelta(eventData)
	}
	return nil
}

func (c *AnthropicStreamConverter) handleMessageStart(data J) []J {
	msg := toMap(data["message"])
	usage := toMap(msg["usage"])
	c.inputTokens = toInt(usage["input_tokens"])
	chunk := c.makeChunk(J{"role": "assistant", "content": ""}, "")
	return []J{chunk}
}

func (c *AnthropicStreamConverter) handleContentBlockStart(data J) []J {
	block := toMap(data["content_block"])
	if toString(block["type"]) != "tool_use" {
		return nil
	}
	c.toolIndex++
	return []J{c.makeChunk(J{
		"tool_calls": []J{{
			"index": c.toolIndex,
			"id":    orDefault(block["id"], proxy.GenID("toolu_")),
			"type":  "function",
			"function": J{
				"name":      block["name"],
				"arguments": "",
			},
		}},
	}, "")}
}

func (c *AnthropicStreamConverter) handleContentBlockDelta(data J) []J {
	delta := toMap(data["delta"])
	deltaType := toString(delta["type"])

	if deltaType == "text_delta" {
		if text := toString(delta["text"]); text != "" {
			return []J{c.makeChunk(J{"content": text}, "")}
		}
	}
	if deltaType == "thinking_delta" {
		if text := toString(delta["thinking"]); text != "" {
			return []J{c.makeChunk(J{"reasoning_content": text}, "")}
		}
	}
	if deltaType == "input_json_delta" {
		if pj := toString(delta["partial_json"]); pj != "" {
			return []J{c.makeChunk(J{
				"tool_calls": []J{{
					"index":    c.toolIndex,
					"function": J{"arguments": pj},
				}},
			}, "")}
		}
	}
	return nil
}

func (c *AnthropicStreamConverter) handleMessageDelta(data J) []J {
	delta := toMap(data["delta"])
	usage := toMap(data["usage"])
	c.outputTokens = toInt(usage["output_tokens"])
	stopReason := toString(delta["stop_reason"])
	finishReason := "stop"
	if fr, ok := anthropicStopReasonMap[stopReason]; ok {
		finishReason = fr
	}
	chunk := c.makeChunk(J{}, finishReason)
	chunk["usage"] = buildCCUsage(c.inputTokens, c.outputTokens)
	return []J{chunk}
}

func (c *AnthropicStreamConverter) makeChunk(delta J, finishReason string) J {
	choice := J{"index": 0, "delta": delta}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	return J{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"model":   "claude",
		"choices": []J{choice},
	}
}

// ─── Helpers ──────────────────────────────────

func convertRequestMessage(msg J) (J, string) {
	role := toString(msg["role"])
	content := msg["content"]

	if role == "system" {
		return nil, flattenText(content)
	}
	if role == "tool" {
		return convertToolRoleMessage(msg), ""
	}

	anthropicRole := "user"
	if role == "assistant" {
		anthropicRole = "assistant"
	}
	anthropicContent := convertContent(msg)

	if role == "assistant" {
		if rc := toString(msg["reasoning_content"]); rc != "" {
			blocks := toBlocks(anthropicContent)
			blocks = append([]J{{"type": "thinking", "thinking": rc}}, blocks...)
			anthropicContent = blocks
		}
		if toolCalls := toSlice(msg["tool_calls"]); len(toolCalls) > 0 {
			anthropicContent = appendToolUseBlocks(anthropicContent, toolCalls)
		}
	}

	if anthropicContent == nil {
		return nil, ""
	}
	return J{"role": anthropicRole, "content": anthropicContent}, ""
}

func convertToolRoleMessage(msg J) J {
	content := msg["content"]
	text := ""
	if s, ok := content.(string); ok {
		text = s
	} else {
		b, _ := json.Marshal(content)
		text = string(b)
	}
	return J{
		"role": "user",
		"content": []J{{
			"type":        "tool_result",
			"tool_use_id": msg["tool_call_id"],
			"content":     text,
		}},
	}
}

func appendToolUseBlocks(content interface{}, toolCalls []interface{}) []J {
	blocks := toBlocks(content)
	for _, raw := range toolCalls {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn := toMap(tc["function"])
		blocks = append(blocks, J{
			"type":  "tool_use",
			"id":    orDefault(tc["id"], proxy.GenID("toolu_")),
			"name":  fn["name"],
			"input": parseToolArguments(fn["arguments"]),
		})
	}
	return blocks
}

func convertContent(msg J) interface{} {
	content := msg["content"]
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		var blocks []J
		for _, raw := range arr {
			part, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			converted := convertContentPart(part)
			if converted != nil {
				blocks = append(blocks, converted)
			}
		}
		return blocks
	}
	return fmt.Sprintf("%v", content)
}

func convertContentPart(part J) J {
	partType := toString(part["type"])
	switch partType {
	case "text":
		return J{"type": "text", "text": part["text"]}
	case "image_url":
		return convertImage(part)
	case "image", "tool_use", "tool_result":
		return part
	}
	return nil
}

func convertImage(part J) J {
	urlData := toMap(part["image_url"])
	url := toString(urlData["url"])
	if strings.HasPrefix(url, "data:") {
		idx := strings.Index(url, ";base64,")
		if idx > 0 {
			mediaType := strings.TrimPrefix(url[:idx], "data:")
			if mediaType == "" {
				mediaType = "image/png"
			}
			b64 := url[idx+8:]
			return J{
				"type": "image",
				"source": J{
					"type":       "base64",
					"media_type": mediaType,
					"data":       b64,
				},
			}
		}
	}
	return J{
		"type": "image",
		"source": J{
			"type": "url",
			"url":  url,
		},
	}
}

func convertToolsToAnthropic(tools interface{}) []J {
	arr := toSlice(tools)
	var result []J
	for _, raw := range arr {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if toString(tool["type"]) == "function" {
			if fn, ok := tool["function"].(map[string]interface{}); ok {
				result = append(result, J{
					"name":         fn["name"],
					"description":  fn["description"],
					"input_schema": orDefault(fn["parameters"], J{"type": "object", "properties": J{}}),
				})
				continue
			}
		}
		if _, ok := tool["name"]; ok {
			result = append(result, J{
				"name":         tool["name"],
				"description":  tool["description"],
				"input_schema": orDefault(tool["input_schema"], J{"type": "object", "properties": J{}}),
			})
		}
	}
	return result
}

func collectResponseParts(blocks []interface{}) (string, string, []J) {
	var contentText, reasoningText string
	var toolCalls []J

	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		blockType := toString(block["type"])
		switch blockType {
		case "text":
			contentText += toString(block["text"])
		case "thinking":
			reasoningText += toString(block["thinking"])
		case "tool_use":
			idx := len(toolCalls)
			input := block["input"]
			var argsText string
			if m, ok := input.(map[string]interface{}); ok {
				b, _ := json.Marshal(m)
				argsText = string(b)
			} else {
				argsText = fmt.Sprintf("%v", input)
			}
			toolCalls = append(toolCalls, J{
				"index": idx,
				"id":    orDefault(block["id"], proxy.GenID("toolu_")),
				"type":  "function",
				"function": J{
					"name":      block["name"],
					"arguments": argsText,
				},
			})
		}
	}
	return contentText, reasoningText, toolCalls
}

func buildCCMessage(contentText, reasoningText string, toolCalls []J) J {
	msg := J{
		"role":    "assistant",
		"content": nilIfEmpty(contentText),
	}
	if reasoningText != "" {
		msg["reasoning_content"] = reasoningText
	}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	return msg
}

func buildCCUsage(inputTokens, outputTokens int) J {
	return J{
		"prompt_tokens":     inputTokens,
		"completion_tokens": outputTokens,
		"total_tokens":      inputTokens + outputTokens,
	}
}

func maxTokens(payload J) int {
	if v, ok := payload["max_tokens"]; ok {
		n := toInt(v)
		if n > 8192 {
			return n
		}
	}
	return 8192
}

func parseToolArguments(args interface{}) interface{} {
	if args == nil {
		return J{}
	}
	if s, ok := args.(string); ok {
		var m J
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
		return J{}
	}
	return args
}

func mergeSameRole(messages []J) []J {
	if len(messages) == 0 {
		return messages
	}
	merged := []J{messages[0]}
	for _, msg := range messages[1:] {
		last := merged[len(merged)-1]
		if toString(msg["role"]) == toString(last["role"]) {
			prevBlocks := toBlocks(last["content"])
			curBlocks := toBlocks(msg["content"])
			last["content"] = append(prevBlocks, curBlocks...)
			merged[len(merged)-1] = last
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

// ─── Responses → CC ──────────────────────────

func ResponsesToCC(payload J) J {
	var messages []J

	if instr := toString(payload["instructions"]); instr != "" {
		messages = append(messages, J{"role": "system", "content": instr})
	}

	input := payload["input"]
	if s, ok := input.(string); ok {
		messages = append(messages, J{"role": "user", "content": s})
	} else if items, ok := input.([]interface{}); ok {
		convertInputItems(items, &messages)
	}

	result := J{
		"model":    payload["model"],
		"messages": messages,
		"stream":   payload["stream"],
	}
	copyRequestOptions(payload, result)
	return result
}

// ─── CC → Responses ──────────────────────────

func CCToResponses(ccResp J, model string) J {
	choices := toSlice(ccResp["choices"])
	var choice J
	if len(choices) > 0 {
		choice, _ = choices[0].(map[string]interface{})
	}
	if choice == nil {
		choice = J{}
	}
	message := toMap(choice["message"])
	finishReason := toString(choice["finish_reason"])

	status := "completed"
	if finishReason == "length" {
		status = "incomplete"
	}

	if model == "" {
		model = toString(ccResp["model"])
	}

	return J{
		"id":     orDefault(ccResp["id"], proxy.GenID("resp_")),
		"object": "response",
		"status": status,
		"model":  model,
		"output": buildResponsesOutput(message),
		"usage":  buildResponsesUsage(toMap(ccResp["usage"])),
	}
}

// ─── Responses → CC Response ──────────────────

func ResponsesToCCResponse(responseData J, model string) J {
	outputItems := toSlice(responseData["output"])
	contentText, reasoningText, toolCalls := collectCCPartsFromResponsesOutput(outputItems)

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	} else if toString(responseData["status"]) == "incomplete" {
		finishReason = "length"
	}

	message := J{"role": "assistant", "content": nilIfEmpty(contentText)}
	if reasoningText != "" {
		message["reasoning_content"] = reasoningText
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	usage := toMap(responseData["usage"])
	if model == "" {
		model = toString(responseData["model"])
	}

	return J{
		"id":     orDefault(responseData["id"], proxy.GenID("chatcmpl-")),
		"object": "chat.completion",
		"model":  model,
		"choices": []J{{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": J{
			"prompt_tokens":     toInt(usage["input_tokens"]),
			"completion_tokens": toInt(usage["output_tokens"]),
			"total_tokens":      toInt(usage["total_tokens"]),
		},
	}
}

// ─── CC → Responses Request ──────────────────

func CCToResponsesRequest(payload J) J {
	var instructions []string
	var inputItems []J

	for _, raw := range toSlice(payload["messages"]) {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role := toString(msg["role"])
		content := msg["content"]

		if role == "system" {
			if text := contentToText(content); text != "" {
				instructions = append(instructions, text)
			}
			continue
		}
		if role == "tool" {
			inputItems = append(inputItems, J{
				"type":    "function_call_output",
				"call_id": msg["tool_call_id"],
				"output":  stringifyOutput(content),
			})
			continue
		}

		text := contentToText(content)
		toolCalls := toSlice(msg["tool_calls"])

		if role == "assistant" && len(toolCalls) > 0 {
			if text != "" {
				inputItems = append(inputItems, J{
					"type":    "message",
					"role":    "assistant",
					"content": []J{{"type": "output_text", "text": text}},
				})
			}
			for _, raw := range toolCalls {
				tc, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				fn := toMap(tc["function"])
				inputItems = append(inputItems, J{
					"type":      "function_call",
					"call_id":   orDefault(tc["id"], proxy.GenID("call_")),
					"name":      fn["name"],
					"arguments": orDefault(fn["arguments"], "{}"),
				})
			}
		} else {
			r := role
			if r == "" {
				r = "user"
			}
			inputItems = append(inputItems, J{"role": r, "content": text})
		}
	}

	result := J{
		"model":  payload["model"],
		"input":  inputItems,
		"stream": payload["stream"],
	}
	if len(instructions) > 0 {
		result["instructions"] = join(instructions, "\n\n")
	}
	copyResponsesRequestOptions(payload, result)
	return result
}

// ─── Responses Stream Converter (CC → Responses SSE) ─────

type ResponsesStreamConverter struct {
	RespID     string
	Model      string
	rsBuf      string
	rsStarted  bool
	rsClosed   bool
	rsID       string
	textBuf    string
	textStarted bool
	textClosed  bool
	msgID      string
	tools      map[int]*toolBuffer
	outputItems []J
	finished   bool
	inputTokens int
}

type toolBuffer struct {
	name   string
	args   string
	callID string
	fcID   string
}

func NewResponsesStreamConverter(model string) *ResponsesStreamConverter {
	return &ResponsesStreamConverter{
		RespID: proxy.GenID("resp_"),
		Model:  model,
		rsID:   proxy.GenID("rs_"),
		msgID:  proxy.GenID("msg_"),
		tools:  make(map[int]*toolBuffer),
	}
}

func (c *ResponsesStreamConverter) StartEvents() []string {
	return []string{c.sse("response.created", J{
		"id":     c.RespID,
		"object": "response",
		"status": "in_progress",
		"model":  c.Model,
		"output": []J{},
	})}
}

func (c *ResponsesStreamConverter) ProcessCCChunk(chunk J) []string {
	var events []string
	for _, raw := range toSlice(chunk["choices"]) {
		choice, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		events = append(events, c.processCCChoice(choice, chunk["usage"])...)
	}
	return events
}

func (c *ResponsesStreamConverter) ProcessAnthropicEvent(eventType string, eventData J) []string {
	switch eventType {
	case "message_start":
		usage := toMap(toMap(eventData["message"])["usage"])
		c.inputTokens = toInt(usage["input_tokens"])
		return nil
	case "content_block_start":
		block := toMap(eventData["content_block"])
		switch toString(block["type"]) {
		case "thinking":
			return c.ensureReasoningStarted()
		case "text":
			return c.ensureTextStarted()
		case "tool_use":
			return c.startTool(len(c.tools), toString(orDefault(block["id"], proxy.GenID("toolu_")).(interface{})), toString(block["name"]))
		}
		return nil
	case "content_block_delta":
		delta := toMap(eventData["delta"])
		deltaType := toString(delta["type"])
		if deltaType == "thinking_delta" {
			if t := toString(delta["thinking"]); t != "" {
				return c.appendReasoningDelta(t)
			}
		}
		if deltaType == "text_delta" {
			if t := toString(delta["text"]); t != "" {
				return c.appendTextDelta(t)
			}
		}
		if deltaType == "input_json_delta" && len(c.tools) > 0 {
			if pj := toString(delta["partial_json"]); pj != "" {
				maxIdx := 0
				for k := range c.tools {
					if k > maxIdx {
						maxIdx = k
					}
				}
				return c.appendToolArguments(maxIdx, pj)
			}
		}
		return nil
	case "message_delta":
		if c.finished {
			return nil
		}
		delta := toMap(eventData["delta"])
		usage := toMap(eventData["usage"])
		stopReason := toString(delta["stop_reason"])
		fr := "stop"
		switch stopReason {
		case "tool_use":
			fr = "tool_calls"
		case "max_tokens":
			fr = "length"
		}
		c.finished = true
		usagePayload := J{
			"input_tokens":  c.inputTokens,
			"output_tokens": toInt(usage["output_tokens"]),
			"total_tokens":  c.inputTokens + toInt(usage["output_tokens"]),
		}
		return c.finishStream(fr, usagePayload)
	}
	return nil
}

func (c *ResponsesStreamConverter) Finalize() []string {
	if c.finished {
		return nil
	}
	c.finished = true
	return c.finishStream("stop", nil)
}

func (c *ResponsesStreamConverter) processCCChoice(choice J, usage interface{}) []string {
	var events []string
	delta := toMap(choice["delta"])
	finishReason := toString(choice["finish_reason"])

	if rc := toString(delta["reasoning_content"]); rc != "" {
		events = append(events, c.appendReasoningDelta(rc)...)
	}
	if content, ok := delta["content"]; ok && content != nil && toString(content) != "" {
		events = append(events, c.appendTextDelta(toString(content))...)
	}
	for _, raw := range toSlice(delta["tool_calls"]) {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		events = append(events, c.onToolCall(tc)...)
	}

	if finishReason != "" && !c.finished {
		c.finished = true
		events = append(events, c.finishStream(finishReason, usage)...)
	}
	return events
}

func (c *ResponsesStreamConverter) ensureReasoningStarted() []string {
	if c.rsStarted {
		return nil
	}
	c.rsStarted = true
	return []string{c.sse("response.output_item.added", J{
		"type":    "reasoning",
		"id":      c.rsID,
		"summary": []J{},
	})}
}

func (c *ResponsesStreamConverter) appendReasoningDelta(text string) []string {
	events := c.ensureReasoningStarted()
	c.rsBuf += text
	events = append(events, c.sse("response.reasoning_summary_text.delta", J{
		"type":  "summary_text",
		"delta": text,
	}))
	return events
}

func (c *ResponsesStreamConverter) ensureTextStarted() []string {
	var events []string
	if c.rsStarted && !c.rsClosed {
		events = append(events, c.closeReasoning()...)
	}
	if !c.textStarted {
		c.textStarted = true
		events = append(events, c.sse("response.output_item.added", J{
			"type":    "message",
			"id":      c.msgID,
			"status":  "in_progress",
			"role":    "assistant",
			"content": []J{},
		}))
		events = append(events, c.sse("response.content_part.added", J{
			"type": "output_text",
			"text": "",
		}))
	}
	return events
}

func (c *ResponsesStreamConverter) appendTextDelta(text string) []string {
	events := c.ensureTextStarted()
	c.textBuf += text
	events = append(events, c.sse("response.output_text.delta", J{
		"type":  "output_text",
		"delta": text,
	}))
	return events
}

func (c *ResponsesStreamConverter) onToolCall(tc J) []string {
	var events []string
	index := toInt(tc["index"])
	fn := toMap(tc["function"])

	if _, exists := c.tools[index]; !exists {
		callID := toString(tc["id"])
		if callID == "" {
			callID = proxy.GenID("call_")
		}
		name := toString(fn["name"])
		events = append(events, c.startTool(index, callID, name)...)
	}

	if name := toString(fn["name"]); name != "" {
		c.tools[index].name = name
	}
	if args := toString(fn["arguments"]); args != "" {
		events = append(events, c.appendToolArguments(index, args)...)
	}
	return events
}

func (c *ResponsesStreamConverter) startTool(index int, callID, name string) []string {
	var events []string
	if c.rsStarted && !c.rsClosed {
		events = append(events, c.closeReasoning()...)
	}
	if c.textStarted && !c.textClosed {
		events = append(events, c.closeText()...)
	}

	fcID := proxy.GenID("fc_")
	c.tools[index] = &toolBuffer{name: name, callID: callID, fcID: fcID}
	events = append(events, c.sse("response.output_item.added", J{
		"type":      "function_call",
		"id":        fcID,
		"status":    "in_progress",
		"call_id":   callID,
		"name":      name,
		"arguments": "",
	}))
	return events
}

func (c *ResponsesStreamConverter) appendToolArguments(index int, delta string) []string {
	buf := c.tools[index]
	buf.args += delta
	return []string{c.sse("response.function_call_arguments.delta", J{
		"type":  "function_call",
		"delta": delta,
	})}
}

func (c *ResponsesStreamConverter) closeReasoning() []string {
	if c.rsClosed {
		return nil
	}
	c.rsClosed = true
	item := J{
		"type":    "reasoning",
		"id":      c.rsID,
		"summary": []J{{"type": "summary_text", "text": c.rsBuf}},
	}
	c.outputItems = append(c.outputItems, item)
	return []string{
		c.sse("response.reasoning_summary_text.done", J{"type": "summary_text", "text": c.rsBuf}),
		c.sse("response.output_item.done", item),
	}
}

func (c *ResponsesStreamConverter) closeText() []string {
	if c.textClosed {
		return nil
	}
	c.textClosed = true
	item := J{
		"type":    "message",
		"id":      c.msgID,
		"status":  "completed",
		"role":    "assistant",
		"content": []J{{"type": "output_text", "text": c.textBuf}},
	}
	c.outputItems = append(c.outputItems, item)
	return []string{
		c.sse("response.output_text.done", J{"type": "output_text", "text": c.textBuf}),
		c.sse("response.output_item.done", item),
	}
}

func (c *ResponsesStreamConverter) finishStream(finishReason string, usage interface{}) []string {
	var events []string
	if c.rsStarted && !c.rsClosed {
		events = append(events, c.closeReasoning()...)
	}
	if c.textStarted && !c.textClosed {
		events = append(events, c.closeText()...)
	}
	events = append(events, c.finishToolCalls()...)

	usageData := J{}
	if m, ok := usage.(map[string]interface{}); ok {
		usageData = m
	}

	status := "completed"
	if finishReason == "length" {
		status = "incomplete"
	}

	events = append(events, c.sse("response.completed", J{
		"id":     c.RespID,
		"object": "response",
		"status": status,
		"model":  c.Model,
		"output": c.outputItems,
		"usage":  usageData,
	}))
	return events
}

func (c *ResponsesStreamConverter) finishToolCalls() []string {
	var events []string
	for i := 0; i < len(c.tools); i++ {
		buf, ok := c.tools[i]
		if !ok {
			continue
		}
		events = append(events, c.sse("response.function_call_arguments.done", J{
			"type":      "function_call",
			"arguments": buf.args,
		}))
		item := J{
			"type":      "function_call",
			"id":        buf.fcID,
			"status":    "completed",
			"call_id":   buf.callID,
			"name":      buf.name,
			"arguments": buf.args,
		}
		events = append(events, c.sse("response.output_item.done", item))
		c.outputItems = append(c.outputItems, item)
	}
	return events
}

func (c *ResponsesStreamConverter) sse(eventType string, data J) string {
	b, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s\n\n", eventType, string(b))
}

// ─── ResponsesToCCStreamConverter ──────────────

type ResponsesToCCStreamConverter struct {
	id        string
	model     string
	toolIndex int
	toolSlots map[string]int
}

func NewResponsesToCCStreamConverter(model string) *ResponsesToCCStreamConverter {
	return &ResponsesToCCStreamConverter{
		id:        proxy.GenID("chatcmpl-"),
		model:     model,
		toolSlots: make(map[string]int),
	}
}

func (c *ResponsesToCCStreamConverter) ProcessEvent(eventType string, eventData J) []J {
	switch eventType {
	case "response.created":
		return []J{c.makeChunk(J{"role": "assistant", "content": ""}, "")}
	case "response.output_text.delta":
		return []J{c.makeChunk(J{"content": eventData["delta"]}, "")}
	case "response.reasoning_summary_text.delta":
		return []J{c.makeChunk(J{"reasoning_content": eventData["delta"]}, "")}
	case "response.output_item.added":
		return c.handleOutputItemAdded(eventData)
	case "response.function_call_arguments.delta":
		return c.handleFunctionArgsDelta(eventData)
	case "response.completed":
		return c.handleCompleted(eventData)
	}
	return nil
}

func (c *ResponsesToCCStreamConverter) handleOutputItemAdded(data J) []J {
	item := toMap(data["item"])
	if len(item) == 0 {
		item = data
	}
	if toString(item["type"]) != "function_call" {
		return nil
	}
	callID := toString(item["call_id"])
	if callID == "" {
		callID = proxy.GenID("call_")
	}
	index, exists := c.toolSlots[callID]
	if !exists {
		index = c.toolIndex
		c.toolSlots[callID] = index
		c.toolIndex++
	}
	return []J{c.makeChunk(J{
		"tool_calls": []J{{
			"index": index,
			"id":    callID,
			"type":  "function",
			"function": J{
				"name":      item["name"],
				"arguments": "",
			},
		}},
	}, "")}
}

func (c *ResponsesToCCStreamConverter) handleFunctionArgsDelta(data J) []J {
	if len(c.toolSlots) == 0 {
		return nil
	}
	maxIdx := 0
	for _, v := range c.toolSlots {
		if v > maxIdx {
			maxIdx = v
		}
	}
	return []J{c.makeChunk(J{
		"tool_calls": []J{{
			"index":    maxIdx,
			"function": J{"arguments": data["delta"]},
		}},
	}, "")}
}

func (c *ResponsesToCCStreamConverter) handleCompleted(data J) []J {
	resp := toMap(data["response"])
	if len(resp) == 0 {
		resp = data
	}
	usage := toMap(resp["usage"])
	hasTool := false
	for _, raw := range toSlice(resp["output"]) {
		item, ok := raw.(map[string]interface{})
		if ok && toString(item["type"]) == "function_call" {
			hasTool = true
			break
		}
	}
	fr := "stop"
	if hasTool {
		fr = "tool_calls"
	}
	chunk := c.makeChunk(J{}, fr)
	chunk["usage"] = J{
		"prompt_tokens":     toInt(usage["input_tokens"]),
		"completion_tokens": toInt(usage["output_tokens"]),
		"total_tokens":      toInt(usage["total_tokens"]),
	}
	return []J{chunk}
}

func (c *ResponsesToCCStreamConverter) makeChunk(delta J, finishReason string) J {
	choice := J{"index": 0, "delta": delta}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	}
	return J{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"model":   c.model,
		"choices": []J{choice},
	}
}

// ─── Helpers ──────────────────────────────────

func buildResponsesOutput(message J) []J {
	var output []J
	if rc := toString(message["reasoning_content"]); rc != "" {
		output = append(output, J{
			"type":    "reasoning",
			"id":      proxy.GenID("rs_"),
			"summary": []J{{"type": "summary_text", "text": rc}},
		})
	}
	if text := toString(message["content"]); text != "" {
		output = append(output, J{
			"type":    "message",
			"id":      proxy.GenID("msg_"),
			"status":  "completed",
			"role":    "assistant",
			"content": []J{{"type": "output_text", "text": text}},
		})
	}
	for _, raw := range toSlice(message["tool_calls"]) {
		tc, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		fn := toMap(tc["function"])
		output = append(output, J{
			"type":      "function_call",
			"id":        proxy.GenID("fc_"),
			"status":    "completed",
			"call_id":   orDefault(tc["id"], proxy.GenID("call_")),
			"name":      fn["name"],
			"arguments": orDefault(fn["arguments"], "{}"),
		})
	}
	return output
}

func buildResponsesUsage(usage J) J {
	return J{
		"input_tokens":  toInt(usage["prompt_tokens"]),
		"output_tokens": toInt(usage["completion_tokens"]),
		"total_tokens":  toInt(usage["total_tokens"]),
	}
}

func collectCCPartsFromResponsesOutput(items []interface{}) (string, string, []J) {
	var contentText, reasoningText string
	var toolCalls []J
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch toString(item["type"]) {
		case "message":
			contentText += extractText(item["content"])
		case "reasoning":
			reasoningText += extractReasoningText(item)
		case "function_call":
			toolCalls = append(toolCalls, J{
				"index": len(toolCalls),
				"id":    orDefault(item["call_id"], proxy.GenID("call_")),
				"type":  "function",
				"function": J{
					"name":      item["name"],
					"arguments": orDefault(item["arguments"], "{}"),
				},
			})
		}
	}
	return contentText, reasoningText, toolCalls
}

func extractText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	arr := toSlice(content)
	var texts []string
	for _, raw := range arr {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		t := toString(part["type"])
		if t == "output_text" || t == "input_text" || t == "text" {
			texts = append(texts, toString(part["text"]))
		}
	}
	return join(texts, "\n")
}

func extractReasoningText(item J) string {
	summary := toSlice(item["summary"])
	var texts []string
	for _, raw := range summary {
		part, ok := raw.(map[string]interface{})
		if ok && toString(part["type"]) == "summary_text" {
			texts = append(texts, toString(part["text"]))
		}
	}
	return join(texts, "")
}

func contentToText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		return extractText(arr)
	}
	if content != nil {
		return fmt.Sprintf("%v", content)
	}
	return ""
}

func stringifyOutput(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if content == nil {
		return ""
	}
	b, _ := json.Marshal(content)
	return string(b)
}

func convertInputItems(items []interface{}, messages *[]J) {
	for i := 0; i < len(items); i++ {
		item := items[i]
		if s, ok := item.(string); ok {
			*messages = append(*messages, J{"role": "user", "content": s})
			continue
		}
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		itemType := toString(m["type"])
		role := toString(m["role"])

		if role != "" && itemType == "" {
			*messages = append(*messages, J{"role": role, "content": contentToText(m["content"])})
			continue
		}

		switch itemType {
		case "message":
			text := extractText(m["content"])
			msg := J{"role": orDefault(role, "assistant"), "content": text}
			*messages = append(*messages, msg)
		case "function_call":
			fn := J{
				"id":   orDefault(m["call_id"], proxy.GenID("call_")),
				"type": "function",
				"function": J{
					"name":      m["name"],
					"arguments": orDefault(m["arguments"], "{}"),
				},
			}
			if len(*messages) > 0 {
				last := (*messages)[len(*messages)-1]
				if toString(last["role"]) == "assistant" {
					tcs := toSlice(last["tool_calls"])
					last["tool_calls"] = append(tcs, fn)
					last["content"] = nil
					(*messages)[len(*messages)-1] = last
					continue
				}
			}
			*messages = append(*messages, J{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": []interface{}{fn},
			})
		case "function_call_output":
			output := toString(m["output"])
			*messages = append(*messages, J{
				"role":         "tool",
				"tool_call_id": m["call_id"],
				"content":      output,
			})
		}
	}
}

func copyRequestOptions(payload, result J) {
	if tools, ok := payload["tools"]; ok {
		result["tools"] = convertResponsesTools(tools)
	}
	for _, key := range []string{"temperature", "top_p"} {
		if v, ok := payload[key]; ok {
			result[key] = v
		}
	}
	if v, ok := payload["max_output_tokens"]; ok {
		result["max_tokens"] = v
	}
	if v, ok := payload["tool_choice"]; ok {
		result["tool_choice"] = v
	}
}

func copyResponsesRequestOptions(payload, result J) {
	if tools, ok := payload["tools"]; ok {
		result["tools"] = convertCCToolsToResponses(tools)
	}
	for _, key := range []string{"temperature", "top_p", "tool_choice"} {
		if v, ok := payload[key]; ok {
			result[key] = v
		}
	}
	if v, ok := payload["max_tokens"]; ok {
		result["max_output_tokens"] = v
	}
}

func convertResponsesTools(tools interface{}) []J {
	arr := toSlice(tools)
	var result []J
	for _, raw := range arr {
		tool, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if toString(tool["type"]) != "function" {
			continue
		}
		if _, ok := tool["function"]; ok {
			result = append(result, tool)
		} else {
			result = append(result, J{
				"type": "function",
				"function": J{
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  orDefault(tool["parameters"], J{"type": "object", "properties": J{}}),
				},
			})
		}
	}
	return result
}

func convertCCToolsToResponses(tools interface{}) []J {
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
					"type":        "function",
					"name":        fn["name"],
					"description": fn["description"],
					"parameters":  orDefault(fn["parameters"], J{"type": "object", "properties": J{}}),
				})
			} else {
				result = append(result, tool)
			}
		}
	}
	return result
}

func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += sep + p
	}
	return result
}

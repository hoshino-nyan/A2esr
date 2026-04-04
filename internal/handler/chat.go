package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/adapter"
	"github.com/hoshino-nyan/A2esr/internal/config"
	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/middleware"
	"github.com/hoshino-nyan/A2esr/internal/models"
	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

type J = map[string]interface{}

// 请求体最大限制 32MB（防止超大请求耗尽内存）
const maxRequestBodySize = 32 * 1024 * 1024

func ChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求失败", "invalid_request")
		return
	}
	if len(body) >= maxRequestBodySize {
		writeError(w, http.StatusRequestEntityTooLarge, "请求体超过大小限制", "invalid_request")
		return
	}
	var payload J
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败", "invalid_request")
		return
	}

	// 捕获请求信息
	capture := &models.RequestCapture{
		Headers:     marshalHeaders(r),
		Body:        string(body),
		Prompt:      extractSystemPrompt(payload),
		UserMessage: extractLastUserMessage(payload),
	}

	clientModel := str(payload["model"])
	isStream, _ := payload["stream"].(bool)
	startTime := time.Now()

	ctx, err := buildRouteContext(clientModel, "chat", isStream)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), "channel_error")
		return
	}

	user := middleware.GetUser(r.Context())
	apiKey := middleware.GetAPIKey(r.Context())
	ctx.ClientIP = extractClientIP(r)
	if user != nil {
		ctx.UserID = user.ID
	}
	if apiKey != nil {
		ctx.APIKeyID = apiKey.ID
		ctx.AllowedChannelIDs = apiKey.ChannelIDs
		if apiKey.ChannelIDs != "" {
			channel, upstreamModel, selErr := database.SelectChannel(clientModel, "chat", apiKey.ChannelIDs)
			if selErr != nil {
				writeError(w, http.StatusBadGateway, selErr.Error(), "channel_error")
				return
			}
			ctx.ChannelID = channel.ID
			ctx.UpstreamModel = upstreamModel
			ctx.Backend = channel.Type
			if ctx.Backend == "" || ctx.Backend == "auto" {
				ctx.Backend = autoDetectBackend(upstreamModel)
			}
			ctx.TargetURL = channel.BaseURL
			ctx.APIKey = channel.APIKey
			ctx.CustomInstructions = channel.CustomInstructions
			ctx.InstructionsPosition = channel.InstructionsPosition
			ctx.BodyModifications = database.ParseJSONMap(channel.BodyModifications)
			ctx.HeaderModifications = database.ParseJSONMap(channel.HeaderModifications)
			ctx.Timeout = channel.Timeout
		}
	}

	dbg("聊天补全", "模型=%s 上游=%s 后端=%s 流式=%v 渠道=%d", clientModel, ctx.UpstreamModel, ctx.Backend, isStream, ctx.ChannelID)

	ctx.Capture = capture

	if msgs := adapter.ToSlicePublic(payload["messages"]); len(msgs) == 0 && payload["input"] != nil {
		payload = adapter.ResponsesToCC(payload)
	}

	if isStream {
		dispatchCCStreamWithRetry(w, ctx, payload, clientModel, startTime)
	} else {
		dispatchCCNonStreamWithRetry(w, ctx, payload, clientModel, startTime)
	}
}

func dispatchCCNonStreamWithRetry(w http.ResponseWriter, ctx *models.RouteContext, payload J, clientModel string, start time.Time) {
	excludeIDs := []int64{}
	var lastBuf *responseBuffer
	var lastErr error

	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避等待
			time.Sleep(retryDelay(attempt, lastErr))

			newCtx := findAlternateContext(clientModel, "chat", ctx.AllowedChannelIDs, excludeIDs, false)
			if newCtx == nil {
				break
			}
			ctx = newCtx
			dbg("重试", "切换到渠道=%d 后端=%s (尝试 %d/%d)", ctx.ChannelID, ctx.Backend, attempt+1, defaultMaxRetries+1)
		}

		buf := newResponseBuffer()
		workPayload := deepCopyPayload(payload)
		workPayload["model"] = ctx.UpstreamModel

		switch ctx.Backend {
		case "openai":
			handleOpenAIBackend(buf, ctx, workPayload, start)
		case "responses":
			handleResponsesBackend(buf, ctx, workPayload, start)
		case "gemini":
			handleGeminiBackend(buf, ctx, workPayload, start)
		default:
			handleAnthropicBackend(buf, ctx, workPayload, start)
		}

		if lastBuf != nil {
			recycleResponseBuffer(lastBuf)
		}
		lastBuf = buf
		if !buf.isRetryable() {
			break
		}
		lastErr = fmt.Errorf("status %d", buf.code)
		excludeIDs = append(excludeIDs, ctx.ChannelID)
		dbg("重试", "渠道=%d 返回 %d，尝试切换", ctx.ChannelID, buf.code)
	}

	if lastBuf != nil {
		lastBuf.writeTo(w)
		recycleResponseBuffer(lastBuf)
	}
}

func dispatchCCStreamWithRetry(w http.ResponseWriter, ctx *models.RouteContext, payload J, clientModel string, start time.Time) {
	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, finalCtx, err := forwardStreamWithRetry(ctx, payload, clientModel, "chat")
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			recordLog(finalCtx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
			return
		}
		processCCStream(sw, finalCtx, resp, clientModel, start)
	})
}

func handleOpenAIBackend(w http.ResponseWriter, ctx *models.RouteContext, payload J, start time.Time) {
	payload = adapter.NormalizeRequest(payload, ctx.UpstreamModel)
	payload = injectInstructionsCC(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
	payload = proxy.ApplyBodyModifications(payload, ctx.BodyModifications)

	headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildOpenAIURL(ctx)

	if ctx.IsStream {
		handleOpenAIStream(w, ctx, payload, url, headers, start)
	} else {
		handleOpenAINonStream(w, ctx, payload, url, headers, start)
	}
}

func handleOpenAINonStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	data := adapter.FixResponse(raw)
	data["model"] = ctx.ClientModel
	logRequestDone(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleOpenAIStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			recordLog(ctx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
			return
		}
		var lastUsage J
		thinkExtractor := &adapter.ThinkTagStreamExtractor{}
		proxy.IterOpenAISSE(resp, func(chunk J) bool {
			if chunk == nil {
				sw.WriteDone()
				logStreamDone(ctx, lastUsage, start)
				return false
			}
			if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
				lastUsage = u
			}
			chunk = adapter.FixStreamChunk(chunk)
			for _, c := range thinkExtractor.ProcessChunk(chunk) {
				c["model"] = ctx.ClientModel
				sw.WriteData(c)
			}
			return true
		})
	})
}

func handleAnthropicBackend(w http.ResponseWriter, ctx *models.RouteContext, payload J, start time.Time) {
	payload["model"] = ctx.UpstreamModel
	anthropicPayload := adapter.CCToMessagesRequest(payload)
	anthropicPayload = injectInstructionsAnthropic(anthropicPayload, ctx.CustomInstructions, ctx.InstructionsPosition)
	anthropicPayload = proxy.ApplyBodyModifications(anthropicPayload, ctx.BodyModifications)

	headers := proxy.BuildAnthropicHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildAnthropicURL(ctx)

	if ctx.IsStream {
		handleAnthropicStream(w, ctx, anthropicPayload, url, headers, start)
	} else {
		handleAnthropicNonStream(w, ctx, anthropicPayload, url, headers, start)
	}
}

func handleAnthropicNonStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	data := adapter.MessagesToCCResponse(raw)
	data["model"] = ctx.ClientModel
	logRequestDone(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleAnthropicStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	converter := adapter.NewAnthropicStreamConverter()

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			recordLog(ctx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
			return
		}
		var lastUsage J
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			chunks := converter.ProcessEvent(eventType, eventData)
			for _, chunk := range chunks {
				chunk["model"] = ctx.ClientModel
				if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(chunk)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)
	})
}

func handleGeminiBackend(w http.ResponseWriter, ctx *models.RouteContext, payload J, start time.Time) {
	payload = adapter.NormalizeRequest(payload, ctx.UpstreamModel)
	payload = injectInstructionsCC(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
	geminiPayload := adapter.CCToGeminiRequest(payload)
	geminiPayload = proxy.ApplyBodyModifications(geminiPayload, ctx.BodyModifications)

	headers := proxy.BuildGeminiHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)

	if ctx.IsStream {
		url := proxy.BuildGeminiURL(ctx, true)
		handleGeminiStream(w, ctx, geminiPayload, url, headers, start)
	} else {
		url := proxy.BuildGeminiURL(ctx, false)
		handleGeminiNonStream(w, ctx, geminiPayload, url, headers, start)
	}
}

func handleGeminiNonStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	data := adapter.GeminiToCCResponse(raw)
	data["model"] = ctx.ClientModel
	logRequestDone(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleGeminiStream(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	converter := adapter.NewGeminiStreamConverter()

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			recordLog(ctx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
			return
		}
		var lastUsage J
		proxy.IterGeminiSSE(resp, func(geminiChunk J) bool {
			chunks := converter.ProcessChunk(geminiChunk)
			for _, chunk := range chunks {
				chunk["model"] = ctx.ClientModel
				if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(chunk)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)
	})
}

func handleResponsesBackend(w http.ResponseWriter, ctx *models.RouteContext, payload J, start time.Time) {
	respPayload := adapter.CCToResponsesRequest(payload)
	respPayload["model"] = ctx.UpstreamModel
	respPayload = injectInstructionsResponses(respPayload, ctx.CustomInstructions, ctx.InstructionsPosition)
	respPayload = proxy.ApplyBodyModifications(respPayload, ctx.BodyModifications)

	headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildResponsesURL(ctx)

	if ctx.IsStream {
		handleResponsesStreamForCC(w, ctx, respPayload, url, headers, start)
	} else {
		handleResponsesNonStreamForCC(w, ctx, respPayload, url, headers, start)
	}
}

func handleResponsesNonStreamForCC(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	data := adapter.ResponsesToCCResponse(raw, ctx.ClientModel)
	logRequestDone(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleResponsesStreamForCC(w http.ResponseWriter, ctx *models.RouteContext, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	converter := adapter.NewResponsesToCCStreamConverter(ctx.ClientModel)

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			recordLog(ctx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
			return
		}
		var lastUsage J
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			chunks := converter.ProcessEvent(eventType, eventData)
			for _, chunk := range chunks {
				if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(chunk)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)
	})
}

// ─── Helpers ──────────────────────────────────

func buildRouteContext(clientModel string, route string, isStream bool) (*models.RouteContext, error) {
	channel, upstreamModel, err := database.SelectChannel(clientModel, route, "")
	if err != nil {
		return nil, err
	}

	backend := channel.Type
	if backend == "" || backend == "auto" {
		backend = autoDetectBackend(upstreamModel)
	}

	return &models.RouteContext{
		ClientModel:          clientModel,
		UpstreamModel:        upstreamModel,
		Backend:              backend,
		TargetURL:            channel.BaseURL,
		APIKey:               channel.APIKey,
		IsStream:             isStream,
		ChannelID:            channel.ID,
		CustomInstructions:   channel.CustomInstructions,
		InstructionsPosition: channel.InstructionsPosition,
		BodyModifications:    database.ParseJSONMap(channel.BodyModifications),
		HeaderModifications:  database.ParseJSONMap(channel.HeaderModifications),
		Timeout:              channel.Timeout,
	}, nil
}

func autoDetectBackend(model string) string {
	lower := strings.ToLower(model)
	if strings.Contains(lower, "claude") || strings.Contains(lower, "anthropic") {
		return "anthropic"
	}
	if strings.Contains(lower, "gemini") {
		return "gemini"
	}
	return "openai"
}

func injectInstructionsCC(payload J, instructions, position string) J {
	if instructions == "" {
		return payload
	}
	messages := adapter.ToSlicePublic(payload["messages"])
	if len(messages) > 0 {
		first, ok := messages[0].(map[string]interface{})
		if ok && str(first["role"]) == "system" {
			original := str(first["content"])
			first["content"] = mergeText(instructions, original, position)
		} else {
			messages = append([]interface{}{J{"role": "system", "content": instructions}}, messages...)
			payload["messages"] = messages
		}
	} else {
		payload["messages"] = []interface{}{J{"role": "system", "content": instructions}}
	}
	return payload
}

func injectInstructionsResponses(payload J, instructions, position string) J {
	if instructions == "" {
		return payload
	}
	existing := str(payload["instructions"])
	payload["instructions"] = mergeText(instructions, existing, position)
	return payload
}

func injectInstructionsAnthropic(payload J, instructions, position string) J {
	if instructions == "" {
		return payload
	}
	existing := str(payload["system"])
	payload["system"] = mergeText(instructions, existing, position)
	return payload
}

func mergeText(custom, existing, position string) string {
	if existing == "" {
		return custom
	}
	if position == "append" {
		return existing + "\n\n" + custom
	}
	return custom + "\n\n" + existing
}

func handleForwardError(w http.ResponseWriter, ctx *models.RouteContext, err error, start time.Time) {
	if ue, ok := err.(*proxy.UpstreamError); ok {
		w.Header().Set("Content-Type", ue.Header.Get("Content-Type"))
		w.WriteHeader(ue.StatusCode)
		w.Write(ue.Body)
	} else {
		writeError(w, http.StatusBadGateway, err.Error(), "upstream_error")
	}
	database.IncrChannelUsage(ctx.ChannelID, 0, 0, true)
	recordLog(ctx, 0, 0, time.Since(start).Milliseconds(), "error", err.Error())
}

func logRequestDone(ctx *models.RouteContext, data J, start time.Time) {
	usage := adapter.ToMap(data["usage"])
	inp := adapter.ToIntPublic(usage["prompt_tokens"])
	outp := adapter.ToIntPublic(usage["completion_tokens"])
	dur := time.Since(start).Milliseconds()
	database.IncrChannelUsage(ctx.ChannelID, inp, outp, false)
	recordLog(ctx, inp, outp, dur, "success", "")
}

func logStreamDone(ctx *models.RouteContext, lastUsage J, start time.Time) {
	inp := adapter.ToIntPublic(lastUsage["prompt_tokens"])
	outp := adapter.ToIntPublic(lastUsage["completion_tokens"])
	dur := time.Since(start).Milliseconds()
	database.IncrChannelUsage(ctx.ChannelID, inp, outp, false)
	recordLog(ctx, inp, outp, dur, "success", "")
}

func recordLog(ctx *models.RouteContext, inp, outp int, dur int64, status, errMsg string) {
	go func() {
		logEntry := &models.RequestLog{
			UserID:        ctx.UserID,
			APIKeyID:      ctx.APIKeyID,
			ChannelID:     ctx.ChannelID,
			ClientModel:   ctx.ClientModel,
			UpstreamModel: ctx.UpstreamModel,
			Route:         "chat",
			Backend:       ctx.Backend,
			Stream:        ctx.IsStream,
			InputTokens:   inp,
			OutputTokens:  outp,
			Duration:      int(dur),
			Status:        status,
			ErrorMsg:      errMsg,
			ClientIP:      ctx.ClientIP,
			CreatedAt:     time.Now(),
		}
		logID := database.InsertRequestLogReturnID(logEntry)
		if logID > 0 && ctx.Capture != nil {
			_ = database.InsertRequestDetail(&models.RequestDetail{
				RequestLogID:   logID,
				RequestHeaders: ctx.Capture.Headers,
				RequestBody:    ctx.Capture.Body,
				Prompt:         ctx.Capture.Prompt,
				UserMessage:    ctx.Capture.UserMessage,
			})
		}
	}()
}

func writeError(w http.ResponseWriter, status int, message, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(J{
		"error": J{"message": message, "type": errType},
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func dbg(tag, format string, args ...interface{}) {
	if config.IsDebug() {
		log.Printf("[%s] "+format, append([]interface{}{tag}, args...)...)
	}
}

// extractClientIP 从请求中提取客户端真实 IP
func extractClientIP(r *http.Request) string {
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		if idx := strings.Index(ip, ","); idx != -1 {
			ip = ip[:idx]
		}
		return strings.TrimSpace(ip)
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// ─── 请求详情捕获辅助函数 ──────────────────────────

func marshalHeaders(r *http.Request) string {
	h := make(map[string]string)
	for k := range r.Header {
		lk := strings.ToLower(k)
		// 过滤敏感头
		if lk == "authorization" || lk == "cookie" {
			h[k] = "***"
		} else {
			h[k] = r.Header.Get(k)
		}
	}
	h["Method"] = r.Method
	h["URL"] = r.URL.String()
	h["Host"] = r.Host
	b, _ := json.Marshal(h)
	return string(b)
}

func extractSystemPrompt(payload J) string {
	messages := adapter.ToSlicePublic(payload["messages"])
	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		if str(m["role"]) == "system" || str(m["role"]) == "developer" {
			return extractContentText(m["content"])
		}
	}
	// responses 格式
	if inst, ok := payload["instructions"].(string); ok && inst != "" {
		return inst
	}
	return ""
}

func extractLastUserMessage(payload J) string {
	messages := adapter.ToSlicePublic(payload["messages"])
	lastUser := ""
	for _, msg := range messages {
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		if str(m["role"]) == "user" {
			lastUser = extractContentText(m["content"])
		}
	}
	// responses 格式的 input
	if lastUser == "" {
		if input, ok := payload["input"].(string); ok && input != "" {
			lastUser = input
		} else if inputArr, ok := payload["input"].([]interface{}); ok {
			for i := len(inputArr) - 1; i >= 0; i-- {
				if item, ok := inputArr[i].(map[string]interface{}); ok && str(item["role"]) == "user" {
					lastUser = extractContentText(item["content"])
					break
				}
			}
		}
	}
	return lastUser
}

func extractContentText(content interface{}) string {
	if s, ok := content.(string); ok {
		return s
	}
	if arr, ok := content.([]interface{}); ok {
		var parts []string
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				if str(m["type"]) == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/middleware"
	"github.com/hoshino-nyan/A2esr/internal/models"
	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

func MessagesPassthrough(w http.ResponseWriter, r *http.Request) {
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

	model := str(payload["model"])
	isStream, _ := payload["stream"].(bool)
	startTime := time.Now()

	ctx, err := buildRouteContext(model, "messages", isStream)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error(), "channel_error")
		return
	}
	ctx.Backend = "anthropic"

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
			channel, upstreamModel, selErr := database.SelectChannel(model, "messages", apiKey.ChannelIDs)
			if selErr != nil {
				writeError(w, http.StatusBadGateway, selErr.Error(), "channel_error")
				return
			}
			ctx.ChannelID = channel.ID
			ctx.UpstreamModel = upstreamModel
			ctx.TargetURL = channel.BaseURL
			ctx.APIKey = channel.APIKey
			ctx.CustomInstructions = channel.CustomInstructions
			ctx.InstructionsPosition = channel.InstructionsPosition
			ctx.BodyModifications = database.ParseJSONMap(channel.BodyModifications)
			ctx.HeaderModifications = database.ParseJSONMap(channel.HeaderModifications)
			ctx.Timeout = channel.Timeout
		}
	}

	dbg("透传", "模型=%s 流式=%v", model, isStream)

	ctx.Capture = capture

	payload = injectInstructionsAnthropic(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
	payload = proxy.ApplyBodyModifications(payload, ctx.BodyModifications)

	headers := proxy.BuildAnthropicHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := strings.TrimRight(ctx.TargetURL, "/") + "/v1/messages"

	if !isStream {
		payload["stream"] = false
		resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
		if err != nil {
			handleForwardError(w, ctx, err, startTime)
			return
		}
		defer resp.Body.Close()
		var data J
		json.NewDecoder(resp.Body).Decode(&data)
		injectThinking(data)
		dur := time.Since(startTime).Milliseconds()
		go func() {
			database.IncrChannelUsage(ctx.ChannelID, 0, 0, false)
			logID := database.InsertRequestLogReturnID(&models.RequestLog{
				UserID: ctx.UserID, APIKeyID: ctx.APIKeyID, ChannelID: ctx.ChannelID,
				ClientModel: ctx.ClientModel, UpstreamModel: ctx.UpstreamModel,
				Route: "messages", Backend: "anthropic", Stream: false,
				Duration: int(dur), Status: "success", ClientIP: ctx.ClientIP, CreatedAt: time.Now(),
			})
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
		writeJSON(w, http.StatusOK, data)
		return
	}

	payload["stream"] = true
	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteData(J{"error": J{"message": err.Error(), "type": "upstream_error"}})
			return
		}
		defer resp.Body.Close()
		proxy.IterRawLines(resp, func(line string) bool {
			sw.WriteRaw(line + "\n")
			return true
		})
		dur := time.Since(startTime).Milliseconds()
		go func() {
			database.IncrChannelUsage(ctx.ChannelID, 0, 0, false)
			logID := database.InsertRequestLogReturnID(&models.RequestLog{
				UserID: ctx.UserID, APIKeyID: ctx.APIKeyID, ChannelID: ctx.ChannelID,
				ClientModel: ctx.ClientModel, UpstreamModel: ctx.UpstreamModel,
				Route: "messages", Backend: "anthropic", Stream: true,
				Duration: int(dur), Status: "success", ClientIP: ctx.ClientIP, CreatedAt: time.Now(),
			})
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
	})
}

func injectThinking(data J) {
	rc, ok1 := data["reasoning_content"]
	if !ok1 {
		rc, ok1 = data["reasoningContent"]
	}
	if !ok1 || rc == nil || rc == "" {
		return
	}
	delete(data, "reasoning_content")
	delete(data, "reasoningContent")

	content, _ := data["content"].([]interface{})
	if content == nil {
		content = []interface{}{}
	}

	for _, raw := range content {
		block, ok := raw.(map[string]interface{})
		if ok && str(block["type"]) == "thinking" {
			return
		}
	}

	content = append([]interface{}{J{"type": "thinking", "thinking": rc}}, content...)
	data["content"] = content
}

package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/adapter"
	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/middleware"
	"github.com/hoshino-nyan/A2esr/internal/models"
	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

func ResponsesEndpoint(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取请求失败", "invalid_request")
		return
	}
	var payload J
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "JSON 解析失败", "invalid_request")
		return
	}

	clientModel := str(payload["model"])
	isStream, _ := payload["stream"].(bool)
	startTime := time.Now()

	ctx, err := buildRouteContext(clientModel, "responses", isStream)
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
			channel, upstreamModel, selErr := database.SelectChannel(clientModel, "responses", apiKey.ChannelIDs)
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

	dbg("响应生成", "模型=%s 上游=%s 后端=%s 流式=%v", clientModel, ctx.UpstreamModel, ctx.Backend, isStream)

	if ctx.Backend == "responses" {
		handleResponsesNative(w, ctx, payload, startTime)
		return
	}

	ccPayload := adapter.ResponsesToCC(payload)
	ccPayload["model"] = ctx.UpstreamModel
	ccPayload = injectInstructionsCC(ccPayload, ctx.CustomInstructions, ctx.InstructionsPosition)

	switch ctx.Backend {
	case "openai":
		handleResponsesViaOpenAI(w, ctx, ccPayload, startTime)
	case "gemini":
		handleResponsesViaGemini(w, ctx, ccPayload, startTime)
	default:
		handleResponsesViaAnthropic(w, ctx, ccPayload, startTime)
	}
}

func handleResponsesNative(w http.ResponseWriter, ctx *RouteCtx, payload J, start time.Time) {
	payload["model"] = ctx.UpstreamModel
	payload = injectInstructionsResponses(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
	payload = proxy.ApplyBodyModifications(payload, ctx.BodyModifications)

	headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildResponsesURL(ctx)

	if ctx.IsStream {
		handleResponsesNativeStream(w, ctx, payload, url, headers, start)
	} else {
		handleResponsesNativeNonStream(w, ctx, payload, url, headers, start)
	}
}

func handleResponsesNativeNonStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var data J
	json.NewDecoder(resp.Body).Decode(&data)
	data["model"] = ctx.ClientModel
	recordResponsesLog(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleResponsesNativeStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteEvent("error", J{"error": err.Error()})
			recordResponsesLogErr(ctx, start, err.Error())
			return
		}
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			if eventType == "response.created" || eventType == "response.completed" {
				if m, ok := eventData["model"]; ok && m != nil {
					eventData["model"] = ctx.ClientModel
				}
				if resp, ok := eventData["response"].(map[string]interface{}); ok {
					if _, ok := resp["model"]; ok {
						resp["model"] = ctx.ClientModel
					}
				}
			}
			sw.WriteEvent(eventType, eventData)
			return true
		})
		recordResponsesLog(ctx, nil, start)
	})
}

func handleResponsesViaOpenAI(w http.ResponseWriter, ctx *RouteCtx, ccPayload J, start time.Time) {
	ccPayload = adapter.NormalizeRequest(ccPayload, "")
	ccPayload = proxy.ApplyBodyModifications(ccPayload, ctx.BodyModifications)

	headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildOpenAIURL(ctx)

	if ctx.IsStream {
		handleResponsesViaOpenAIStream(w, ctx, ccPayload, url, headers, start)
	} else {
		handleResponsesViaOpenAINonStream(w, ctx, ccPayload, url, headers, start)
	}
}

func handleResponsesViaOpenAINonStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	fixed := adapter.FixResponse(raw)
	data := adapter.CCToResponses(fixed, ctx.ClientModel)
	recordResponsesLog(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleResponsesViaOpenAIStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	converter := adapter.NewResponsesStreamConverter(ctx.ClientModel)

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		for _, evt := range converter.StartEvents() {
			sw.WriteRaw(evt)
		}
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteEvent("error", J{"error": err.Error()})
			recordResponsesLogErr(ctx, start, err.Error())
			return
		}
		proxy.IterOpenAISSE(resp, func(chunk J) bool {
			if chunk == nil {
				for _, evt := range converter.Finalize() {
					sw.WriteRaw(evt)
				}
				recordResponsesLog(ctx, nil, start)
				return false
			}
			chunk = adapter.FixStreamChunk(chunk)
			for _, evt := range converter.ProcessCCChunk(chunk) {
				sw.WriteRaw(evt)
			}
			return true
		})
	})
}

func handleResponsesViaGemini(w http.ResponseWriter, ctx *RouteCtx, ccPayload J, start time.Time) {
	geminiPayload := adapter.CCToGeminiRequest(ccPayload)
	geminiPayload = proxy.ApplyBodyModifications(geminiPayload, ctx.BodyModifications)

	headers := proxy.BuildGeminiHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)

	if ctx.IsStream {
		url := proxy.BuildGeminiURL(ctx, true)
		handleResponsesViaGeminiStream(w, ctx, geminiPayload, url, headers, start)
	} else {
		url := proxy.BuildGeminiURL(ctx, false)
		handleResponsesViaGeminiNonStream(w, ctx, geminiPayload, url, headers, start)
	}
}

func handleResponsesViaGeminiNonStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	ccData := adapter.GeminiToCCResponse(raw)
	data := adapter.CCToResponses(ccData, ctx.ClientModel)
	recordResponsesLog(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleResponsesViaGeminiStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	converter := adapter.NewResponsesStreamConverter(ctx.ClientModel)
	geminiConverter := adapter.NewGeminiStreamConverter()

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		for _, evt := range converter.StartEvents() {
			sw.WriteRaw(evt)
		}
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteEvent("error", J{"error": err.Error()})
			recordResponsesLogErr(ctx, start, err.Error())
			return
		}
		proxy.IterGeminiSSE(resp, func(chunk J) bool {
			ccChunks := geminiConverter.ProcessChunk(chunk)
			for _, ccChunk := range ccChunks {
				for _, evt := range converter.ProcessCCChunk(ccChunk) {
					sw.WriteRaw(evt)
				}
			}
			return true
		})
		for _, evt := range converter.Finalize() {
			sw.WriteRaw(evt)
		}
		recordResponsesLog(ctx, nil, start)
	})
}

func handleResponsesViaAnthropic(w http.ResponseWriter, ctx *RouteCtx, ccPayload J, start time.Time) {
	anthropicPayload := adapter.CCToMessagesRequest(ccPayload)
	anthropicPayload = proxy.ApplyBodyModifications(anthropicPayload, ctx.BodyModifications)

	headers := proxy.BuildAnthropicHeaders(ctx.APIKey)
	headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
	url := proxy.BuildAnthropicURL(ctx)

	if ctx.IsStream {
		handleResponsesViaAnthropicStream(w, ctx, anthropicPayload, url, headers, start)
	} else {
		handleResponsesViaAnthropicNonStream(w, ctx, anthropicPayload, url, headers, start)
	}
}

func handleResponsesViaAnthropicNonStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = false
	resp, err := proxy.ForwardRequest(url, headers, payload, false, ctx.Timeout)
	if err != nil {
		handleForwardError(w, ctx, err, start)
		return
	}
	defer resp.Body.Close()
	var raw J
	json.NewDecoder(resp.Body).Decode(&raw)
	ccData := adapter.MessagesToCCResponse(raw)
	data := adapter.CCToResponses(ccData, ctx.ClientModel)
	recordResponsesLog(ctx, data, start)
	writeJSON(w, http.StatusOK, data)
}

func handleResponsesViaAnthropicStream(w http.ResponseWriter, ctx *RouteCtx, payload J, url string, headers map[string]string, start time.Time) {
	payload["stream"] = true
	converter := adapter.NewResponsesStreamConverter(ctx.ClientModel)

	proxy.SSEResponse(w, func(sw *proxy.SSEWriter) {
		for _, evt := range converter.StartEvents() {
			sw.WriteRaw(evt)
		}
		resp, err := proxy.ForwardRequest(url, headers, payload, true, ctx.Timeout)
		if err != nil {
			sw.WriteEvent("error", J{"error": err.Error()})
			recordResponsesLogErr(ctx, start, err.Error())
			return
		}
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			for _, evt := range converter.ProcessAnthropicEvent(eventType, eventData) {
				sw.WriteRaw(evt)
			}
			return true
		})
		for _, evt := range converter.Finalize() {
			sw.WriteRaw(evt)
		}
		recordResponsesLog(ctx, nil, start)
	})
}

type RouteCtx = models.RouteContext

func recordResponsesLog(ctx *RouteCtx, data J, start time.Time) {
	usage := adapter.ToMap(data["usage"])
	inp := adapter.ToIntPublic(usage["input_tokens"])
	outp := adapter.ToIntPublic(usage["output_tokens"])
	dur := time.Since(start).Milliseconds()
	go func() {
		database.IncrChannelUsage(ctx.ChannelID, inp, outp, false)
		_ = database.InsertRequestLog(&models.RequestLog{
			UserID: ctx.UserID, APIKeyID: ctx.APIKeyID, ChannelID: ctx.ChannelID,
			ClientModel: ctx.ClientModel, UpstreamModel: ctx.UpstreamModel,
			Route: "responses", Backend: ctx.Backend, Stream: ctx.IsStream,
			InputTokens: inp, OutputTokens: outp, Duration: int(dur),
			Status: "success", ClientIP: ctx.ClientIP, CreatedAt: time.Now(),
		})
	}()
}

func recordResponsesLogErr(ctx *RouteCtx, start time.Time, errMsg string) {
	dur := time.Since(start).Milliseconds()
	go func() {
		database.IncrChannelUsage(ctx.ChannelID, 0, 0, true)
		_ = database.InsertRequestLog(&models.RequestLog{
			UserID: ctx.UserID, APIKeyID: ctx.APIKeyID, ChannelID: ctx.ChannelID,
			ClientModel: ctx.ClientModel, UpstreamModel: ctx.UpstreamModel,
			Route: "responses", Backend: ctx.Backend, Stream: ctx.IsStream,
			Duration: int(dur), Status: "error", ErrorMsg: errMsg, ClientIP: ctx.ClientIP, CreatedAt: time.Now(),
		})
	}()
}

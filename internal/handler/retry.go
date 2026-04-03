package handler

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/adapter"
	"github.com/hoshino-nyan/A2esr/internal/config"
	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/models"
	"github.com/hoshino-nyan/A2esr/internal/proxy"
)

const defaultMaxRetries = 2

func isRetryableError(err error) bool {
	if ue, ok := err.(*proxy.UpstreamError); ok {
		return ue.IsRetryable()
	}
	return false
}

func findAlternateContext(clientModel, route, allowedChannelIDs string, excludeIDs []int64, isStream bool) *models.RouteContext {
	channel, upstreamModel, err := database.SelectChannelExcluding(clientModel, route, allowedChannelIDs, excludeIDs)
	if err != nil {
		return nil
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
		AllowedChannelIDs:    allowedChannelIDs,
		CustomInstructions:   channel.CustomInstructions,
		InstructionsPosition: channel.InstructionsPosition,
		BodyModifications:    database.ParseJSONMap(channel.BodyModifications),
		HeaderModifications:  database.ParseJSONMap(channel.HeaderModifications),
		Timeout:              channel.Timeout,
	}
}

func deepCopyPayload(payload J) J {
	b, _ := json.Marshal(payload)
	var cp J
	json.Unmarshal(b, &cp)
	return cp
}

// ─── Non-Streaming Retry (ResponseBuffer) ─────

type responseBuffer struct {
	code    int
	headers http.Header
	body    bytes.Buffer
}

func newResponseBuffer() *responseBuffer {
	return &responseBuffer{headers: make(http.Header)}
}

func (b *responseBuffer) Header() http.Header     { return b.headers }
func (b *responseBuffer) WriteHeader(code int)     { b.code = code }
func (b *responseBuffer) Write(data []byte) (int, error) {
	if b.code == 0 {
		b.code = 200
	}
	return b.body.Write(data)
}

func (b *responseBuffer) writeTo(w http.ResponseWriter) {
	for k, vals := range b.headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	if b.code != 0 {
		w.WriteHeader(b.code)
	}
	w.Write(b.body.Bytes())
}

func (b *responseBuffer) isRetryable() bool {
	switch b.code {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// ─── Streaming Retry: prepareAndForward ───────

func prepareAndForwardCC(ctx *models.RouteContext, payload J, stream bool) (*http.Response, error) {
	payload["model"] = ctx.UpstreamModel

	switch ctx.Backend {
	case "openai":
		payload = adapter.NormalizeRequest(payload, ctx.UpstreamModel)
		payload = injectInstructionsCC(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
		payload = proxy.ApplyBodyModifications(payload, ctx.BodyModifications)
		headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
		headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
		url := proxy.BuildOpenAIURL(ctx)
		payload["stream"] = stream
		return proxy.ForwardRequest(url, headers, payload, stream, ctx.Timeout)

	case "gemini":
		payload = adapter.NormalizeRequest(payload, ctx.UpstreamModel)
		payload = injectInstructionsCC(payload, ctx.CustomInstructions, ctx.InstructionsPosition)
		geminiPayload := adapter.CCToGeminiRequest(payload)
		geminiPayload = proxy.ApplyBodyModifications(geminiPayload, ctx.BodyModifications)
		headers := proxy.BuildGeminiHeaders(ctx.APIKey)
		headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
		url := proxy.BuildGeminiURL(ctx, stream)
		return proxy.ForwardRequest(url, headers, geminiPayload, stream, ctx.Timeout)

	case "responses":
		respPayload := adapter.CCToResponsesRequest(payload)
		respPayload["model"] = ctx.UpstreamModel
		respPayload = injectInstructionsResponses(respPayload, ctx.CustomInstructions, ctx.InstructionsPosition)
		respPayload = proxy.ApplyBodyModifications(respPayload, ctx.BodyModifications)
		headers := proxy.BuildOpenAIHeaders(ctx.APIKey)
		headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
		url := proxy.BuildResponsesURL(ctx)
		respPayload["stream"] = stream
		return proxy.ForwardRequest(url, headers, respPayload, stream, ctx.Timeout)

	default:
		anthropicPayload := adapter.CCToMessagesRequest(payload)
		anthropicPayload = injectInstructionsAnthropic(anthropicPayload, ctx.CustomInstructions, ctx.InstructionsPosition)
		anthropicPayload = proxy.ApplyBodyModifications(anthropicPayload, ctx.BodyModifications)
		headers := proxy.BuildAnthropicHeaders(ctx.APIKey)
		headers = proxy.ApplyHeaderModifications(headers, ctx.HeaderModifications)
		url := proxy.BuildAnthropicURL(ctx)
		anthropicPayload["stream"] = stream
		return proxy.ForwardRequest(url, headers, anthropicPayload, stream, ctx.Timeout)
	}
}

func forwardStreamWithRetry(ctx *models.RouteContext, payload J, clientModel, route string) (*http.Response, *models.RouteContext, error) {
	excludeIDs := []int64{}
	currentCtx := ctx
	var lastErr error

	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		if attempt > 0 {
			newCtx := findAlternateContext(clientModel, route, ctx.AllowedChannelIDs, excludeIDs, true)
			if newCtx == nil {
				break
			}
			currentCtx = newCtx
			if config.IsDebug() {
				log.Printf("[重试] 流式请求切换到渠道=%d 后端=%s (尝试 %d/%d)",
					currentCtx.ChannelID, currentCtx.Backend, attempt+1, defaultMaxRetries+1)
			}
		}

		workPayload := deepCopyPayload(payload)
		resp, err := prepareAndForwardCC(currentCtx, workPayload, true)
		if err == nil {
			return resp, currentCtx, nil
		}

		lastErr = err
		excludeIDs = append(excludeIDs, currentCtx.ChannelID)
		database.IncrChannelUsage(currentCtx.ChannelID, 0, 0, true)

		if !isRetryableError(err) {
			break
		}
	}

	return nil, currentCtx, lastErr
}

// ─── CC Stream Processing ─────────────────────

func processCCStream(sw *proxy.SSEWriter, ctx *models.RouteContext, resp *http.Response, clientModel string, start time.Time) {
	var lastUsage J

	switch ctx.Backend {
	case "openai":
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
				c["model"] = clientModel
				sw.WriteData(c)
			}
			return true
		})

	case "gemini":
		converter := adapter.NewGeminiStreamConverter()
		proxy.IterGeminiSSE(resp, func(chunk J) bool {
			for _, cc := range converter.ProcessChunk(chunk) {
				cc["model"] = clientModel
				if u := adapter.ToMap(cc["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(cc)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)

	case "responses":
		converter := adapter.NewResponsesToCCStreamConverter(clientModel)
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			for _, chunk := range converter.ProcessEvent(eventType, eventData) {
				if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(chunk)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)

	default:
		converter := adapter.NewAnthropicStreamConverter()
		proxy.IterEventSSE(resp, func(eventType string, eventData J) bool {
			for _, chunk := range converter.ProcessEvent(eventType, eventData) {
				chunk["model"] = clientModel
				if u := adapter.ToMap(chunk["usage"]); len(u) > 0 {
					lastUsage = u
				}
				sw.WriteData(chunk)
			}
			return true
		})
		sw.WriteDone()
		logStreamDone(ctx, lastUsage, start)
	}
}

// ─── CC Non-Stream Processing ─────────────────

func processCCNonStreamResponse(ctx *models.RouteContext, raw J) J {
	switch ctx.Backend {
	case "openai":
		return adapter.FixResponse(raw)
	case "gemini":
		return adapter.GeminiToCCResponse(raw)
	case "responses":
		return adapter.ResponsesToCCResponse(raw, ctx.ClientModel)
	default:
		return adapter.MessagesToCCResponse(raw)
	}
}

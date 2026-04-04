package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/models"
)

// ─── 全局 HTTP 连接池 ──────────────────────────
// 按超时分桶复用 http.Client，避免每次请求新建连接
var (
	clientPool   = make(map[int]*http.Client)
	clientPoolMu sync.RWMutex

	// 默认 Transport：连接池化、keepAlive
	defaultTransport = &http.Transport{
		MaxIdleConns:        200,
		MaxIdleConnsPerHost: 50,
		IdleConnTimeout:     120 * time.Second,
		DisableKeepAlives:   false,
		ForceAttemptHTTP2:   true,
	}
)

// getPooledClient 按超时值获取共享的 http.Client（连接池复用）
func getPooledClient(timeoutSec int) *http.Client {
	if timeoutSec <= 0 {
		timeoutSec = 300
	}

	clientPoolMu.RLock()
	c, ok := clientPool[timeoutSec]
	clientPoolMu.RUnlock()
	if ok {
		return c
	}

	clientPoolMu.Lock()
	defer clientPoolMu.Unlock()
	// 双重检查
	if c, ok = clientPool[timeoutSec]; ok {
		return c
	}
	c = &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: defaultTransport,
	}
	clientPool[timeoutSec] = c
	return c
}

func GenID(prefix string) string {
	return fmt.Sprintf("%s%x", prefix, time.Now().UnixNano())
}

// ─── Header Builders ──────────────────────────

func BuildOpenAIHeaders(apiKey string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}
}

func BuildAnthropicHeaders(apiKey string) map[string]string {
	h := map[string]string{
		"anthropic-version": "2023-06-01",
		"Content-Type":      "application/json",
	}
	if strings.HasPrefix(apiKey, "sk-") {
		h["x-api-key"] = apiKey
	} else {
		h["Authorization"] = "Bearer " + apiKey
	}
	return h
}

func BuildGeminiHeaders(apiKey string) map[string]string {
	h := map[string]string{
		"Content-Type": "application/json",
	}
	if strings.HasPrefix(apiKey, "AIza") {
		h["x-goog-api-key"] = apiKey
	} else {
		h["Authorization"] = "Bearer " + apiKey
	}
	return h
}

func ApplyHeaderModifications(headers map[string]string, mods map[string]interface{}) map[string]string {
	if len(mods) == 0 {
		return headers
	}
	for k, v := range mods {
		if v == nil {
			delete(headers, k)
		} else {
			headers[k] = fmt.Sprintf("%v", v)
		}
	}
	return headers
}

func ApplyBodyModifications(payload map[string]interface{}, mods map[string]interface{}) map[string]interface{} {
	if len(mods) == 0 {
		return payload
	}
	for k, v := range mods {
		if v == nil {
			delete(payload, k)
		} else {
			payload[k] = v
		}
	}
	return payload
}

// ─── Target URL Builders ──────────────────────

func BuildOpenAIURL(ctx *models.RouteContext) string {
	return strings.TrimRight(ctx.TargetURL, "/") + "/v1/chat/completions"
}

func BuildResponsesURL(ctx *models.RouteContext) string {
	return strings.TrimRight(ctx.TargetURL, "/") + "/v1/responses"
}

func BuildAnthropicURL(ctx *models.RouteContext) string {
	return strings.TrimRight(ctx.TargetURL, "/") + "/v1/messages"
}

func BuildGeminiURL(ctx *models.RouteContext, stream bool) string {
	base := strings.TrimRight(ctx.TargetURL, "/")
	if stream {
		return fmt.Sprintf("%s/v1/models/%s:streamGenerateContent?alt=sse", base, ctx.UpstreamModel)
	}
	return fmt.Sprintf("%s/v1/models/%s:generateContent", base, ctx.UpstreamModel)
}

// ─── Forward Request ──────────────────────────

func ForwardRequest(url string, headers map[string]string, payload interface{}, stream bool, timeout int) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := getPooledClient(timeout)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[Proxy] 上游返回 %d: %s", resp.StatusCode, string(respBody)[:min(300, len(respBody))])
		return nil, &UpstreamError{
			StatusCode: resp.StatusCode,
			Body:       respBody,
			Header:     resp.Header,
			RetryAfter: ParseRetryAfter(resp.Header),
		}
	}
	return resp, nil
}

type UpstreamError struct {
	StatusCode int
	Body       []byte
	Header     http.Header
	RetryAfter time.Duration // 从 Retry-After 头解析
}

func (e *UpstreamError) Error() string {
	if len(e.Body) > 0 {
		msg := string(e.Body)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Sprintf("上游错误 %d: %s", e.StatusCode, msg)
	}
	return fmt.Sprintf("上游错误 %d", e.StatusCode)
}

func (e *UpstreamError) IsRetryable() bool {
	switch e.StatusCode {
	case 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// ParseRetryAfter 从响应头提取 Retry-After 值
func ParseRetryAfter(h http.Header) time.Duration {
	ra := h.Get("Retry-After")
	if ra == "" {
		return 0
	}
	// 尝试秒数
	var secs int
	if _, err := fmt.Sscanf(ra, "%d", &secs); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	// 尝试 HTTP-date
	if t, err := http.ParseTime(ra); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}

// ─── SSE Helpers ──────────────────────────────

func SSEResponse(w http.ResponseWriter, generator func(w *SSEWriter)) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	sw := &SSEWriter{w: w, f: flusher, buf: bufio.NewWriterSize(w, 4096)}
	generator(sw)
}

type SSEWriter struct {
	w   http.ResponseWriter
	f   http.Flusher
	buf *bufio.Writer
}

func (s *SSEWriter) WriteData(data interface{}) {
	var payload string
	switch v := data.(type) {
	case string:
		payload = v
	default:
		b, _ := json.Marshal(data)
		payload = string(b)
	}
	fmt.Fprintf(s.buf, "data: %s\n\n", payload)
	s.buf.Flush()
	s.f.Flush()
}

func (s *SSEWriter) WriteEvent(eventType string, data interface{}) {
	var payload string
	switch v := data.(type) {
	case string:
		payload = v
	default:
		b, _ := json.Marshal(data)
		payload = string(b)
	}
	fmt.Fprintf(s.buf, "event: %s\ndata: %s\n\n", eventType, payload)
	s.buf.Flush()
	s.f.Flush()
}

func (s *SSEWriter) WriteDone() {
	fmt.Fprintf(s.buf, "data: [DONE]\n\n")
	s.buf.Flush()
	s.f.Flush()
}

func (s *SSEWriter) WriteRaw(raw string) {
	fmt.Fprint(s.buf, raw)
	s.buf.Flush()
	s.f.Flush()
}

// ─── SSE Parsers ──────────────────────────────

func IterOpenAISSE(resp *http.Response, fn func(chunk map[string]interface{}) bool) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			fn(nil)
			return
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if !fn(chunk) {
			return
		}
	}
}

func IterEventSSE(resp *http.Response, fn func(eventType string, data map[string]interface{}) bool) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	eventType := ""
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			eventType = ""
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(line[6:])
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(line[5:])
			if data == "" {
				continue
			}
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				continue
			}
			if !fn(eventType, parsed) {
				return
			}
		}
	}
}

func IterGeminiSSE(resp *http.Response, fn func(chunk map[string]interface{}) bool) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if !fn(chunk) {
			return
		}
	}
}

func IterRawLines(resp *http.Response, fn func(line string) bool) {
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if !fn(scanner.Text()) {
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

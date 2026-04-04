package models

import "time"

type User struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Password  string    `json:"-"`
	Role      string    `json:"role"` // admin, user
	Status    int       `json:"status"` // 1=active 0=disabled
	QPM       int       `json:"qpm"` // queries per minute, 0=unlimited
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type APIKey struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Remark        string     `json:"remark"`
	Status        int        `json:"status"` // 1=active 0=disabled
	QPM           int        `json:"qpm"`
	ChannelIDs    string     `json:"channel_ids"` // comma-separated, empty=all
	RequestCount  int64      `json:"request_count"`
	SuccessCount  int64      `json:"success_count"`
	ErrorCount    int64      `json:"error_count"`
	InputTokens   int64      `json:"input_tokens"`
	OutputTokens  int64      `json:"output_tokens"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type Channel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // openai, anthropic, gemini, responses
	BaseURL   string    `json:"base_url"`
	APIKey    string    `json:"api_key"`
	Models    string    `json:"models"` // comma-separated model IDs
	Status    int       `json:"status"` // 1=active 0=disabled
	Priority  int       `json:"priority"`
	Weight    int       `json:"weight"`
	QPM       int       `json:"qpm"`
	Timeout   int       `json:"timeout"` // seconds
	MaxRetry  int       `json:"max_retry"`

	CustomInstructions    string `json:"custom_instructions"`
	InstructionsPosition  string `json:"instructions_position"` // prepend, append
	BodyModifications     string `json:"body_modifications"`    // JSON
	HeaderModifications   string `json:"header_modifications"`  // JSON

	UsedCount    int64     `json:"used_count"`
	FailCount    int64     `json:"fail_count"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ModelMapping struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Route         string `json:"route"` // all, chat, messages, responses
	Priority      int    `json:"priority"`
	ClientModel   string `json:"client_model"`
	UpstreamModel string `json:"upstream_model"`
	ChannelIDs    string `json:"channel_ids"` // comma-separated, empty=all
	Status        int    `json:"status"`      // 1=active 0=disabled

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RequestLog struct {
	ID            int64     `json:"id"`
	UserID        int64     `json:"user_id"`
	APIKeyID      int64     `json:"api_key_id"`
	ChannelID     int64     `json:"channel_id"`
	ClientModel   string    `json:"client_model"`
	UpstreamModel string    `json:"upstream_model"`
	Route         string    `json:"route"`
	Backend       string    `json:"backend"`
	Stream        bool      `json:"stream"`
	InputTokens   int       `json:"input_tokens"`
	OutputTokens  int       `json:"output_tokens"`
	Duration      int       `json:"duration_ms"`
	Status        string    `json:"status"` // success, error
	ErrorMsg      string    `json:"error_msg"`
	ClientIP      string    `json:"client_ip"`
	CreatedAt     time.Time `json:"created_at"`
}

// RequestDetail 完整请求详情记录（最多保留200条）
type RequestDetail struct {
	ID             int64     `json:"id"`
	RequestLogID   int64     `json:"request_log_id"`
	RequestHeaders string    `json:"request_headers"` // JSON 格式的请求头
	RequestBody    string    `json:"request_body"`    // 原始请求体
	Prompt         string    `json:"prompt"`          // 提取的提示词/用户消息
	UserMessage    string    `json:"user_message"`    // 用户发送的内容摘要
	AIResponse     string    `json:"ai_response"`    // AI 生成的响应摘要
	ToolCalls      string    `json:"tool_calls"`      // 工具调用信息 JSON
	CreatedAt      time.Time `json:"created_at"`
}

// RequestCapture 用于暂存请求详情
type RequestCapture struct {
	Headers     string // JSON 格式的请求头
	Body        string // 原始请求体
	Prompt      string // 提取的提示词/系统消息
	UserMessage string // 用户发送的内容摘要
}

type RouteContext struct {
	ClientModel          string
	UpstreamModel        string
	Backend              string
	TargetURL            string
	APIKey               string
	IsStream             bool
	ChannelID            int64
	AllowedChannelIDs    string
	UserID               int64
	APIKeyID             int64
	ClientIP             string
	CustomInstructions   string
	InstructionsPosition string
	BodyModifications    map[string]interface{}
	HeaderModifications  map[string]interface{}
	Timeout              int
	Capture              *RequestCapture // 请求详情捕获
}

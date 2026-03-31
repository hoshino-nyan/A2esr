package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/hoshino-nyan/A2esr/internal/config"
	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/models"
)

type J = map[string]interface{}

func Login(w http.ResponseWriter, r *http.Request) {
	var body J
	json.NewDecoder(r.Body).Decode(&body)

	key := str(body["key"])
	if config.C.AdminToken == "" {
		writeJSON(w, 200, J{"ok": true, "message": "未配置鉴权"})
		return
	}

	if key == config.C.AdminToken {
		writeJSON(w, 200, J{"ok": true})
		return
	}

	apiKey, err := database.GetAPIKeyByKey(key)
	if err != nil || apiKey == nil || apiKey.Status != 1 {
		writeJSON(w, 401, J{"ok": false, "message": "密钥错误"})
		return
	}
	user, err := database.GetUserByID(apiKey.UserID)
	if err != nil || user == nil || user.Role != "admin" {
		writeJSON(w, 403, J{"ok": false, "message": "需要管理员权限"})
		return
	}
	writeJSON(w, 200, J{"ok": true})
}

// ─── Users CRUD ──────────────────────────────

func ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := database.GetUsers()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, users)
}

func CreateUser(w http.ResponseWriter, r *http.Request) {
	var u models.User
	json.NewDecoder(r.Body).Decode(&u)
	if u.Username == "" {
		writeError(w, 400, "用户名不能为空")
		return
	}
	if u.Status == 0 {
		u.Status = 1
	}
	if u.Role == "" {
		u.Role = "user"
	}
	if err := database.CreateUser(&u); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, u)
}

func UpdateUser(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	user, err := database.GetUserByID(id)
	if err != nil || user == nil {
		writeError(w, 404, "用户不存在")
		return
	}
	var body J
	json.NewDecoder(r.Body).Decode(&body)
	if v, ok := body["username"].(string); ok {
		user.Username = v
	}
	if v, ok := body["password"].(string); ok {
		user.Password = v
	}
	if v, ok := body["role"].(string); ok {
		user.Role = v
	}
	if v, ok := body["status"].(float64); ok {
		user.Status = int(v)
	}
	if v, ok := body["qpm"].(float64); ok {
		user.QPM = int(v)
	}
	if err := database.UpdateUser(user); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, user)
}

func DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	if err := database.DeleteUser(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, J{"ok": true})
}

// ─── API Keys CRUD ──────────────────────────

func ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := database.GetAPIKeys()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, keys)
}

func GenerateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body J
	json.NewDecoder(r.Body).Decode(&body)
	k := models.APIKey{
		UserID:     1,
		Key:        generateKey(),
		Name:       str(body["name"]),
		Remark:     str(body["remark"]),
		Status:     1,
		QPM:        0,
		ChannelIDs: str(body["channel_ids"]),
	}
	if k.Name == "" {
		k.Name = "自动生成密钥"
	}
	if v, ok := body["qpm"].(float64); ok {
		k.QPM = int(v)
	}
	if err := database.CreateAPIKey(&k); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, k)
}

func CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var k models.APIKey
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &k)
	if k.Key == "" {
		k.Key = generateKey()
	}
	if k.Name == "" {
		k.Name = "自动生成密钥"
	}
	if k.UserID == 0 {
		k.UserID = 1
	}
	if k.Status == 0 {
		k.Status = 1
	}
	if err := database.CreateAPIKey(&k); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, k)
}

func UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	keys, _ := database.GetAPIKeys()
	var key *models.APIKey
	for _, k := range keys {
		if k.ID == id {
			key = &k
			break
		}
	}
	if key == nil {
		writeError(w, 404, "密钥不存在")
		return
	}
	var body J
	json.NewDecoder(r.Body).Decode(&body)
	if v, ok := body["name"].(string); ok {
		key.Name = v
	}
	if v, ok := body["remark"].(string); ok {
		key.Remark = v
	}
	if v, ok := body["status"].(float64); ok {
		key.Status = int(v)
	}
	if v, ok := body["qpm"].(float64); ok {
		key.QPM = int(v)
	}
	if v, ok := body["channel_ids"].(string); ok {
		key.ChannelIDs = v
	}
	if err := database.UpdateAPIKey(key); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, key)
}

func ToggleAPIKey(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	keys, _ := database.GetAPIKeys()
	var key *models.APIKey
	for _, k := range keys {
		if k.ID == id {
			key = &k
			break
		}
	}
	if key == nil {
		writeError(w, 404, "密钥不存在")
		return
	}
	if key.Status == 1 {
		key.Status = 0
	} else {
		key.Status = 1
	}
	if err := database.UpdateAPIKey(key); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, key)
}

func DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	if err := database.DeleteAPIKey(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, J{"ok": true})
}

// ─── Channels CRUD ──────────────────────────

func ListChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := database.GetChannels()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, channels)
}

func CreateChannel(w http.ResponseWriter, r *http.Request) {
	var c models.Channel
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &c)
	if c.Name == "" {
		writeError(w, 400, "渠道名称不能为空")
		return
	}
	if c.Status == 0 {
		c.Status = 1
	}
	if c.Weight == 0 {
		c.Weight = 1
	}
	if c.Timeout == 0 {
		c.Timeout = 300
	}
	if c.Type == "" {
		c.Type = "openai"
	}
	if err := database.CreateChannel(&c); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, c)
}

func UpdateChannel(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	ch, err := database.GetChannelByID(id)
	if err != nil || ch == nil {
		writeError(w, 404, "渠道不存在")
		return
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, ch)
	ch.ID = id
	if err := database.UpdateChannel(ch); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, ch)
}

func DeleteChannel(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	if err := database.DeleteChannel(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, J{"ok": true})
}

// ─── Model Mappings CRUD ──────────────────────

func ListModelMappings(w http.ResponseWriter, r *http.Request) {
	mappings, err := database.GetModelMappings()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, mappings)
}

func CreateModelMapping(w http.ResponseWriter, r *http.Request) {
	var m models.ModelMapping
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &m)
	m.Route = normalizeMappingRoute(m.Route)
	if strings.TrimSpace(m.ClientModel) == "" {
		writeError(w, 400, "客户端模型不能为空")
		return
	}
	if strings.TrimSpace(m.UpstreamModel) == "" {
		writeError(w, 400, "上游模型不能为空")
		return
	}
	if !isValidMappingRoute(m.Route) {
		writeError(w, 400, "接口类型不合法")
		return
	}
	if strings.TrimSpace(m.Name) == "" {
		m.Name = m.ClientModel + " → " + m.UpstreamModel
	}
	if m.Status == 0 {
		m.Status = 1
	}
	if err := database.CreateModelMapping(&m); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, m)
}

func UpdateModelMapping(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	mappings, _ := database.GetModelMappings()
	var mapping *models.ModelMapping
	for _, m := range mappings {
		if m.ID == id {
			mapping = &m
			break
		}
	}
	if mapping == nil {
		writeError(w, 404, "映射不存在")
		return
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, mapping)
	mapping.ID = id
	mapping.Route = normalizeMappingRoute(mapping.Route)
	if strings.TrimSpace(mapping.ClientModel) == "" {
		writeError(w, 400, "客户端模型不能为空")
		return
	}
	if strings.TrimSpace(mapping.UpstreamModel) == "" {
		writeError(w, 400, "上游模型不能为空")
		return
	}
	if !isValidMappingRoute(mapping.Route) {
		writeError(w, 400, "接口类型不合法")
		return
	}
	if strings.TrimSpace(mapping.Name) == "" {
		mapping.Name = mapping.ClientModel + " → " + mapping.UpstreamModel
	}
	if err := database.UpdateModelMapping(mapping); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, mapping)
}

func DeleteModelMapping(w http.ResponseWriter, r *http.Request) {
	id := extractID(r)
	if err := database.DeleteModelMapping(id); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, J{"ok": true})
}

// ─── Stats ──────────────────────────────────

func GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := database.GetRequestLogStats()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, stats)
}

// ─── Models List (for Cursor) ──────────────

func ListModels(w http.ResponseWriter, r *http.Request) {
	mappings, _ := database.GetModelMappings()
	var modelList []J
	seen := map[string]bool{}
	for _, m := range mappings {
		if m.Status != 1 {
			continue
		}
		if strings.TrimSpace(m.ClientModel) == "" || seen[m.ClientModel] {
			continue
		}
		seen[m.ClientModel] = true
		modelList = append(modelList, J{
			"id":       m.ClientModel,
			"object":   "model",
			"owned_by": "api2cursor",
		})
	}
	if len(modelList) == 0 {
		modelList = append(modelList, J{
			"id":       "claude-sonnet-4-5-20250929",
			"object":   "model",
			"owned_by": "anthropic",
		})
	}
	writeJSON(w, 200, J{"object": "list", "data": modelList})
}

// ─── Helpers ──────────────────────────────────

func isValidMappingRoute(route string) bool {
	switch normalizeMappingRoute(route) {
	case "all", "chat", "messages", "responses":
		return true
	default:
		return false
	}
}

func normalizeMappingRoute(route string) string {
	route = strings.TrimSpace(strings.ToLower(route))
	if route == "" {
		return "all"
	}
	return route
}

func extractID(r *http.Request) int64 {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return 0
	}
	idStr := parts[len(parts)-1]
	id, _ := strconv.ParseInt(idStr, 10, 64)
	return id
}

func generateKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "sk-" + hex.EncodeToString(b)
}

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, J{"error": msg})
}

func RegisterAdminRoutes(mux *http.ServeMux, authMw func(http.Handler) http.Handler) {
	wrap := func(handler http.HandlerFunc) http.Handler {
		return authMw(http.HandlerFunc(handler))
	}

	mux.HandleFunc("/api/admin/login", Login)
	mux.Handle("/api/admin/users", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			ListUsers(w, r)
		case "POST":
			CreateUser(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/users/", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			UpdateUser(w, r)
		case "DELETE":
			DeleteUser(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/keys", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			ListAPIKeys(w, r)
		case "POST":
			CreateAPIKey(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/keys/generate", wrap(GenerateAPIKey))
	mux.Handle("/api/admin/keys/toggle/", wrap(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			ToggleAPIKey(w, r)
			return
		}
		http.Error(w, "method not allowed", 405)
	}))
	mux.Handle("/api/admin/keys/", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			UpdateAPIKey(w, r)
		case "DELETE":
			DeleteAPIKey(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/channels", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			ListChannels(w, r)
		case "POST":
			CreateChannel(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/channels/", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			UpdateChannel(w, r)
		case "DELETE":
			DeleteChannel(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/mappings", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			ListModelMappings(w, r)
		case "POST":
			CreateModelMapping(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/mappings/", wrap(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PUT":
			UpdateModelMapping(w, r)
		case "DELETE":
			DeleteModelMapping(w, r)
		default:
			http.Error(w, "method not allowed", 405)
		}
	}))
	mux.Handle("/api/admin/stats", wrap(GetStats))
	mux.HandleFunc("/v1/models", ListModels)

	staticDir := fmt.Sprintf("%s", "static")
	fs := http.FileServer(http.Dir(staticDir))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.ServeFile(w, r, staticDir+"/admin.html")
	})
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		http.ServeFile(w, r, staticDir+"/admin.html")
	})
}

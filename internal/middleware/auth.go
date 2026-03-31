package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/hoshino-nyan/A2esr/internal/config"
	"github.com/hoshino-nyan/A2esr/internal/database"
)

type contextKey string

const (
	CtxUser   contextKey = "user"
	CtxAPIKey contextKey = "apikey"
)

func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		skip := []string{"/health", "/admin", "/static/", "/api/admin/login"}
		for _, s := range skip {
			if path == s || strings.HasPrefix(path, s) {
				next.ServeHTTP(w, r)
				return
			}
		}

		token := extractToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": map[string]interface{}{
					"message": "缺少 API 密钥",
					"type":    "authentication_error",
				},
			})
			return
		}

		apiKey, err := database.GetAPIKeyByKey(token)
		if err != nil {
			log.Printf("[Auth] 查询密钥失败: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error": map[string]interface{}{
					"message": "服务器内部错误",
					"type":    "server_error",
				},
			})
			return
		}
		if apiKey == nil || apiKey.Status != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": map[string]interface{}{
					"message": "API 密钥无效或已禁用",
					"type":    "authentication_error",
				},
			})
			return
		}

		user, err := database.GetUserByID(apiKey.UserID)
		if err != nil || user == nil || user.Status != 1 {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error": map[string]interface{}{
					"message": "用户已被禁用",
					"type":    "permission_error",
				},
			})
			return
		}

		ctx := r.Context()
		ctx = SetUser(ctx, user)
		ctx = SetAPIKey(ctx, apiKey)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func AdminAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/admin/login" {
			next.ServeHTTP(w, r)
			return
		}

		token := extractToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": "未授权",
			})
			return
		}

		if config.C.AdminToken != "" && token == config.C.AdminToken {
			next.ServeHTTP(w, r)
			return
		}

		apiKey, err := database.GetAPIKeyByKey(token)
		if err != nil || apiKey == nil || apiKey.Status != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"error": "未授权",
			})
			return
		}

		user, err := database.GetUserByID(apiKey.UserID)
		if err != nil || user == nil || user.Role != "admin" {
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error": "需要管理员权限",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return ""
}

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/hoshino-nyan/A2esr/internal/admin"
	"github.com/hoshino-nyan/A2esr/internal/config"
	"github.com/hoshino-nyan/A2esr/internal/database"
	"github.com/hoshino-nyan/A2esr/internal/handler"
	"github.com/hoshino-nyan/A2esr/internal/middleware"
)

func main() {
	if _, err := os.Stat(".env"); err == nil {
		loadDotEnv(".env")
	}

	config.Load()

	log.Println("═══════════════════════════════════════════")
	log.Println("  API 2 Cursor")
	log.Println("═══════════════════════════════════════════")

	if err := database.Init(config.C.DatabasePath); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Printf("数据库: %s", config.C.DatabasePath)

	mux := http.NewServeMux()

	// 根路径（直接访问服务名或反代根未重写时常见 404）
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"service": "api2cursor",
			"admin":   "/admin",
			"health":  "/health",
		})
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"version": "go-2.0",
		})
	})

	// API routes (with auth)
	authMux := http.NewServeMux()
	authMux.HandleFunc("/v1/chat/completions", handler.ChatCompletions)
	authMux.HandleFunc("/v1/responses", handler.ResponsesEndpoint)
	authMux.HandleFunc("/v1/messages", handler.MessagesPassthrough)

	mux.Handle("/v1/chat/completions", middleware.AuthMiddleware(authMux))
	mux.Handle("/v1/responses", middleware.AuthMiddleware(authMux))
	mux.Handle("/v1/messages", middleware.AuthMiddleware(authMux))

	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Unknown API endpoint: " + r.Method + " " + r.URL.Path,
				"type":    "invalid_request_error",
			},
		})
	})

	// Admin routes
	admin.RegisterAdminRoutes(mux, middleware.AdminAuthMiddleware)

	// Wrap with CORS
	corsHandler := middleware.CORSMiddleware(mux)

	addr := fmt.Sprintf("%s:%d", config.C.ListenAddr, config.C.Port)
	log.Printf("服务启动: http://%s", addr)
	log.Printf("管理面板: http://%s/admin", addr)
	log.Printf("调试模式: %s", config.C.DebugMode)

	if err := http.ListenAndServe(addr, corsHandler); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func loadDotEnv(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(data)) {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		idx := indexOf(line, "=")
		if idx < 0 {
			continue
		}
		key := trimSpace(line[:idx])
		val := trimSpace(line[idx+1:])
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t') {
		j--
	}
	return s[i:j]
}

func indexOf(s string, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

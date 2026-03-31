package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr   string // 监听地址，如 127.0.0.1（本机反代）或 0.0.0.0（容器内需接受转发）
	Port         int
	AdminToken   string
	DatabasePath string
	DebugMode    string // off, simple, verbose
	DataDir      string
}

var C Config

func Load() {
	C = Config{
		ListenAddr:   getEnv("LISTEN_ADDR", "127.0.0.1"),
		Port:         getIntEnv("PORT", 28473),
		AdminToken:   getEnv("ADMIN_TOKEN", ""),
		DatabasePath: getEnv("DB_PATH", "data/api2cursor.db"),
		DebugMode:    getEnv("DEBUG_MODE", "off"),
		DataDir:      getEnv("DATA_DIR", "data"),
	}

	mode := strings.ToLower(strings.TrimSpace(C.DebugMode))
	if mode != "off" && mode != "simple" && mode != "verbose" {
		C.DebugMode = "off"
	} else {
		C.DebugMode = mode
	}
}

func IsDebug() bool {
	return C.DebugMode == "simple" || C.DebugMode == "verbose"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

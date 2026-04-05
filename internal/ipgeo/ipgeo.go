package ipgeo

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	Value     string
	ExpiresAt time.Time
}

var (
	mu       sync.RWMutex
	cache    = map[string]cacheEntry{}
	client   = &http.Client{Timeout: 3 * time.Second}
	cacheTTL = 24 * time.Hour
)

type ipAPIResponse struct {
	Status  string `json:"status"`
	Country string `json:"country"`
	City    string `json:"city"`
}

func Lookup(ip string) string {
	cleanIP := normalizeIP(ip)
	if cleanIP == "" {
		return ""
	}
	if isPrivateIP(cleanIP) {
		return "本地"
	}
	if cached, ok := getCache(cleanIP); ok {
		return cached
	}
	value := lookupRemote(cleanIP)
	setCache(cleanIP, value)
	return value
}

func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(ip, "[]")
}

func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	if parsed.IsLoopback() || parsed.IsPrivate() {
		return true
	}
	return false
}

func getCache(ip string) (string, bool) {
	mu.RLock()
	entry, ok := cache[ip]
	mu.RUnlock()
	if !ok || time.Now().After(entry.ExpiresAt) {
		return "", false
	}
	return entry.Value, true
}

func setCache(ip, value string) {
	mu.Lock()
	cache[ip] = cacheEntry{Value: value, ExpiresAt: time.Now().Add(cacheTTL)}
	mu.Unlock()
}

func lookupRemote(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://ip-api.com/json/"+ip+"?fields=status,country,city", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "go-api2cursor/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ""
	}

	var data ipAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	if data.Status != "success" {
		return ""
	}
	parts := make([]string, 0, 2)
	if data.Country != "" {
		parts = append(parts, data.Country)
	}
	if data.City != "" && data.City != data.Country {
		parts = append(parts, data.City)
	}
	return strings.Join(parts, "·")
}

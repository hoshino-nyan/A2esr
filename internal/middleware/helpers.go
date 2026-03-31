package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hoshino-nyan/A2esr/internal/models"
)

func SetUser(ctx context.Context, u *models.User) context.Context {
	return context.WithValue(ctx, CtxUser, u)
}

func GetUser(ctx context.Context) *models.User {
	if u, ok := ctx.Value(CtxUser).(*models.User); ok {
		return u
	}
	return nil
}

func SetAPIKey(ctx context.Context, k *models.APIKey) context.Context {
	return context.WithValue(ctx, CtxAPIKey, k)
}

func GetAPIKey(ctx context.Context) *models.APIKey {
	if k, ok := ctx.Value(CtxAPIKey).(*models.APIKey); ok {
		return k
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

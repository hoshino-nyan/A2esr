package database

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/models"
)

// ─── 内存缓存层 ──────────────────────────────────
// 缓存渠道、模型映射、API Key 和用户，减少 SQLite 查询压力。
// 写操作（增删改）自动使缓存失效。

const cacheTTL = 30 * time.Second // 缓存生存期

// ─── 渠道缓存 ─────────────────────────────────────

type channelCache struct {
	mu        sync.RWMutex
	channels  []models.Channel
	expireAt  time.Time
}

var chCache channelCache

func getCachedChannels() ([]models.Channel, error) {
	chCache.mu.RLock()
	if time.Now().Before(chCache.expireAt) && chCache.channels != nil {
		result := chCache.channels
		chCache.mu.RUnlock()
		return result, nil
	}
	chCache.mu.RUnlock()

	chCache.mu.Lock()
	defer chCache.mu.Unlock()

	// 双重检查
	if time.Now().Before(chCache.expireAt) && chCache.channels != nil {
		return chCache.channels, nil
	}

	channels, err := getChannelsFromDB()
	if err != nil {
		return nil, err
	}
	chCache.channels = channels
	chCache.expireAt = time.Now().Add(cacheTTL)
	return channels, nil
}

func invalidateChannelCache() {
	chCache.mu.Lock()
	chCache.expireAt = time.Time{} // 立即过期
	chCache.mu.Unlock()
}

// ─── 模型映射缓存 ─────────────────────────────────

type mappingCache struct {
	mu       sync.RWMutex
	mappings []models.ModelMapping
	expireAt time.Time
}

var mmCache mappingCache

func getCachedModelMappings() ([]models.ModelMapping, error) {
	mmCache.mu.RLock()
	if time.Now().Before(mmCache.expireAt) && mmCache.mappings != nil {
		result := mmCache.mappings
		mmCache.mu.RUnlock()
		return result, nil
	}
	mmCache.mu.RUnlock()

	mmCache.mu.Lock()
	defer mmCache.mu.Unlock()

	if time.Now().Before(mmCache.expireAt) && mmCache.mappings != nil {
		return mmCache.mappings, nil
	}

	mappings, err := getModelMappingsFromDB()
	if err != nil {
		return nil, err
	}
	mmCache.mappings = mappings
	mmCache.expireAt = time.Now().Add(cacheTTL)
	return mappings, nil
}

func invalidateMappingCache() {
	mmCache.mu.Lock()
	mmCache.expireAt = time.Time{}
	mmCache.mu.Unlock()
}

// ─── API Key 缓存 ─────────────────────────────────

type apiKeyEntry struct {
	key      *models.APIKey
	expireAt time.Time
}

var (
	apiKeyCache   = make(map[string]*apiKeyEntry)
	apiKeyCacheMu sync.RWMutex
)

const apiKeyCacheTTL = 60 * time.Second

func getCachedAPIKeyByKey(key string) (*models.APIKey, error) {
	apiKeyCacheMu.RLock()
	entry, ok := apiKeyCache[key]
	if ok && time.Now().Before(entry.expireAt) {
		result := entry.key
		apiKeyCacheMu.RUnlock()
		return result, nil
	}
	apiKeyCacheMu.RUnlock()

	apiKeyCacheMu.Lock()
	defer apiKeyCacheMu.Unlock()

	// 双重检查
	entry, ok = apiKeyCache[key]
	if ok && time.Now().Before(entry.expireAt) {
		return entry.key, nil
	}

	apiKey, err := getAPIKeyByKeyFromDB(key)
	if err != nil {
		return nil, err
	}
	apiKeyCache[key] = &apiKeyEntry{key: apiKey, expireAt: time.Now().Add(apiKeyCacheTTL)}
	return apiKey, nil
}

func invalidateAPIKeyCache() {
	apiKeyCacheMu.Lock()
	apiKeyCache = make(map[string]*apiKeyEntry)
	apiKeyCacheMu.Unlock()
}

// InvalidateAPIKeyCacheByKey 使特定 key 的缓存失效
func InvalidateAPIKeyCacheByKey(key string) {
	apiKeyCacheMu.Lock()
	delete(apiKeyCache, key)
	apiKeyCacheMu.Unlock()
}

// ─── 用户缓存 ─────────────────────────────────────

type userEntry struct {
	user     *models.User
	expireAt time.Time
}

var (
	userCache   = make(map[int64]*userEntry)
	userCacheMu sync.RWMutex
)

const userCacheTTL = 60 * time.Second

func getCachedUserByID(id int64) (*models.User, error) {
	userCacheMu.RLock()
	entry, ok := userCache[id]
	if ok && time.Now().Before(entry.expireAt) {
		result := entry.user
		userCacheMu.RUnlock()
		return result, nil
	}
	userCacheMu.RUnlock()

	userCacheMu.Lock()
	defer userCacheMu.Unlock()

	entry, ok = userCache[id]
	if ok && time.Now().Before(entry.expireAt) {
		return entry.user, nil
	}

	user, err := getUserByIDFromDB(id)
	if err != nil {
		return nil, err
	}
	userCache[id] = &userEntry{user: user, expireAt: time.Now().Add(userCacheTTL)}
	return user, nil
}

func invalidateUserCache() {
	userCacheMu.Lock()
	userCache = make(map[int64]*userEntry)
	userCacheMu.Unlock()
}

// ─── 统一失效 ─────────────────────────────────────

// InvalidateAllCaches 使所有缓存失效（管理后台修改时调用）
func InvalidateAllCaches() {
	invalidateChannelCache()
	invalidateMappingCache()
	invalidateAPIKeyCache()
	invalidateUserCache()
}

// ─── 渠道用量批量刷入 ─────────────────────────────

type channelUsageDelta struct {
	inputTokens  int64
	outputTokens int64
	usedCount    int64
	failCount    int64
}

var (
	usageDeltas   = make(map[int64]*channelUsageDelta)
	usageDeltasMu sync.Mutex
	usageFlushOn  int32 // atomic flag
)

// IncrChannelUsageBatched 累积渠道用量变化，定期批量刷入
func IncrChannelUsageBatched(channelID int64, inputTokens, outputTokens int, failed bool) {
	usageDeltasMu.Lock()
	d, ok := usageDeltas[channelID]
	if !ok {
		d = &channelUsageDelta{}
		usageDeltas[channelID] = d
	}
	d.inputTokens += int64(inputTokens)
	d.outputTokens += int64(outputTokens)
	d.usedCount++
	if failed {
		d.failCount++
	}
	usageDeltasMu.Unlock()

	// 启动定时刷入
	if atomic.CompareAndSwapInt32(&usageFlushOn, 0, 1) {
		go usageFlushLoop()
	}
}

func usageFlushLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		flushUsageDeltas()
	}
}

func flushUsageDeltas() {
	usageDeltasMu.Lock()
	snapshot := usageDeltas
	usageDeltas = make(map[int64]*channelUsageDelta)
	usageDeltasMu.Unlock()

	for id, d := range snapshot {
		_, _ = db.Exec(
			`UPDATE channels SET used_count=used_count+?, fail_count=fail_count+?, input_tokens=input_tokens+?, output_tokens=output_tokens+? WHERE id=?`,
			d.usedCount, d.failCount, d.inputTokens, d.outputTokens, id,
		)
	}
}

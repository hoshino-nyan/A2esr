package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hoshino-nyan/A2esr/internal/models"
)

// ─── Users ──────────────────────────────────────

func GetUsers() ([]models.User, error) {
	rows, err := db.Query("SELECT id, username, password, role, status, qpm, created_at, updated_at FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Status, &u.QPM, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func GetUserByID(id int64) (*models.User, error) {
	u := &models.User{}
	err := db.QueryRow("SELECT id, username, password, role, status, qpm, created_at, updated_at FROM users WHERE id=?", id).
		Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.Status, &u.QPM, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func CreateUser(u *models.User) error {
	now := time.Now()
	res, err := db.Exec(
		"INSERT INTO users (username, password, role, status, qpm, created_at, updated_at) VALUES (?,?,?,?,?,?,?)",
		u.Username, u.Password, u.Role, u.Status, u.QPM, now, now,
	)
	if err != nil {
		return err
	}
	u.ID, _ = res.LastInsertId()
	u.CreatedAt = now
	u.UpdatedAt = now
	return nil
}

func UpdateUser(u *models.User) error {
	u.UpdatedAt = time.Now()
	_, err := db.Exec(
		"UPDATE users SET username=?, password=?, role=?, status=?, qpm=?, updated_at=? WHERE id=?",
		u.Username, u.Password, u.Role, u.Status, u.QPM, u.UpdatedAt, u.ID,
	)
	return err
}

func DeleteUser(id int64) error {
	_, err := db.Exec("DELETE FROM users WHERE id=?", id)
	return err
}

// ─── API Keys ──────────────────────────────────

func GetAPIKeys() ([]models.APIKey, error) {
	rows, err := db.Query("SELECT id, user_id, key, name, remark, status, qpm, channel_ids, created_at, updated_at FROM api_keys ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.Remark, &k.Status, &k.QPM, &k.ChannelIDs, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	for i := range keys {
		if err := fillAPIKeyStats(&keys[i]); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func fillAPIKeyStats(k *models.APIKey) error {
	var lastUsed sql.NullString
	err := db.QueryRow(`SELECT
		IFNULL((SELECT COUNT(*) FROM request_logs WHERE api_key_id=?), 0),
		IFNULL((SELECT COUNT(*) FROM request_logs WHERE api_key_id=? AND status='success'), 0),
		IFNULL((SELECT COUNT(*) FROM request_logs WHERE api_key_id=? AND status='error'), 0),
		IFNULL((SELECT SUM(input_tokens) FROM request_logs WHERE api_key_id=?), 0),
		IFNULL((SELECT SUM(output_tokens) FROM request_logs WHERE api_key_id=?), 0),
		(SELECT MAX(created_at) FROM request_logs WHERE api_key_id=?)`,
		k.ID, k.ID, k.ID, k.ID, k.ID, k.ID).
		Scan(&k.RequestCount, &k.SuccessCount, &k.ErrorCount, &k.InputTokens, &k.OutputTokens, &lastUsed)
	if err != nil {
		return err
	}
	if lastUsed.Valid && lastUsed.String != "" {
		t, parseErr := time.Parse("2006-01-02 15:04:05", lastUsed.String)
		if parseErr == nil {
			k.LastUsedAt = &t
		}
	}
	return nil
}

func GetAPIKeysByUser(userID int64) ([]models.APIKey, error) {
	rows, err := db.Query("SELECT id, user_id, key, name, remark, status, qpm, channel_ids, created_at, updated_at FROM api_keys WHERE user_id=? ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.Remark, &k.Status, &k.QPM, &k.ChannelIDs, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func GetAPIKeyByKey(key string) (*models.APIKey, error) {
	k := &models.APIKey{}
	err := db.QueryRow("SELECT id, user_id, key, name, remark, status, qpm, channel_ids, created_at, updated_at FROM api_keys WHERE key=?", key).
		Scan(&k.ID, &k.UserID, &k.Key, &k.Name, &k.Remark, &k.Status, &k.QPM, &k.ChannelIDs, &k.CreatedAt, &k.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

func CreateAPIKey(k *models.APIKey) error {
	now := time.Now()
	res, err := db.Exec(
		"INSERT INTO api_keys (user_id, key, name, remark, status, qpm, channel_ids, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
		k.UserID, k.Key, k.Name, k.Remark, k.Status, k.QPM, k.ChannelIDs, now, now,
	)
	if err != nil {
		return err
	}
	k.ID, _ = res.LastInsertId()
	k.CreatedAt = now
	k.UpdatedAt = now
	return nil
}

func UpdateAPIKey(k *models.APIKey) error {
	k.UpdatedAt = time.Now()
	_, err := db.Exec(
		"UPDATE api_keys SET user_id=?, key=?, name=?, remark=?, status=?, qpm=?, channel_ids=?, updated_at=? WHERE id=?",
		k.UserID, k.Key, k.Name, k.Remark, k.Status, k.QPM, k.ChannelIDs, k.UpdatedAt, k.ID,
	)
	return err
}

func DeleteAPIKey(id int64) error {
	_, err := db.Exec("DELETE FROM api_keys WHERE id=?", id)
	return err
}

// ─── Channels ──────────────────────────────────

func GetChannels() ([]models.Channel, error) {
	rows, err := db.Query(`SELECT id, name, type, base_url, api_key, models, status, priority, weight,
		qpm, timeout, max_retry, custom_instructions, instructions_position, body_modifications,
		header_modifications, used_count, fail_count, input_tokens, output_tokens, created_at, updated_at
		FROM channels ORDER BY priority DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var channels []models.Channel
	for rows.Next() {
		var c models.Channel
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &c.BaseURL, &c.APIKey, &c.Models,
			&c.Status, &c.Priority, &c.Weight, &c.QPM, &c.Timeout, &c.MaxRetry,
			&c.CustomInstructions, &c.InstructionsPosition, &c.BodyModifications,
			&c.HeaderModifications, &c.UsedCount, &c.FailCount, &c.InputTokens, &c.OutputTokens,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		channels = append(channels, c)
	}
	return channels, nil
}

func GetChannelByID(id int64) (*models.Channel, error) {
	c := &models.Channel{}
	err := db.QueryRow(`SELECT id, name, type, base_url, api_key, models, status, priority, weight,
		qpm, timeout, max_retry, custom_instructions, instructions_position, body_modifications,
		header_modifications, used_count, fail_count, input_tokens, output_tokens, created_at, updated_at
		FROM channels WHERE id=?`, id).
		Scan(&c.ID, &c.Name, &c.Type, &c.BaseURL, &c.APIKey, &c.Models,
			&c.Status, &c.Priority, &c.Weight, &c.QPM, &c.Timeout, &c.MaxRetry,
			&c.CustomInstructions, &c.InstructionsPosition, &c.BodyModifications,
			&c.HeaderModifications, &c.UsedCount, &c.FailCount, &c.InputTokens, &c.OutputTokens,
			&c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func CreateChannel(c *models.Channel) error {
	now := time.Now()
	res, err := db.Exec(`INSERT INTO channels (name, type, base_url, api_key, models, status, priority, weight,
		qpm, timeout, max_retry, custom_instructions, instructions_position, body_modifications,
		header_modifications, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.Name, c.Type, c.BaseURL, c.APIKey, c.Models, c.Status, c.Priority, c.Weight,
		c.QPM, c.Timeout, c.MaxRetry, c.CustomInstructions, c.InstructionsPosition,
		c.BodyModifications, c.HeaderModifications, now, now,
	)
	if err != nil {
		return err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt = now
	c.UpdatedAt = now
	return nil
}

func UpdateChannel(c *models.Channel) error {
	c.UpdatedAt = time.Now()
	_, err := db.Exec(`UPDATE channels SET name=?, type=?, base_url=?, api_key=?, models=?, status=?,
		priority=?, weight=?, qpm=?, timeout=?, max_retry=?, custom_instructions=?,
		instructions_position=?, body_modifications=?, header_modifications=?, updated_at=? WHERE id=?`,
		c.Name, c.Type, c.BaseURL, c.APIKey, c.Models, c.Status, c.Priority, c.Weight,
		c.QPM, c.Timeout, c.MaxRetry, c.CustomInstructions, c.InstructionsPosition,
		c.BodyModifications, c.HeaderModifications, c.UpdatedAt, c.ID,
	)
	return err
}

func DeleteChannel(id int64) error {
	_, err := db.Exec("DELETE FROM channels WHERE id=?", id)
	return err
}

func IncrChannelUsage(channelID int64, inputTokens, outputTokens int, failed bool) {
	failInc := 0
	if failed {
		failInc = 1
	}
	_, _ = db.Exec(
		"UPDATE channels SET used_count=used_count+1, fail_count=fail_count+?, input_tokens=input_tokens+?, output_tokens=output_tokens+? WHERE id=?",
		failInc, inputTokens, outputTokens, channelID,
	)
}

// ─── Model Mappings ──────────────────────────────

func GetModelMappings() ([]models.ModelMapping, error) {
	rows, err := db.Query("SELECT id, name, description, route, priority, client_model, upstream_model, channel_ids, status, created_at, updated_at FROM model_mappings ORDER BY priority DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mappings []models.ModelMapping
	for rows.Next() {
		var m models.ModelMapping
		if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.Route, &m.Priority, &m.ClientModel, &m.UpstreamModel, &m.ChannelIDs, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if strings.TrimSpace(m.Route) == "" {
			m.Route = "all"
		}
		mappings = append(mappings, m)
	}
	return mappings, nil
}

func CreateModelMapping(m *models.ModelMapping) error {
	now := time.Now()
	res, err := db.Exec(
		"INSERT INTO model_mappings (name, description, route, priority, client_model, upstream_model, channel_ids, status, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)",
		m.Name, m.Description, m.Route, m.Priority, m.ClientModel, m.UpstreamModel, m.ChannelIDs, m.Status, now, now,
	)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	m.CreatedAt = now
	m.UpdatedAt = now
	return nil
}

func UpdateModelMapping(m *models.ModelMapping) error {
	m.UpdatedAt = time.Now()
	_, err := db.Exec(
		"UPDATE model_mappings SET name=?, description=?, route=?, priority=?, client_model=?, upstream_model=?, channel_ids=?, status=?, updated_at=? WHERE id=?",
		m.Name, m.Description, m.Route, m.Priority, m.ClientModel, m.UpstreamModel, m.ChannelIDs, m.Status, m.UpdatedAt, m.ID,
	)
	return err
}

func DeleteModelMapping(id int64) error {
	_, err := db.Exec("DELETE FROM model_mappings WHERE id=?", id)
	return err
}

func GetBestModelMapping(clientModel string, route string, allowedChannelIDs string) (*models.ModelMapping, error) {
	mappings, err := GetModelMappings()
	if err != nil {
		return nil, err
	}

	allowedIDs := parseIDs(allowedChannelIDs)
	var matched []models.ModelMapping
	for _, m := range mappings {
		if m.Status != 1 {
			continue
		}
		if strings.TrimSpace(m.ClientModel) != clientModel {
			continue
		}
		if !routeMatches(m.Route, route) {
			continue
		}
		if !mappingChannelAllowed(m.ChannelIDs, allowedIDs) {
			continue
		}
		matched = append(matched, m)
	}

	if len(matched) == 0 {
		return nil, nil
	}

	best := matched[0]
	for _, m := range matched[1:] {
		if mappingScore(m, route) > mappingScore(best, route) {
			best = m
			continue
		}
		if mappingScore(m, route) == mappingScore(best, route) {
			if m.Priority > best.Priority || (m.Priority == best.Priority && m.ID < best.ID) {
				best = m
			}
		}
	}
	return &best, nil
}

// ─── Request Logs ──────────────────────────────

func InsertRequestLog(l *models.RequestLog) error {
	streamInt := 0
	if l.Stream {
		streamInt = 1
	}
	_, err := db.Exec(
		`INSERT INTO request_logs (user_id, api_key_id, channel_id, client_model, upstream_model,
		route, backend, stream, input_tokens, output_tokens, duration_ms, status, error_msg, client_ip, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.UserID, l.APIKeyID, l.ChannelID, l.ClientModel, l.UpstreamModel,
		l.Route, l.Backend, streamInt, l.InputTokens, l.OutputTokens, l.Duration,
		l.Status, l.ErrorMsg, l.ClientIP, l.CreatedAt,
	)
	return err
}

func GetRequestLogStats() (map[string]interface{}, error) {
	result := map[string]interface{}{}

	var totalReqs, successReqs, errorReqs int64
	var totalInput, totalOutput int64
	db.QueryRow(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN status='success' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status='error' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0)
		FROM request_logs`).
		Scan(&totalReqs, &successReqs, &errorReqs, &totalInput, &totalOutput)

	result["total_requests"] = totalReqs
	result["success_requests"] = successReqs
	result["error_requests"] = errorReqs
	result["total_input_tokens"] = totalInput
	result["total_output_tokens"] = totalOutput

	// 活跃用户数（不同客户端 IP 数量）
	var activeUsers int64
	db.QueryRow("SELECT COUNT(DISTINCT client_ip) FROM request_logs WHERE client_ip != ''").Scan(&activeUsers)
	result["active_users"] = activeUsers

	// 活跃密钥数（状态为启用的密钥数量）
	var activeKeys int64
	db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE status = 1").Scan(&activeKeys)
	result["active_keys"] = activeKeys

	// 活跃渠道数（状态为启用的渠道数量）
	var activeChannels int64
	db.QueryRow("SELECT COUNT(*) FROM channels WHERE status = 1").Scan(&activeChannels)
	result["active_channels"] = activeChannels

	rows, err := db.Query(`SELECT client_model, COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0)
		FROM request_logs GROUP BY client_model ORDER BY COUNT(*) DESC LIMIT 50`)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	modelStats := []map[string]interface{}{}
	for rows.Next() {
		var model string
		var count, inp, outp int64
		rows.Scan(&model, &count, &inp, &outp)
		modelStats = append(modelStats, map[string]interface{}{
			"model":         model,
			"request_count": count,
			"input_tokens":  inp,
			"output_tokens": outp,
		})
	}
	result["models"] = modelStats
	return result, nil
}

// ─── Channel Selection ──────────────────────────

func SelectChannel(clientModel string, route string, allowedChannelIDs string) (*models.Channel, string, error) {
	mapping, err := GetBestModelMapping(clientModel, route, allowedChannelIDs)
	if err != nil {
		return nil, "", err
	}

	upstreamModel := clientModel
	channelFilter := ""
	if mapping != nil {
		if mapping.UpstreamModel != "" {
			upstreamModel = mapping.UpstreamModel
		}
		channelFilter = mapping.ChannelIDs
	}

	channels, err := GetChannels()
	if err != nil {
		return nil, upstreamModel, err
	}

	var candidates []models.Channel
	filterIDs := parseIDs(channelFilter)
	allowedIDs := parseIDs(allowedChannelIDs)

	for _, ch := range channels {
		if ch.Status != 1 {
			continue
		}
		if len(allowedIDs) > 0 && !idInList(ch.ID, allowedIDs) {
			continue
		}
		if len(filterIDs) > 0 && !idInList(ch.ID, filterIDs) {
			continue
		}
		if ch.Models != "" {
			supported := false
			for _, m := range strings.Split(ch.Models, ",") {
				m = strings.TrimSpace(m)
				if m == upstreamModel || m == clientModel || m == "*" {
					supported = true
					break
				}
			}
			if !supported {
				continue
			}
		}
		candidates = append(candidates, ch)
	}

	if len(candidates) == 0 {
		return nil, upstreamModel, fmt.Errorf("no available channel for model %q", clientModel)
	}

	maxPriority := candidates[0].Priority
	var topCandidates []models.Channel
	for _, c := range candidates {
		if c.Priority >= maxPriority {
			maxPriority = c.Priority
		}
	}
	for _, c := range candidates {
		if c.Priority == maxPriority {
			topCandidates = append(topCandidates, c)
		}
	}

	if len(topCandidates) == 1 {
		return &topCandidates[0], upstreamModel, nil
	}

	totalWeight := 0
	for _, c := range topCandidates {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		totalWeight += w
	}

	pick := int(time.Now().UnixNano() % int64(totalWeight))
	for _, c := range topCandidates {
		w := c.Weight
		if w <= 0 {
			w = 1
		}
		pick -= w
		if pick < 0 {
			return &c, upstreamModel, nil
		}
	}
	return &topCandidates[0], upstreamModel, nil
}

func routeMatches(ruleRoute string, currentRoute string) bool {
	ruleRoute = strings.TrimSpace(strings.ToLower(ruleRoute))
	currentRoute = strings.TrimSpace(strings.ToLower(currentRoute))
	if ruleRoute == "" || ruleRoute == "all" {
		return true
	}
	return ruleRoute == currentRoute
}

func mappingChannelAllowed(mappingChannelIDs string, allowedIDs []int64) bool {
	mappingIDs := parseIDs(mappingChannelIDs)
	if len(mappingIDs) == 0 || len(allowedIDs) == 0 {
		return true
	}
	for _, id := range mappingIDs {
		if idInList(id, allowedIDs) {
			return true
		}
	}
	return false
}

func mappingScore(m models.ModelMapping, currentRoute string) int {
	score := 0
	if strings.EqualFold(strings.TrimSpace(m.Route), strings.TrimSpace(currentRoute)) {
		score += 1000000
	}
	score += m.Priority * 1000
	score -= int(m.ID)
	return score
}

func idInList(id int64, ids []int64) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

func parseIDs(s string) []int64 {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var ids []int64
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var id int64
		if _, err := fmt.Sscanf(p, "%d", &id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func ParseJSONMap(s string) map[string]interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil
	}
	return m
}

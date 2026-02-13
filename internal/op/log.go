package op

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/snowflake"
	"gorm.io/gorm"
)

const relayLogMaxSize = 20
const relayLogMaxSizeNoDB = 100 // 当不保存到数据库时，允许更大的缓存用于实时查询

type RelayLogListFilter struct {
	StartTime         *int
	EndTime           *int
	Status            *string
	ChannelID         *int
	Model             *string
	Keyword           *string
	HasRetry          *bool
	NormalizedKeyword string
}

var relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
var relayLogCacheLock sync.Mutex

var relayLogFlushLock sync.Mutex

var relayLogSubscribers = make(map[chan model.RelayLog]struct{})
var relayLogSubscribersLock sync.RWMutex

var relayLogStreamTokens = make(map[string]struct{})
var relayLogStreamTokensLock sync.RWMutex

func RelayLogStreamTokenCreate() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	relayLogStreamTokensLock.Lock()
	relayLogStreamTokens[token] = struct{}{}
	relayLogStreamTokensLock.Unlock()

	return token, nil
}

func RelayLogStreamTokenVerify(token string) bool {
	relayLogStreamTokensLock.RLock()
	_, ok := relayLogStreamTokens[token]
	relayLogStreamTokensLock.RUnlock()
	return ok
}

func RelayLogStreamTokenRevoke(token string) {
	relayLogStreamTokensLock.Lock()
	delete(relayLogStreamTokens, token)
	relayLogStreamTokensLock.Unlock()
}

func RelayLogSubscribe() chan model.RelayLog {
	ch := make(chan model.RelayLog, 10)
	relayLogSubscribersLock.Lock()
	relayLogSubscribers[ch] = struct{}{}
	relayLogSubscribersLock.Unlock()
	return ch
}

func RelayLogUnsubscribe(ch chan model.RelayLog) {
	relayLogSubscribersLock.Lock()
	delete(relayLogSubscribers, ch)
	relayLogSubscribersLock.Unlock()
	close(ch)
}

func notifySubscribers(relayLog model.RelayLog) {
	relayLogSubscribersLock.RLock()
	defer relayLogSubscribersLock.RUnlock()

	for ch := range relayLogSubscribers {
		select {
		case ch <- relayLog:
		default:
		}
	}
}

func relayLogFlushToDB(ctx context.Context) error {
	relayLogFlushLock.Lock()
	defer relayLogFlushLock.Unlock()

	relayLogCacheLock.Lock()
	if len(relayLogCache) == 0 {
		relayLogCacheLock.Unlock()
		return nil
	}
	batch := make([]model.RelayLog, len(relayLogCache))
	copy(batch, relayLogCache)
	flushedUpto := len(batch)
	relayLogCacheLock.Unlock()

	result := db.GetDB().WithContext(ctx).Create(&batch)
	if result.Error != nil {
		return result.Error
	}

	relayLogCacheLock.Lock()
	if len(relayLogCache) >= flushedUpto {
		relayLogCache = relayLogCache[flushedUpto:]
	} else {
		relayLogCache = relayLogCache[:0]
	}
	if len(relayLogCache) == 0 {
		relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	}
	relayLogCacheLock.Unlock()

	return nil
}

func RelayLogAdd(ctx context.Context, relayLog model.RelayLog) error {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}
	maxSize := relayLogMaxSize
	if !enabled {
		maxSize = relayLogMaxSizeNoDB
	}
	relayLog.ID = snowflake.GenerateID()
	go notifySubscribers(relayLog)

	relayLogCacheLock.Lock()
	relayLogCache = append(relayLogCache, relayLog)
	if len(relayLogCache) >= maxSize {
		if enabled {
			relayLogCacheLock.Unlock()
			return relayLogFlushToDB(ctx)
		}
		// 如果未启用日志保存，移除最旧的日志，保留最新的日志用于实时查询
		keepSize := maxSize / 2
		if len(relayLogCache) > keepSize {
			relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
		}
	}
	relayLogCacheLock.Unlock()
	return nil
}

func RelayLogSaveDBTask(ctx context.Context) error {
	log.Debugf("relay log save db task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("relay log save db task finished, save time: %s", time.Since(startTime))
	}()
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return err
	}

	if enabled {
		if err := relayLogFlushToDB(ctx); err != nil {
			return err
		}
		return relayLogCleanup(ctx)
	}

	// 如果未启用日志保存，检查缓存大小，如果超过限制则清理旧日志
	relayLogCacheLock.Lock()
	if len(relayLogCache) > relayLogMaxSizeNoDB {
		keepSize := relayLogMaxSizeNoDB / 2
		relayLogCache = relayLogCache[len(relayLogCache)-keepSize:]
	}
	relayLogCacheLock.Unlock()

	return nil
}

func relayLogCleanup(ctx context.Context) error {
	keepPeriod, err := SettingGetInt(model.SettingKeyRelayLogKeepPeriod)
	if err != nil {
		return err
	}

	if keepPeriod <= 0 {
		return nil
	}

	cutoffTime := time.Now().Add(-time.Duration(keepPeriod) * 24 * time.Hour).Unix()
	return db.GetDB().WithContext(ctx).Where("time < ?", cutoffTime).Delete(&model.RelayLog{}).Error
}

func normalizeRelayLogListFilter(filter RelayLogListFilter) RelayLogListFilter {
	normalized := filter
	if normalized.StartTime != nil && normalized.EndTime != nil && *normalized.StartTime > *normalized.EndTime {
		normalized.StartTime, normalized.EndTime = normalized.EndTime, normalized.StartTime
	}
	if normalized.Keyword != nil {
		trimmed := strings.TrimSpace(*normalized.Keyword)
		if trimmed == "" {
			normalized.Keyword = nil
		} else {
			normalized.NormalizedKeyword = strings.ToLower(trimmed)
			normalized.Keyword = &trimmed
		}
	}
	if normalized.Model != nil {
		trimmed := strings.TrimSpace(*normalized.Model)
		if trimmed == "" {
			normalized.Model = nil
		} else {
			normalized.Model = &trimmed
		}
	}
	if normalized.Status != nil {
		trimmed := strings.TrimSpace(*normalized.Status)
		if trimmed == "" {
			normalized.Status = nil
		} else {
			normalized.Status = &trimmed
		}
	}
	return normalized
}

func filterRelayLog(logEntry model.RelayLog, filter RelayLogListFilter) bool {
	if filter.StartTime != nil {
		if logEntry.Time < int64(*filter.StartTime) {
			return false
		}
	}
	if filter.EndTime != nil {
		if logEntry.Time > int64(*filter.EndTime) {
			return false
		}
	}
	if filter.Status != nil {
		expectSuccess := *filter.Status == "success"
		if expectSuccess {
			if logEntry.Error != "" {
				return false
			}
		} else if logEntry.Error == "" {
			return false
		}
	}
	if filter.ChannelID != nil {
		if logEntry.ChannelId != *filter.ChannelID {
			return false
		}
	}
	if filter.Model != nil {
		if logEntry.ActualModelName != *filter.Model {
			return false
		}
	}
	if filter.HasRetry != nil {
		hasRetry := logEntry.TotalAttempts > 1
		if hasRetry != *filter.HasRetry {
			return false
		}
	}
	if filter.NormalizedKeyword != "" {
		requestContent := strings.ToLower(logEntry.RequestContent)
		responseContent := strings.ToLower(logEntry.ResponseContent)
		errorContent := strings.ToLower(logEntry.Error)
		if !strings.Contains(requestContent, filter.NormalizedKeyword) &&
			!strings.Contains(responseContent, filter.NormalizedKeyword) &&
			!strings.Contains(errorContent, filter.NormalizedKeyword) {
			return false
		}
	}
	return true
}

func escapeLikePattern(input string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"%", "\\%",
		"_", "\\_",
	)
	return replacer.Replace(input)
}

func applyRelayLogDBFilter(query *gorm.DB, filter RelayLogListFilter) *gorm.DB {
	if filter.StartTime != nil {
		query = query.Where("time >= ?", *filter.StartTime)
	}
	if filter.EndTime != nil {
		query = query.Where("time <= ?", *filter.EndTime)
	}
	if filter.Status != nil {
		if *filter.Status == "success" {
			query = query.Where("error = ''")
		} else {
			query = query.Where("error <> ''")
		}
	}
	if filter.ChannelID != nil {
		query = query.Where("channel_id = ?", *filter.ChannelID)
	}
	if filter.Model != nil {
		query = query.Where("actual_model_name = ?", *filter.Model)
	}
	if filter.HasRetry != nil {
		if *filter.HasRetry {
			query = query.Where("total_attempts > 1")
		} else {
			query = query.Where("total_attempts <= 1")
		}
	}
	if filter.NormalizedKeyword != "" {
		likeKeyword := "%" + escapeLikePattern(filter.NormalizedKeyword) + "%"
		query = query.Where(
			"LOWER(request_content) LIKE ? ESCAPE '\\' OR LOWER(response_content) LIKE ? ESCAPE '\\' OR LOWER(error) LIKE ? ESCAPE '\\'",
			likeKeyword,
			likeKeyword,
			likeKeyword,
		)
	}
	return query
}

func RelayLogList(ctx context.Context, filter RelayLogListFilter, page, pageSize int) ([]model.RelayLog, error) {
	enabled, err := SettingGetBool(model.SettingKeyRelayLogKeepEnabled)
	if err != nil {
		return nil, err
	}

	filter = normalizeRelayLogListFilter(filter)

	relayLogCacheLock.Lock()
	var cachedLogs []model.RelayLog
	for _, logEntry := range relayLogCache {
		if filterRelayLog(logEntry, filter) {
			cachedLogs = append(cachedLogs, logEntry)
		}
	}
	relayLogCacheLock.Unlock()

	for i, j := 0, len(cachedLogs)-1; i < j; i, j = i+1, j-1 {
		cachedLogs[i], cachedLogs[j] = cachedLogs[j], cachedLogs[i]
	}

	cacheCount := len(cachedLogs)
	offset := (page - 1) * pageSize

	var result []model.RelayLog
	if offset < cacheCount {
		cacheEnd := offset + pageSize
		if cacheEnd > cacheCount {
			cacheEnd = cacheCount
		}
		result = append(result, cachedLogs[offset:cacheEnd]...)
	}

	if enabled {
		remaining := pageSize - len(result)
		if remaining > 0 {
			dbOffset := 0
			if offset > cacheCount {
				dbOffset = offset - cacheCount
			}

			query := applyRelayLogDBFilter(db.GetDB().WithContext(ctx), filter)
			var dbLogs []model.RelayLog
			if err := query.Order("id DESC").Offset(dbOffset).Limit(remaining).Find(&dbLogs).Error; err != nil {
				return nil, err
			}
			result = append(result, dbLogs...)
		}
	}

	return result, nil
}

func RelayLogClear(ctx context.Context) error {
	relayLogCacheLock.Lock()
	relayLogCache = make([]model.RelayLog, 0, relayLogMaxSize)
	relayLogCacheLock.Unlock()
	return db.GetDB().WithContext(ctx).Where("1 = 1").Delete(&model.RelayLog{}).Error
}

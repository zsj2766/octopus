package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/log").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listLog),
		).
		AddRoute(
			router.NewRoute("/clear", http.MethodDelete).
				Handle(clearLog),
		).
		AddRoute(
			router.NewRoute("/stream-token", http.MethodGet).
				Handle(getStreamToken),
		)

	router.NewGroupRouter("/api/v1/log").
		AddRoute(
			router.NewRoute("/stream", http.MethodGet).
				Handle(streamLog),
		)
}

func parseOptionalIntQuery(c *gin.Context, key string) (*int, error) {
	value := c.Query(key)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalBoolQuery(c *gin.Context, key string) (*bool, error) {
	value := c.Query(key)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalStringQuery(c *gin.Context, key string) *string {
	value := c.Query(key)
	if value == "" {
		return nil
	}
	return &value
}

func listLog(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	startTime, err := parseOptionalIntQuery(c, "start_time")
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid start_time")
		return
	}
	endTime, err := parseOptionalIntQuery(c, "end_time")
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid end_time")
		return
	}
	channelID, err := parseOptionalIntQuery(c, "channel_id")
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid channel_id")
		return
	}
	hasRetry, err := parseOptionalBoolQuery(c, "has_retry")
	if err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid has_retry")
		return
	}
	status := parseOptionalStringQuery(c, "status")
	if status != nil && *status != "success" && *status != "failed" {
		resp.Error(c, http.StatusBadRequest, "invalid status")
		return
	}

	filter := op.RelayLogListFilter{
		StartTime: startTime,
		EndTime:   endTime,
		Status:    status,
		ChannelID: channelID,
		Model:     parseOptionalStringQuery(c, "model"),
		Keyword:   parseOptionalStringQuery(c, "keyword"),
		HasRetry:  hasRetry,
	}

	logs, err := op.RelayLogList(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, logs)
}

func clearLog(c *gin.Context) {
	if err := op.RelayLogClear(c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func getStreamToken(c *gin.Context) {
	token, err := op.RelayLogStreamTokenCreate()
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"token": token})
}

func streamLog(c *gin.Context) {
	token := c.Query("token")
	if token == "" || !op.RelayLogStreamTokenVerify(token) {
		resp.Error(c, http.StatusUnauthorized, "invalid stream token")
		return
	}

	op.RelayLogStreamTokenRevoke(token)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	logChan := op.RelayLogSubscribe()
	defer op.RelayLogUnsubscribe(logChan)

	ctx := c.Request.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case log, ok := <-logChan:
			if !ok {
				return
			}
			data, err := json.Marshal(log)
			if err != nil {
				continue
			}
			c.Writer.Write([]byte(fmt.Sprintf("data: %s\n\n", data)))
			c.Writer.Flush()
		}
	}
}

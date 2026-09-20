package visit

import (
	"github.com/gin-gonic/gin"

	"streetlight/internal/httpx"
	"streetlight/internal/response"
)

// Handler 处理维修质量回访相关的 HTTP 请求。
type Handler struct {
	service *Service
}

// NewHandler 构造回访处理器。
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List 查询回访任务列表。
func (h *Handler) List(c *gin.Context) {
	var query ListQuery
	if err := httpx.BindQuery(c, &query); err != nil {
		response.Fail(c, err)
		return
	}
	items, total, page, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, response.NewPageData(items, total, page.Page, page.PageSize))
}

// ListByFault 查询指定故障的全部回访轮次。
func (h *Handler) ListByFault(c *gin.Context) {
	faultID, err := httpx.ParseID(c, "faultId")
	if err != nil {
		response.Fail(c, err)
		return
	}
	items, err := h.service.ListByFault(c.Request.Context(), faultID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, items)
}

// Get 查询回访任务详情。
func (h *Handler) Get(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// Contact 记录一次回访联系情况。
func (h *Handler) Contact(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	var req ContactRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := h.service.RecordContact(c.Request.Context(), id, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// Evaluate 回访评定, 不合格时自动触发返修。
func (h *Handler) Evaluate(c *gin.Context) {
	id, err := httpx.ParseID(c, "id")
	if err != nil {
		response.Fail(c, err)
		return
	}
	var req EvaluateRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		response.Fail(c, err)
		return
	}
	entity, err := h.service.Evaluate(c.Request.Context(), id, req)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, entity)
}

// Metadata 返回回访字典。
func (h *Handler) Metadata(c *gin.Context) {
	response.OK(c, h.service.Metadata())
}

// Statistics 回访与返修统计。
func (h *Handler) Statistics(c *gin.Context) {
	statistics, err := h.service.Statistics(c.Request.Context())
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, statistics)
}

package visit

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 维修质量回访模块, 负责完工后回访任务、联系记录、满意度与返修联动。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造质量回访模块, repairs 为维修记录模块提供的端口实现。
func New(db *gorm.DB, repairs RepairPort) *Module {
	repository := NewRepository(db)
	service := NewService(repository, repairs)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Service 暴露业务服务, 供 bootstrap 装配故障结算校验与维修完工钩子。
func (m *Module) Service() *Service { return m.service }

// Repository 暴露仓储, 供状态查询模块装配。
func (m *Module) Repository() *Repository { return m.repository }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "维修质量回访" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any { return []any{&Visit{}, &VisitContactLog{}} }

// RegisterRoutes 实现 module.Module 接口。
func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/visits")
	{
		group.GET("", m.handler.List)
		group.GET("/meta", m.handler.Metadata)
		group.GET("/statistics", m.handler.Statistics)
		group.GET("/fault/:faultId", m.handler.ListByFault)
		group.GET("/:id", m.handler.Get)
		group.POST("/:id/contact", m.handler.Contact)
		group.POST("/:id/evaluate", m.handler.Evaluate)
	}
}

package visit

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Module 维修质量回访模块: 完工后生成回访任务, 不合格驱动返修, 并守卫故障结算。
type Module struct {
	repository *Repository
	service    *Service
	handler    *Handler
}

// New 构造质量回访模块。
// repairs 提供维修单只读能力, factory 用于回访不合格时开出返修维修单。
func New(db *gorm.DB, repairs RepairReader, factory RepairFactory) *Module {
	repository := NewRepository(db)
	service := NewService(repository, repairs, factory)
	return &Module{
		repository: repository,
		service:    service,
		handler:    NewHandler(service),
	}
}

// Repository 暴露仓储, 供状态查询模块装配。
func (m *Module) Repository() *Repository { return m.repository }

// Service 暴露服务, 供 bootstrap 向故障/维修模块注入端口。
func (m *Module) Service() *Service { return m.service }

// Name 实现 module.Module 接口。
func (m *Module) Name() string { return "维修质量回访" }

// Models 实现 module.Module 接口。
func (m *Module) Models() []any { return []any{&Visit{}, &VisitContact{}} }

// RegisterRoutes 实现 module.Module 接口。
func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/visits")
	{
		group.GET("", m.handler.List)
		group.GET("/meta", m.handler.Metadata)
		group.GET("/statistics", m.handler.Statistics)
		group.GET("/fault/:faultId", m.handler.ListByFault)
		group.GET("/:id", m.handler.Get)
		group.POST("/:id/complete", m.handler.Complete)
	}
}

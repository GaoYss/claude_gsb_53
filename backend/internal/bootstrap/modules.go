package bootstrap

import (
	"gorm.io/gorm"

	"streetlight/internal/module"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/status"
	"streetlight/internal/modules/visit"
)

// buildModules 按依赖方向装配业务模块。
//
// 依赖关系: 路灯台账 <- 故障登记 <- 维修记录 <- 质量回访, 维修状态查询依赖各模块的只读仓储。
// 存在两处反向调用, 与既有 SetOpenFaultCounter 一样通过 setter 注入, 避免构造函数循环依赖:
//   - 维修完工 -> 生成回访任务 / 删除维修记录前的回访校验 (repair.SetVisitHooks)
//   - 故障结算 -> 回访完成情况校验 (fault.SetSettlementGuard)
func buildModules(db *gorm.DB) []module.Module {
	lampModule := lamp.New(db)

	faultModule := fault.New(db, lampModule.Service())
	lampModule.Service().SetOpenFaultCounter(faultModule.Repository())

	repairModule := repair.New(db, faultModule.Service())

	visitModule := visit.New(db, repairModule.Repository(), repairModule.Service())
	repairModule.Service().SetVisitHooks(visitModule.Service())
	faultModule.Service().SetSettlementGuard(visitModule.Service())

	statusModule := status.New(
		db,
		lampModule.Repository(),
		faultModule.Repository(),
		repairModule.Repository(),
		visitModule.Repository(),
	)

	return []module.Module{
		lampModule,
		faultModule,
		repairModule,
		visitModule,
		statusModule,
	}
}

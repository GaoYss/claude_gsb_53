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
// 其中反向调用通过 setter 注入端口, 避免构造函数循环依赖:
//   - 路灯删除前校验未闭环故障: lamp.SetOpenFaultCounter(faultRepo)
//   - 故障关闭(结算)前校验回访:   fault.SetSettlementChecker(visitService)
//   - 维修完工后生成回访任务:     repair.SetVisitHook(visitService)
func buildModules(db *gorm.DB) []module.Module {
	lampModule := lamp.New(db)

	faultModule := fault.New(db, lampModule.Service())
	lampModule.Service().SetOpenFaultCounter(faultModule.Repository())

	repairModule := repair.New(db, faultModule.Service())

	visitModule := visit.New(db, repairModule.Service())
	repairModule.Service().SetVisitHook(visitModule.Service())
	faultModule.Service().SetSettlementChecker(visitModule.Service())

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

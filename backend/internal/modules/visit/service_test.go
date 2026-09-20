package visit_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/visit"
)

type harness struct {
	lamps   *lamp.Service
	faults  *fault.Service
	repairs *repair.Service
	visits  *visit.Service
	db      *gorm.DB
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{
		Logger:         logger.Default.LogMode(logger.Silent),
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.AutoMigrate(&lamp.Lamp{}, &fault.Fault{}, &repair.Repair{}, &visit.Visit{}, &visit.VisitContactLog{}))

	lampRepository := lamp.NewRepository(db)
	lampService := lamp.NewService(lampRepository)

	faultRepository := fault.NewRepository(db)
	faultService := fault.NewService(faultRepository, lampService)
	lampService.SetOpenFaultCounter(faultRepository)

	repairRepository := repair.NewRepository(db)
	repairService := repair.NewService(repairRepository, faultService)

	visitRepository := visit.NewRepository(db)
	visitService := visit.NewService(visitRepository, repairService)
	repairService.SetVisitHook(visitService)
	faultService.SetSettlementChecker(visitService)

	return &harness{
		lamps:   lampService,
		faults:  faultService,
		repairs: repairService,
		visits:  visitService,
		db:      db,
	}
}

func (h *harness) setupFinishedFault(t *testing.T) (*lamp.Lamp, *fault.Fault, *repair.Repair) {
	t.Helper()
	ctx := context.Background()

	device, err := h.lamps.Create(ctx, lamp.CreateRequest{
		Code: "LD-V-001", Name: "测试灯杆", RoadName: "测试路", LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)

	entity, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID:     device.ID,
		FaultType:  "灯不亮",
		FaultLevel: fault.LevelHigh,
		Source:     fault.SourceCitizen,
		Description: "整灯不亮",
		Reporter:   "巡检员",
		ReportedAt: time.Now().Add(-4 * time.Hour).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)

	record, err := h.repairs.Create(ctx, repair.CreateRequest{
		FaultID:   entity.ID,
		Repairman: "维修工甲",
		StartedAt: time.Now().Add(-3 * time.Hour).Format("2006-01-02 15:04:05"),
	})
	require.NoError(t, err)

	// 指定明确的完工时间, 用于验证返修不改写原完工时间。
	finishedAt := time.Now().Add(-2 * time.Hour).Format("2006-01-02 15:04:05")
	finished, err := h.repairs.Finish(ctx, record.ID, repair.FinishRequest{
		Result: repair.ResultFixed, FinishedAt: finishedAt, Content: "首次处置: 更换驱动电源",
	})
	require.NoError(t, err)
	return device, entity, finished
}

func latestVisit(t *testing.T, h *harness, faultID uint) *visit.Visit {
	t.Helper()
	list, err := h.visits.ListByFault(context.Background(), faultID)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	return &list[len(list)-1]
}

func TestVisitAutoCreatedOnFinish(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, entity, finished := h.setupFinishedFault(t)

	task := latestVisit(t, h, entity.ID)
	require.Equal(t, visit.StatusPending, task.Status)
	require.Equal(t, 1, task.Round)
	require.Equal(t, finished.ID, task.RepairID)
	require.Regexp(t, `^HF\d{12}$`, task.VisitNo)
	require.WithinDuration(t, finished.FinishedAt.Add(visit.DefaultDeadline), task.DueAt, time.Second)

	// 非已修复结果完工不生成回访: 另一条故障以 pending_parts 完工, 不应产生回访任务
	device2, err := h.lamps.Create(ctx, lamp.CreateRequest{
		Code: "LD-V-009", RoadName: "测试路", LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	entity2, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device2.ID, FaultType: "线路故障", Description: "等待配件",
	})
	require.NoError(t, err)
	record2, err := h.repairs.Create(ctx, repair.CreateRequest{FaultID: entity2.ID, Repairman: "维修工乙"})
	require.NoError(t, err)
	_, err = h.repairs.Finish(ctx, record2.ID, repair.FinishRequest{Result: repair.ResultPendingParts})
	require.NoError(t, err)
	list2, err := h.visits.ListByFault(ctx, entity2.ID)
	require.NoError(t, err)
	require.Empty(t, list2, "非已修复完工不应生成回访任务")
}

func TestUnqualifiedTriggersReworkAndRevisit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, entity, original := h.setupFinishedFault(t)

	first := latestVisit(t, h, entity.ID)
	unqualified := false
	evaluated, err := h.visits.Evaluate(ctx, first.ID, visit.EvaluateRequest{
		ContactResult: visit.ContactConnected,
		ContactName:   "市民李先生",
		Satisfaction:  2,
		Qualified:     &unqualified,
		Content:       "维修后当晚仍然不亮",
	})
	require.NoError(t, err)
	require.Equal(t, visit.StatusUnqualified, evaluated.Status)
	require.NotNil(t, evaluated.ReworkRepairID)
	require.NotEmpty(t, evaluated.ReworkRepairNo)

	// 返修生成了一条新的维修记录, 关联原维修记录
	rework, err := h.repairs.Get(ctx, *evaluated.ReworkRepairID)
	require.NoError(t, err)
	require.NotNil(t, rework.OriginalRepairID)
	require.Equal(t, original.ID, *rework.OriginalRepairID)
	require.Equal(t, repair.StatusOngoing, rework.Status)

	// 故障从已修复回退到维修中, 维修次数累加; 路灯回到维修状态
	faultAfter, err := h.faults.GetByID(ctx, entity.ID)
	require.NoError(t, err)
	require.Equal(t, fault.StatusProcessing, faultAfter.Status)
	require.Equal(t, 2, faultAfter.RepairCount)

	// 原维修记录的完工时间与首次处置过程不被改写
	originalAgain, err := h.repairs.Get(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, repair.StatusFinished, originalAgain.Status)
	require.Equal(t, repair.ResultFixed, originalAgain.Result)
	require.True(t, original.FinishedAt.Equal(*originalAgain.FinishedAt), "原完工时间不应被返修改写")
	require.NotNil(t, original.FinishedAt)
	require.Equal(t, "首次处置: 更换驱动电源", originalAgain.Content)

	// 返修完工后自动生成第二轮回访
	_, err = h.repairs.Finish(ctx, rework.ID, repair.FinishRequest{Result: repair.ResultFixed, Content: "重新更换驱动"})
	require.NoError(t, err)
	list, err := h.visits.ListByFault(ctx, entity.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, 2, list[1].Round)
	require.Equal(t, rework.ID, list[1].RepairID)

	// 第二轮回访合格后才允许结算
	qualified := true
	_, err = h.visits.Evaluate(ctx, list[1].ID, visit.EvaluateRequest{
		ContactResult: visit.ContactConnected, Satisfaction: 5, Qualified: &qualified,
	})
	require.NoError(t, err)
	closed, err := h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "回访合格, 结算闭环"})
	require.NoError(t, err)
	require.Equal(t, fault.StatusClosed, closed.Status)

	// 返修次数进入统计
	stats, err := h.visits.Statistics(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.ReworkTotal)
	require.Equal(t, int64(1), stats.QualifiedTotal)
	require.Equal(t, int64(1), stats.UnqualifiedTotal)
}

func TestSettlementBlockedBeforeVisitCompleted(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_, entity, _ := h.setupFinishedFault(t)

	// 完工后未回访, 不允许结算
	_, err := h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "试图结算"})
	requireConflict(t, err)

	first := latestVisit(t, h, entity.ID)

	// 仅记录联系情况、未评定, 仍不允许结算
	_, err = h.visits.RecordContact(ctx, first.ID, visit.ContactRequest{
		ContactResult: visit.ContactNoAnswer, Content: "首次拨打无人接听",
	})
	require.NoError(t, err)
	_, err = h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "试图结算"})
	requireConflict(t, err)

	// 评定不合格触发返修后, 返修未重新回访合格, 仍不允许结算
	unqualified := false
	evaluated, err := h.visits.Evaluate(ctx, first.ID, visit.EvaluateRequest{
		ContactResult: visit.ContactConnected, Satisfaction: 1, Qualified: &unqualified,
	})
	require.NoError(t, err)
	_, err = h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "试图结算"})
	requireConflict(t, err)

	rework, err := h.repairs.Get(ctx, *evaluated.ReworkRepairID)
	require.NoError(t, err)
	_, err = h.repairs.Finish(ctx, rework.ID, repair.FinishRequest{Result: repair.ResultFixed})
	require.NoError(t, err)

	// 第二轮回访仍待处理, 依旧不能结算
	_, err = h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "试图结算"})
	requireConflict(t, err)
}

func TestCloseWithoutRepairAllowed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	device, err := h.lamps.Create(ctx, lamp.CreateRequest{
		Code: "LD-V-002", RoadName: "测试路", LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	entity, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "其他", Description: "误报, 现场核实无故障",
	})
	require.NoError(t, err)

	// 无已修复完工记录时(误报作废), 不强制回访即可结算
	closed, err := h.faults.Close(ctx, entity.ID, fault.CloseRequest{Remark: "误报作废"})
	require.NoError(t, err)
	require.Equal(t, fault.StatusClosed, closed.Status)
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, http.StatusConflict, businessErr.Status, "错误信息: %s", businessErr.Message)
}

package visit_test

import (
	"context"
	"net/http"
	"testing"

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

// harness 装配含质量回访钩子的完整模块链。
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

	require.NoError(t, db.AutoMigrate(&lamp.Lamp{}, &fault.Fault{}, &repair.Repair{}, &visit.Visit{}, &visit.VisitContact{}))

	lampRepository := lamp.NewRepository(db)
	lampService := lamp.NewService(lampRepository)

	faultRepository := fault.NewRepository(db)
	faultService := fault.NewService(faultRepository, lampService)
	lampService.SetOpenFaultCounter(faultRepository)

	repairRepository := repair.NewRepository(db)
	repairService := repair.NewService(repairRepository, faultService)

	visitRepository := visit.NewRepository(db)
	visitService := visit.NewService(visitRepository, repairRepository, repairService)
	repairService.SetVisitHooks(visitService)
	faultService.SetSettlementGuard(visitService)

	return &harness{
		lamps:   lampService,
		faults:  faultService,
		repairs: repairService,
		visits:  visitService,
		db:      db,
	}
}

func (h *harness) createLamp(t *testing.T, code string) *lamp.Lamp {
	t.Helper()
	entity, err := h.lamps.Create(context.Background(), lamp.CreateRequest{
		Code: code, Name: "测试灯杆", RoadName: "测试路", LampType: lamp.LampTypeLED,
	})
	require.NoError(t, err)
	return entity
}

func (h *harness) reportAndFinishFixed(t *testing.T, description string) (*fault.Fault, *repair.Repair) {
	t.Helper()
	ctx := context.Background()
	device := h.createLamp(t, "LD-V-"+description)
	target, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "灯不亮", Description: description, Reporter: "巡检员",
	})
	require.NoError(t, err)
	record, err := h.repairs.Create(ctx, repair.CreateRequest{FaultID: target.ID, Repairman: "维修工甲"})
	require.NoError(t, err)
	finished, err := h.repairs.Finish(ctx, record.ID, repair.FinishRequest{Result: repair.ResultFixed, Content: "首次处置"})
	require.NoError(t, err)
	return target, finished
}

func requireConflict(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, http.StatusConflict, businessErr.Status, "错误信息: %s", businessErr.Message)
}

func requireBadRequest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	businessErr, ok := apperr.As(err)
	require.True(t, ok, "期望业务错误, 实际: %v", err)
	require.Equal(t, http.StatusBadRequest, businessErr.Status, "错误信息: %s", businessErr.Message)
}

func satisfaction(v int) *int { return &v }

// 完工后自动生成回访任务, 未回访前不允许结算。
func TestVisitGeneratedOnFinishAndBlocksSettlement(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	target, first := h.reportAndFinishFixed(t, "001")

	visits, err := h.visits.ListByFault(ctx, target.ID)
	require.NoError(t, err)
	require.Len(t, visits, 1)
	require.Equal(t, visit.StatusPending, visits[0].Status)
	require.Equal(t, 1, visits[0].Round)
	require.Equal(t, first.ID, visits[0].RepairID)
	require.Equal(t, first.ID, visits[0].OriginRepairID, "首轮回访的原维修单即首次处置单")
	require.Regexp(t, `^HF\d{8}\d{4}$`, visits[0].VisitNo)

	// 回访未完成, 关闭故障(结算)被拦截
	_, err = h.faults.Close(ctx, target.ID, fault.CloseRequest{Remark: "尝试结算"})
	requireConflict(t, err)

	// 回访合格后允许结算
	detail, err := h.visits.Complete(ctx, visits[0].ID, visit.CompleteRequest{
		ContactStatus: visit.ContactReached,
		ContactMethod: visit.MethodPhone,
		Visitor:       "客服小王",
		Satisfaction:  satisfaction(5),
		Result:        visit.ResultQualified,
		Feedback:      "路灯恢复正常",
	})
	require.NoError(t, err)
	require.Equal(t, visit.StatusCompleted, detail.Status)
	require.Equal(t, visit.ResultQualified, detail.Result)
	require.Len(t, detail.Contacts, 1)
	require.Equal(t, 1, detail.ContactAttempts)

	closed, err := h.faults.Close(ctx, target.ID, fault.CloseRequest{Remark: "回访合格, 闭环"})
	require.NoError(t, err)
	require.Equal(t, fault.StatusClosed, closed.Status)
}

// 未联系上只登记联系情况, 回访单保持待回访。
func TestUnreachedKeepsVisitPending(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	target, _ := h.reportAndFinishFixed(t, "002")
	visits, _ := h.visits.ListByFault(ctx, target.ID)

	detail, err := h.visits.Complete(ctx, visits[0].ID, visit.CompleteRequest{
		ContactStatus: visit.ContactUnreached,
		Remark:        "拨打 3 次无人接听",
	})
	require.NoError(t, err)
	require.Equal(t, visit.StatusPending, detail.Status, "未联系上不应完成回访")
	require.Equal(t, 1, detail.ContactAttempts)
	require.NotNil(t, detail.LastContactAt)
	require.Len(t, detail.Contacts, 1)

	// 仍然不能结算
	_, err = h.faults.Close(ctx, target.ID, fault.CloseRequest{})
	requireConflict(t, err)

	// 已联系上但未给结论/满意度应被拒绝
	_, err = h.visits.Complete(ctx, visits[0].ID, visit.CompleteRequest{ContactStatus: visit.ContactReached})
	requireBadRequest(t, err)
}

// 不合格回访触发返修, 返修完工后重新回访, 首次处置不被改写。
func TestUnqualifiedTriggersReworkAndRevisit(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	target, first := h.reportAndFinishFixed(t, "003")

	firstFinishedAt := *first.FinishedAt
	firstStartedAt := first.StartedAt
	firstContent := first.Content

	visits, _ := h.visits.ListByFault(ctx, target.ID)

	// 不合格必须填写原因
	_, err := h.visits.Complete(ctx, visits[0].ID, visit.CompleteRequest{
		ContactStatus: visit.ContactReached,
		Satisfaction:  satisfaction(2),
		Result:        visit.ResultUnqualified,
	})
	requireBadRequest(t, err)

	// 判定不合格, 触发返修
	detail, err := h.visits.Complete(ctx, visits[0].ID, visit.CompleteRequest{
		ContactStatus:     visit.ContactReached,
		Satisfaction:      satisfaction(2),
		Result:            visit.ResultUnqualified,
		UnqualifiedReason: "维修后次日再次熄灭",
	})
	require.NoError(t, err)
	require.Equal(t, visit.ResultUnqualified, detail.Result)
	require.NotNil(t, detail.ReworkRepairID)

	// 故障回到维修中, 维修次数累加
	faultAfterRework, err := h.faults.GetByID(ctx, target.ID)
	require.NoError(t, err)
	require.Equal(t, fault.StatusProcessing, faultAfterRework.Status)
	require.Equal(t, 2, faultAfterRework.RepairCount)

	// 返修单已生成且处于进行中, 关联同一条故障
	reworkID := *detail.ReworkRepairID
	require.NotEqual(t, first.ID, reworkID)
	reworkRepair, err := h.repairs.Get(ctx, reworkID)
	require.NoError(t, err)
	require.Equal(t, repair.StatusOngoing, reworkRepair.Status)
	require.Equal(t, target.ID, reworkRepair.FaultID)
	require.Equal(t, "维修工甲", reworkRepair.Repairman, "未指定返修人时默认派给原维修人员")

	// 首次处置记录与原完工时间保持不变
	firstAgain, err := h.repairs.Get(ctx, first.ID)
	require.NoError(t, err)
	require.True(t, firstStartedAt.Equal(firstAgain.StartedAt), "首次开工时间不应被改写")
	require.True(t, firstFinishedAt.Equal(*firstAgain.FinishedAt), "原完工时间不应被返修改写")
	require.Equal(t, firstContent, firstAgain.Content)
	require.Equal(t, repair.StatusFinished, firstAgain.Status)

	// 返修未完工前不允许结算
	_, err = h.faults.Close(ctx, target.ID, fault.CloseRequest{})
	requireConflict(t, err)

	// 返修完工(已修复) -> 生成第 2 轮回访
	_, err = h.repairs.Finish(ctx, reworkID, repair.FinishRequest{Result: repair.ResultFixed, Content: "重新接线并复测"})
	require.NoError(t, err)

	visits2, err := h.visits.ListByFault(ctx, target.ID)
	require.NoError(t, err)
	require.Len(t, visits2, 2)
	require.Equal(t, 2, visits2[1].Round, "返修完工后应生成新一轮回访")
	require.Equal(t, reworkID, visits2[1].RepairID)
	require.Equal(t, first.ID, visits2[1].OriginRepairID, "再回访仍关联首次处置的原维修记录")
	require.Equal(t, visit.StatusPending, visits2[1].Status)

	// 第二轮回访合格
	_, err = h.visits.Complete(ctx, visits2[1].ID, visit.CompleteRequest{
		ContactStatus: visit.ContactReached,
		Satisfaction:  satisfaction(4),
		Result:        visit.ResultQualified,
	})
	require.NoError(t, err)

	// 此时允许结算
	_, err = h.faults.Close(ctx, target.ID, fault.CloseRequest{Remark: "返修后回访合格"})
	require.NoError(t, err)

	// 返修次数进入统计
	stats, err := h.visits.Statistics(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), stats.ReworkTotal)
	require.Equal(t, int64(1), stats.UnqualifiedTotal)
	require.Equal(t, int64(1), stats.QualifiedTotal)
}

// 非"已修复"完工不生成回访, 且故障因末次维修非修复而无法结算。
func TestNonFixedFinishNoVisitAndCannotSettle(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	device := h.createLamp(t, "LD-V-004")
	target, err := h.faults.Create(ctx, fault.CreateRequest{
		LampID: device.ID, FaultType: "线路故障", Description: "待配件", Reporter: "巡检员",
	})
	require.NoError(t, err)
	record, err := h.repairs.Create(ctx, repair.CreateRequest{FaultID: target.ID, Repairman: "维修工乙"})
	require.NoError(t, err)
	_, err = h.repairs.Finish(ctx, record.ID, repair.FinishRequest{Result: repair.ResultPendingParts})
	require.NoError(t, err)

	visits, err := h.visits.ListByFault(ctx, target.ID)
	require.NoError(t, err)
	require.Empty(t, visits, "非已修复完工不应生成回访任务")

	_, err = h.faults.Close(ctx, target.ID, fault.CloseRequest{})
	requireConflict(t, err)
}

// 已生成回访的维修记录不允许删除。
func TestRepairWithVisitCannotDelete(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	target, first := h.reportAndFinishFixed(t, "005")
	_ = target
	requireConflict(t, h.repairs.Delete(ctx, first.ID))
}

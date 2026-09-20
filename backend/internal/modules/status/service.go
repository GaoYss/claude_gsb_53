package status

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/visit"
	"streetlight/pkg/pagination"
)

// OverdueThreshold 是判定"超期未处理"的时长阈值。
const OverdueThreshold = 24 * time.Hour

// lampStatusSortSpec 定义维修状态列表允许的排序字段。
var lampStatusSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"code":       "lamp.code",
		"road_name":  "lamp.road_name",
		"run_status": "lamp.run_status",
		"lamp_type":  "lamp.lamp_type",
		"updated_at": "lamp.updated_at",
	},
	Default: "lamp.id",
}

// LampQuery 是维修状态列表的查询条件。
type LampQuery struct {
	pagination.Params
	Keyword   string `form:"keyword"`
	RoadName  string `form:"road_name"`
	LampType  string `form:"lamp_type"`
	RunStatus string `form:"run_status"`
	OnlyOpen  bool   `form:"only_open"` // 仅显示存在未闭环故障的路灯
}

// TrackQuery 是维修状态追踪的查询条件, 三者任选其一。
type TrackQuery struct {
	FaultID  uint   `form:"fault_id"`
	FaultNo  string `form:"fault_no"`
	LampCode string `form:"lamp_code"`
}

// Service 提供跨模块的维修状态查询能力(只读)。
// 作为读模型, 它直接基于 lamp / fault / repair 三张表组装视图, 避免不必要的多次往返查询。
type Service struct {
	db      *gorm.DB
	lamps   *lamp.Repository
	faults  *fault.Repository
	repairs *repair.Repository
	visits  *visit.Repository
}

// NewService 构造维修状态查询服务。
func NewService(db *gorm.DB, lamps *lamp.Repository, faults *fault.Repository, repairs *repair.Repository, visits *visit.Repository) *Service {
	return &Service{db: db, lamps: lamps, faults: faults, repairs: repairs, visits: visits}
}

// Overview 汇总维修状态看板数据。
func (s *Service) Overview(ctx context.Context) (*Overview, error) {
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	tomorrow := todayStart.AddDate(0, 0, 1)
	overdueBefore := now.Add(-OverdueThreshold)

	lampTotal, err := s.lamps.Count(ctx)
	if err != nil {
		return nil, err
	}
	lampByStatus, err := s.lamps.CountByColumn(ctx, "run_status")
	if err != nil {
		return nil, err
	}
	roadCount, err := s.lamps.CountDistinct(ctx, "road_name")
	if err != nil {
		return nil, err
	}

	faultTotal, err := s.faults.Count(ctx)
	if err != nil {
		return nil, err
	}
	faultOpen, err := s.faults.CountOpen(ctx)
	if err != nil {
		return nil, err
	}
	faultByStatus, err := s.faults.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}
	faultByType, err := s.faults.CountByColumn(ctx, "fault_type")
	if err != nil {
		return nil, err
	}
	faultByLevel, err := s.faults.CountByColumn(ctx, "fault_level")
	if err != nil {
		return nil, err
	}
	faultByRoad, err := s.faults.CountByColumn(ctx, "road_name")
	if err != nil {
		return nil, err
	}
	todayReported, err := s.faults.CountReportedBetween(ctx, todayStart, tomorrow)
	if err != nil {
		return nil, err
	}
	overdueTotal, err := s.faults.CountPendingBefore(ctx, overdueBefore)
	if err != nil {
		return nil, err
	}

	repairTotal, err := s.repairs.Count(ctx)
	if err != nil {
		return nil, err
	}
	repairByStatus, err := s.repairs.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}
	todayFinished, err := s.repairs.CountFinishedBetween(ctx, todayStart, tomorrow)
	if err != nil {
		return nil, err
	}
	averageDuration, err := s.repairs.AverageDurationHours(ctx)
	if err != nil {
		return nil, err
	}
	totalCost, err := s.repairs.SumCost(ctx)
	if err != nil {
		return nil, err
	}

	visitByStatus, err := s.visits.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}
	visitByResult, err := s.visits.CountByColumn(ctx, "result")
	if err != nil {
		return nil, err
	}
	avgSatisfaction, err := s.visits.AverageSatisfaction(ctx)
	if err != nil {
		return nil, err
	}
	visitCompleted := visitByStatus[visit.StatusCompleted]
	visitQualified := visitByResult[visit.ResultQualified]
	visitUnqualified := visitByResult[visit.ResultUnqualified]
	var qualifiedRate float64
	if visitCompleted > 0 {
		qualifiedRate = round2(float64(visitQualified) / float64(visitCompleted))
	}

	recentFaults, err := s.faults.ListRecent(ctx, 8)
	if err != nil {
		return nil, err
	}
	overdueFaults, err := s.faults.ListPendingBefore(ctx, overdueBefore, 8)
	if err != nil {
		return nil, err
	}

	return &Overview{
		Lamp: LampSummary{
			Total:       lampTotal,
			RoadCount:   roadCount,
			ByRunStatus: lampByStatus,
		},
		Fault: FaultSummary{
			Total:         faultTotal,
			OpenTotal:     faultOpen,
			ByStatus:      faultByStatus,
			TodayReported: todayReported,
			OverdueTotal:  overdueTotal,
		},
		Repair: RepairSummary{
			Total:             repairTotal,
			OngoingTotal:      repairByStatus[repair.StatusOngoing],
			FinishedTotal:     repairByStatus[repair.StatusFinished],
			TodayFinished:     todayFinished,
			AverageDurationHr: round2(averageDuration),
			TotalCost:         round2(totalCost),
		},
		Visit: VisitSummary{
			Total:            visitByStatus[visit.StatusPending] + visitCompleted,
			PendingTotal:     visitByStatus[visit.StatusPending],
			CompletedTotal:   visitCompleted,
			QualifiedTotal:   visitQualified,
			UnqualifiedTotal: visitUnqualified,
			ReworkTotal:      visitUnqualified,
			QualifiedRate:    qualifiedRate,
			AvgSatisfaction:  round2(avgSatisfaction),
		},
		FaultByType:   topCounts(faultByType, 0),
		FaultByLevel:  orderedCounts(faultByLevel, fault.Levels()),
		TopRoads:      topCounts(faultByRoad, 5),
		RecentFaults:  toBriefs(recentFaults),
		OverdueFaults: toBriefs(overdueFaults),
		OverdueHours:  OverdueThreshold.Hours(),
		GeneratedAt:   now,
	}, nil
}

// Lamps 查询路灯维修状态列表: 在台账信息之上叠加当前故障与最近一次维修进展。
func (s *Service) Lamps(ctx context.Context, query LampQuery) ([]LampStatusRow, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, lampStatusSortSpec)

	base := func() *gorm.DB {
		statement := s.db.WithContext(ctx).Model(&lamp.Lamp{})
		if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
			like := "%" + keyword + "%"
			statement = statement.Where(
				"lamp.code LIKE ? OR lamp.name LIKE ? OR lamp.road_name LIKE ? OR lamp.address LIKE ?",
				like, like, like, like,
			)
		}
		if value := strings.TrimSpace(query.RoadName); value != "" {
			statement = statement.Where("lamp.road_name = ?", value)
		}
		if value := strings.TrimSpace(query.LampType); value != "" {
			statement = statement.Where("lamp.lamp_type = ?", value)
		}
		if value := strings.TrimSpace(query.RunStatus); value != "" {
			statement = statement.Where("lamp.run_status = ?", value)
		}
		if query.OnlyOpen {
			statement = statement.Where(
				"EXISTS (SELECT 1 FROM fault WHERE fault.lamp_id = lamp.id AND fault.status IN ?)",
				[]string{fault.StatusPending, fault.StatusProcessing},
			)
		}
		return statement
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, page, err
	}

	devices := make([]lamp.Lamp, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&devices).Error; err != nil {
		return nil, 0, page, err
	}

	rows := make([]LampStatusRow, 0, len(devices))
	if len(devices) == 0 {
		return rows, total, page, nil
	}

	ids := make([]uint, 0, len(devices))
	for _, device := range devices {
		ids = append(ids, device.ID)
	}

	totalByLamp, err := s.countFaultsByLamp(ctx, ids, false)
	if err != nil {
		return nil, 0, page, err
	}
	openByLamp, err := s.countFaultsByLamp(ctx, ids, true)
	if err != nil {
		return nil, 0, page, err
	}
	currentFaults, err := s.currentFaults(ctx, ids)
	if err != nil {
		return nil, 0, page, err
	}
	latestRepairs, err := s.latestRepairs(ctx, ids)
	if err != nil {
		return nil, 0, page, err
	}

	for _, device := range devices {
		row := LampStatusRow{
			LampID:      device.ID,
			LampCode:    device.Code,
			LampName:    device.Name,
			RoadName:    device.RoadName,
			District:    device.District,
			LampType:    device.LampType,
			RunStatus:   device.RunStatus,
			TotalFaults: totalByLamp[device.ID],
			OpenFaults:  openByLamp[device.ID],
		}
		if current, ok := currentFaults[device.ID]; ok {
			reportedAt := current.ReportedAt
			row.FaultNo = current.FaultNo
			row.FaultType = current.FaultType
			row.FaultLevel = current.FaultLevel
			row.FaultStatus = current.Status
			row.FaultReported = &reportedAt
		}
		if latest, ok := latestRepairs[device.ID]; ok {
			row.RepairNo = latest.RepairNo
			row.Repairman = latest.Repairman
			row.RepairStatus = latest.Status
			row.RepairResult = latest.Result
			row.RepairedAt = latest.FinishedAt
		}
		rows = append(rows, row)
	}
	return rows, total, page, nil
}

// Track 按故障 ID / 故障单号 / 路灯编号查询完整处理链路。
func (s *Service) Track(ctx context.Context, query TrackQuery) (*TrackResult, error) {
	switch {
	case query.FaultID > 0:
		entity, err := s.faults.GetByID(ctx, query.FaultID)
		if err != nil {
			return nil, err
		}
		return s.buildFaultTrack(ctx, entity)

	case strings.TrimSpace(query.FaultNo) != "":
		entity, err := s.faults.GetByNo(ctx, strings.TrimSpace(query.FaultNo))
		if err != nil {
			return nil, err
		}
		return s.buildFaultTrack(ctx, entity)

	case strings.TrimSpace(query.LampCode) != "":
		device, err := s.lamps.GetByCode(ctx, strings.TrimSpace(query.LampCode))
		if err != nil {
			return nil, err
		}
		history, err := s.faults.ListByLamp(ctx, device.ID)
		if err != nil {
			return nil, err
		}

		result := &TrackResult{
			SearchType:    "lamp",
			Lamp:          device,
			Repairs:       make([]repair.Repair, 0),
			Visits:        make([]visit.Visit, 0),
			Timeline:      make([]TimelineEvent, 0),
			RelatedFaults: toBriefs(history),
		}
		if len(history) > 0 {
			latest := history[0]
			repairs, err := s.repairs.ListByFault(ctx, latest.ID)
			if err != nil {
				return nil, err
			}
			visits, err := s.visits.ListByFault(ctx, latest.ID)
			if err != nil {
				return nil, err
			}
			result.Fault = &latest
			result.Repairs = repairs
			result.Visits = visits
			result.ReworkCount = reworkCount(visits)
			result.VisitStatus = latestVisitStatus(visits)
			result.Timeline = buildTimeline(&latest, repairs, visits)
		}
		return result, nil

	default:
		return nil, apperr.BadRequest("请提供 fault_id、fault_no 或 lamp_code 之一作为查询条件")
	}
}

// buildFaultTrack 组装单条故障的完整链路。
func (s *Service) buildFaultTrack(ctx context.Context, entity *fault.Fault) (*TrackResult, error) {
	device, err := s.lamps.GetByID(ctx, entity.LampID)
	if err != nil {
		return nil, err
	}
	repairs, err := s.repairs.ListByFault(ctx, entity.ID)
	if err != nil {
		return nil, err
	}
	visits, err := s.visits.ListByFault(ctx, entity.ID)
	if err != nil {
		return nil, err
	}
	return &TrackResult{
		SearchType:  "fault",
		Lamp:        device,
		Fault:       entity,
		Repairs:     repairs,
		Visits:      visits,
		ReworkCount: reworkCount(visits),
		VisitStatus: latestVisitStatus(visits),
		Timeline:    buildTimeline(entity, repairs, visits),
	}, nil
}

// countFaultsByLamp 批量统计每盏路灯的故障数量, openOnly 为 true 时仅统计未闭环故障。
func (s *Service) countFaultsByLamp(ctx context.Context, lampIDs []uint, openOnly bool) (map[uint]int64, error) {
	type row struct {
		LampID uint
		Total  int64
	}
	rows := make([]row, 0)

	statement := s.db.WithContext(ctx).Model(&fault.Fault{}).
		Select("lamp_id, COUNT(*) AS total").
		Where("lamp_id IN ?", lampIDs)
	if openOnly {
		statement = statement.Where("status IN ?", []string{fault.StatusPending, fault.StatusProcessing})
	}
	if err := statement.Group("lamp_id").Scan(&rows).Error; err != nil {
		return nil, err
	}

	result := make(map[uint]int64, len(rows))
	for _, item := range rows {
		result[item.LampID] = item.Total
	}
	return result, nil
}

// currentFaults 批量取出每盏路灯当前未闭环的故障(取最近一条)。
func (s *Service) currentFaults(ctx context.Context, lampIDs []uint) (map[uint]fault.Fault, error) {
	entities := make([]fault.Fault, 0)
	err := s.db.WithContext(ctx).Model(&fault.Fault{}).
		Where("lamp_id IN ? AND status IN ?", lampIDs, []string{fault.StatusPending, fault.StatusProcessing}).
		Order("reported_at DESC, id DESC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]fault.Fault, len(entities))
	for _, item := range entities {
		if _, exists := result[item.LampID]; !exists {
			result[item.LampID] = item
		}
	}
	return result, nil
}

// latestRepairs 批量取出每盏路灯最近一次的维修记录。
func (s *Service) latestRepairs(ctx context.Context, lampIDs []uint) (map[uint]repair.Repair, error) {
	entities := make([]repair.Repair, 0)
	err := s.db.WithContext(ctx).Model(&repair.Repair{}).
		Where("lamp_id IN ?", lampIDs).
		Order("started_at DESC, id DESC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}

	result := make(map[uint]repair.Repair, len(entities))
	for _, item := range entities {
		if _, exists := result[item.LampID]; !exists {
			result[item.LampID] = item
		}
	}
	return result, nil
}

// buildTimeline 依据故障、维修与回访记录构建处置时间线。
// 返修的开工/完工作为独立维修记录自然追加, 首次处置过程与原完工时间不会被改写。
func buildTimeline(entity *fault.Fault, repairs []repair.Repair, visits []visit.Visit) []TimelineEvent {
	events := make([]TimelineEvent, 0, len(repairs)*2+len(visits)*2+2)

	events = append(events, TimelineEvent{
		Stage:     "reported",
		Label:     "故障登记",
		Operator:  entity.Reporter,
		Detail:    entity.FaultType + ": " + entity.Description,
		Timestamp: entity.ReportedAt,
	})

	for _, item := range repairs {
		events = append(events, TimelineEvent{
			Stage:     "repair_started",
			Label:     "维修开工",
			Operator:  item.Repairman,
			Detail:    strings.TrimSpace(item.RepairNo + " " + item.Content),
			Timestamp: item.StartedAt,
		})
		if item.FinishedAt != nil {
			detail := item.RepairNo
			if item.Result != "" {
				detail = strings.TrimSpace(detail + " 结果: " + repair.ResultLabel(item.Result))
			}
			if item.Materials != "" {
				detail = strings.TrimSpace(detail + " 耗材: " + item.Materials)
			}
			events = append(events, TimelineEvent{
				Stage:     "repair_finished",
				Label:     "维修完成",
				Operator:  item.Repairman,
				Detail:    detail,
				Timestamp: *item.FinishedAt,
			})
		}
	}

	for _, item := range visits {
		events = append(events, TimelineEvent{
			Stage:     "visit_created",
			Label:     "生成回访任务",
			Detail:    strings.TrimSpace(item.VisitNo + " 第" + chineseRound(item.Round) + "轮回访"),
			Timestamp: item.CreatedAt,
		})
		if item.VisitedAt != nil {
			switch item.Result {
			case visit.ResultQualified:
				events = append(events, TimelineEvent{
					Stage:     "visit_qualified",
					Label:     "回访合格",
					Operator:  item.Visitor,
					Detail:    visitDetail(item),
					Timestamp: *item.VisitedAt,
				})
			case visit.ResultUnqualified:
				events = append(events, TimelineEvent{
					Stage:     "visit_unqualified",
					Label:     "回访不合格 · 触发返修",
					Operator:  item.Visitor,
					Detail:    visitDetail(item),
					Timestamp: *item.VisitedAt,
				})
			}
		}
	}

	// 标记返修开工/完工节点, 与首修区分。
	reworkRepairs := reworkRepairNos(visits, repairs)
	for index := range events {
		if !strings.HasPrefix(events[index].Detail, "WX") {
			continue
		}
		no := repairNoFromDetail(events[index].Detail)
		if reworkRepairs[no] {
			switch events[index].Stage {
			case "repair_started":
				events[index].Stage = "rework_started"
				events[index].Label = "返修开工"
			case "repair_finished":
				events[index].Stage = "rework_finished"
				events[index].Label = "返修完成"
			}
		}
	}

	if entity.ClosedAt != nil {
		events = append(events, TimelineEvent{
			Stage:     "closed",
			Label:     "故障关闭",
			Detail:    entity.CloseRemark,
			Timestamp: *entity.ClosedAt,
		})
	}

	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	return events
}

// reworkRepairNos 收集由不合格回访触发的返修维修单号。
func reworkRepairNos(visits []visit.Visit, repairs []repair.Repair) map[string]bool {
	idToNo := make(map[uint]string, len(repairs))
	for _, item := range repairs {
		idToNo[item.ID] = item.RepairNo
	}
	result := make(map[string]bool)
	for _, item := range visits {
		if item.ReworkRepairID != nil {
			if no := idToNo[*item.ReworkRepairID]; no != "" {
				result[no] = true
			}
		}
		// 兜底: 第 2 轮及以后回访对应的维修单也是返修单(种子数据等场景可能未写 rework_repair_id)。
		if item.Round > 1 {
			result[item.RepairNo] = true
		}
	}
	return result
}

// reworkCount 返修次数 = 回访不合格次数。
func reworkCount(visits []visit.Visit) int {
	count := 0
	for _, item := range visits {
		if item.Result == visit.ResultUnqualified {
			count++
		}
	}
	return count
}

// latestVisitStatus 末次回访的总体状态, 供前端快速展示。
func latestVisitStatus(visits []visit.Visit) string {
	if len(visits) == 0 {
		return "none"
	}
	latest := visits[len(visits)-1]
	switch {
	case latest.Result == visit.ResultQualified:
		return "qualified"
	case latest.Result == visit.ResultUnqualified:
		return "unqualified"
	default:
		return "pending"
	}
}

// visitDetail 组装回访时间线节点的描述。
func visitDetail(item visit.Visit) string {
	var builder strings.Builder
	if item.Satisfaction != nil {
		builder.WriteString("满意度 ")
		builder.WriteString(strconv.Itoa(*item.Satisfaction))
		builder.WriteString(" 分")
	}
	if item.UnqualifiedReason != "" {
		if builder.Len() > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString("原因: ")
		builder.WriteString(item.UnqualifiedReason)
	}
	if item.Feedback != "" {
		if builder.Len() > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(item.Feedback)
	}
	return builder.String()
}

// repairNoFromDetail 从时间线描述前缀中取出维修单号。
func repairNoFromDetail(detail string) string {
	fields := strings.Fields(detail)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// chineseRound 返回回访轮次的中文序数(前 5 轮), 超出部分用阿拉伯数字。
func chineseRound(round int) string {
	nums := []string{"", "1", "2", "3", "4", "5"}
	if round >= 1 && round <= 5 {
		return nums[round]
	}
	return strconv.Itoa(round)
}

// orderedCounts 按给定顺序输出分组统计, 保证前端展示顺序稳定且包含零值项。
func orderedCounts(counts map[string]int64, order []string) []LabelCount {
	result := make([]LabelCount, 0, len(order))
	for _, label := range order {
		result = append(result, LabelCount{Label: label, Count: counts[label]})
	}
	return result
}

// topCounts 按数量倒序输出分组统计, limit <= 0 表示不截断。
func topCounts(counts map[string]int64, limit int) []LabelCount {
	result := make([]LabelCount, 0, len(counts))
	for label, count := range counts {
		result = append(result, LabelCount{Label: label, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Label < result[j].Label
		}
		return result[i].Count > result[j].Count
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

// toBriefs 将故障记录转换为摘要并计算已等待时长。
func toBriefs(entities []fault.Fault) []FaultBrief {
	now := time.Now()
	result := make([]FaultBrief, 0, len(entities))
	for _, item := range entities {
		result = append(result, FaultBrief{
			ID:           item.ID,
			FaultNo:      item.FaultNo,
			LampCode:     item.LampCode,
			RoadName:     item.RoadName,
			FaultType:    item.FaultType,
			FaultLevel:   item.FaultLevel,
			Status:       item.Status,
			ReportedAt:   item.ReportedAt,
			WaitingHours: round2(now.Sub(item.ReportedAt).Hours()),
		})
	}
	return result
}

// round2 保留两位小数。
func round2(value float64) float64 {
	if value < 0 {
		value = 0
	}
	return math.Round(value*100) / 100
}

package visit

import (
	"context"
	"strings"
	"time"

	"streetlight/internal/apperr"
	"streetlight/internal/modules/repair"
	"streetlight/pkg/pagination"
)

// visitSortSpec 定义回访任务列表允许的排序字段白名单。
var visitSortSpec = pagination.SortSpec{
	Allowed: map[string]string{
		"visit_no":   "visit_no",
		"due_at":     "due_at",
		"status":     "status",
		"created_at": "created_at",
		"contacted_at": "contacted_at",
		"round":      "round",
	},
	Default: "due_at",
}

// RepairPort 由维修记录模块实现, 回访模块通过它读取维修信息并在不合格时发起返修。
type RepairPort interface {
	Get(ctx context.Context, id uint) (*repair.Repair, error)
	ListByFault(ctx context.Context, faultID uint) ([]repair.Repair, error)
	CreateRework(ctx context.Context, originalRepairID uint, req repair.ReworkRequest) (*repair.Repair, error)
	CountRework(ctx context.Context) (int64, error)
}

// Service 承载维修质量回访的业务规则, 并向故障模块提供结算前校验。
type Service struct {
	repo    *Repository
	repairs RepairPort
}

// NewService 构造质量回访服务。
func NewService(repo *Repository, repairs RepairPort) *Service {
	return &Service{repo: repo, repairs: repairs}
}

// Get 查询回访任务详情。
func (s *Service) Get(ctx context.Context, id uint) (*Visit, error) {
	return s.repo.GetByID(ctx, id)
}

// ListByFault 查询某条故障的全部回访轮次。
func (s *Service) ListByFault(ctx context.Context, faultID uint) ([]Visit, error) {
	items, _, err := s.repo.List(ctx, Filter{FaultID: faultID}, pagination.Query{
		Page: 1, PageSize: 200, SortColumn: "round", Descending: false,
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// List 分页查询回访任务。
func (s *Service) List(ctx context.Context, query ListQuery) ([]Visit, int64, pagination.Query, error) {
	page := pagination.Parse(query.Params, visitSortSpec)
	filter, err := buildFilter(query)
	if err != nil {
		return nil, 0, page, err
	}
	items, total, err := s.repo.List(ctx, filter, page)
	if err != nil {
		return nil, 0, page, err
	}
	return items, total, page, nil
}

// OnRepairFinished 实现维修模块的回访钩子: 维修完工(已修复)后按规则自动生成回访任务。
// 返修单完工同样会触发, 此时生成下一轮回访, 轮次递增。
func (s *Service) OnRepairFinished(ctx context.Context, repairID uint) error {
	record, err := s.repairs.Get(ctx, repairID)
	if err != nil {
		return err
	}
	if record.Status != repair.StatusFinished || record.Result != repair.ResultFixed {
		return nil
	}

	// 幂等保护: 同一维修记录只生成一轮回访任务。
	existing, err := s.repo.GetByRepair(ctx, repairID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	round, err := s.repo.CountByFault(ctx, record.FaultID)
	if err != nil {
		return err
	}

	finishedAt := time.Now()
	if record.FinishedAt != nil {
		finishedAt = *record.FinishedAt
	}

	entity := &Visit{
		FaultID:  record.FaultID,
		FaultNo:  record.FaultNo,
		LampID:   record.LampID,
		LampCode: record.LampCode,
		RepairID: record.ID,
		RepairNo: record.RepairNo,
		Round:    int(round) + 1,
		DueAt:    finishedAt.Add(DefaultDeadline),
		Status:   StatusPending,
	}
	prefix := "HF" + finishedAt.Format("20060102")
	return s.repo.CreateWithUniqueNo(ctx, entity, prefix)
}

// RecordContact 记录一次回访联系情况, 不改变评定结论, 可在评定前多次追加。
func (s *Service) RecordContact(ctx context.Context, id uint, req ContactRequest) (*Visit, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.IsClosed() {
		return nil, apperr.Conflict("回访任务 %s 已结束, 不允许继续追加联系记录", entity.VisitNo)
	}

	contactedAt, err := parseTime(req.ContactedAt, time.Now())
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.ContactName)
	phone := strings.TrimSpace(req.ContactPhone)

	logEntry := &VisitContactLog{
		VisitID:       entity.ID,
		ContactResult: req.ContactResult,
		ContactName:   name,
		ContactPhone:  phone,
		Content:       strings.TrimSpace(req.Content),
		ContactedAt:   contactedAt,
	}
	if err := s.repo.CreateContactLog(ctx, logEntry); err != nil {
		return nil, err
	}

	// 首次联系后任务从"待回访"推进到"已联系待评定"。
	if entity.Status == StatusPending {
		entity.Status = StatusContacted
	}
	entity.ContactResult = req.ContactResult
	if name != "" {
		entity.ContactName = name
	}
	if phone != "" {
		entity.ContactPhone = phone
	}
	if entity.ContactedAt == nil || contactedAt.After(*entity.ContactedAt) {
		entity.ContactedAt = &contactedAt
	}
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

// Evaluate 回访评定: 记录联系情况与满意度, 合格则闭环; 不合格则自动发起返修并关联原维修记录。
func (s *Service) Evaluate(ctx context.Context, id uint, req EvaluateRequest) (*Visit, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.IsClosed() {
		return nil, apperr.Conflict("回访任务 %s 已结束, 不允许重复评定", entity.VisitNo)
	}

	contactedAt, err := parseTime(req.ContactedAt, time.Now())
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.ContactName)
	phone := strings.TrimSpace(req.ContactPhone)

	if err := s.repo.CreateContactLog(ctx, &VisitContactLog{
		VisitID:       entity.ID,
		ContactResult: req.ContactResult,
		ContactName:   name,
		ContactPhone:  phone,
		Content:       strings.TrimSpace(req.Content),
		ContactedAt:   contactedAt,
	}); err != nil {
		return nil, err
	}

	entity.ContactResult = req.ContactResult
	entity.ContactName = name
	entity.ContactPhone = phone
	entity.ContactedAt = &contactedAt
	entity.Satisfaction = &req.Satisfaction
	entity.Content = strings.TrimSpace(req.Content)
	entity.Remark = strings.TrimSpace(req.Remark)

	qualified := req.Qualified != nil && *req.Qualified
	entity.Qualified = &qualified

	if qualified {
		// 合格: 回访闭环, 故障维持"已修复", 可继续结算关闭。
		entity.Status = StatusQualified
		if err := s.repo.Update(ctx, entity); err != nil {
			return nil, err
		}
		return s.repo.GetByID(ctx, id)
	}

	// 不合格: 自动创建返修维修单(关联原维修记录), 故障回退为维修中。
	reworkReq := repair.ReworkRequest{}
	if req.Rework != nil {
		reworkReq = repair.ReworkRequest{
			Repairman:    req.Rework.Repairman,
			RepairTeam:   req.Rework.RepairTeam,
			ContactPhone: req.Rework.ContactPhone,
			StartedAt:    req.Rework.StartedAt,
			Content:      req.Rework.Content,
			Remark:       req.Rework.Remark,
		}
	}
	if strings.TrimSpace(reworkReq.Content) == "" {
		reworkReq.Content = "回访不合格触发返修: " + entity.Content
	}

	rework, err := s.repairs.CreateRework(ctx, entity.RepairID, reworkReq)
	if err != nil {
		return nil, err
	}

	entity.Status = StatusUnqualified
	entity.ReworkRepairID = &rework.ID
	entity.ReworkRepairNo = rework.RepairNo
	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

// CheckSettlement 实现故障模块的结算前校验: 存在已修复完工记录时, 必须最终回访合格才允许结算。
func (s *Service) CheckSettlement(ctx context.Context, faultID uint) error {
	repairs, err := s.repairs.ListByFault(ctx, faultID)
	if err != nil {
		return err
	}

	hasFixedFinish := false
	for _, item := range repairs {
		if item.Status == repair.StatusFinished && item.Result == repair.ResultFixed {
			hasFixedFinish = true
			break
		}
	}
	if !hasFixedFinish {
		// 没有已修复完工记录(如误报作废), 无需回访即可结算。
		return nil
	}

	latest, err := s.repo.LatestByFault(ctx, faultID)
	if err != nil {
		return err
	}
	if latest == nil {
		return apperr.Conflict("该故障维修已完工但尚未回访, 回访完成前不允许结算")
	}
	switch latest.Status {
	case StatusQualified:
		return nil
	case StatusUnqualified:
		return apperr.Conflict("最近一轮回访(%s)不合格, 返修单 %s 完工并重新回访合格前不允许结算",
			latest.VisitNo, latest.ReworkRepairNo)
	default:
		return apperr.Conflict("回访任务 %s 尚未完成, 回访合格前不允许结算", latest.VisitNo)
	}
}

// Metadata 返回回访模块字典。
func (s *Service) Metadata() *Meta {
	return &Meta{
		Statuses:       Statuses(),
		ContactResults: ContactResults(),
	}
}

// Statistics 汇总回访与返修统计。
func (s *Service) Statistics(ctx context.Context) (*Statistics, error) {
	total, err := s.repo.Count(ctx)
	if err != nil {
		return nil, err
	}
	byStatus, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	overdue, err := s.repo.CountOverdue(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	avgScore, err := s.repo.AverageSatisfaction(ctx)
	if err != nil {
		return nil, err
	}
	reworkTotal, err := s.repairs.CountRework(ctx)
	if err != nil {
		return nil, err
	}

	result := &Statistics{
		Total:            total,
		PendingTotal:     byStatus[StatusPending] + byStatus[StatusContacted],
		QualifiedTotal:   byStatus[StatusQualified],
		UnqualifiedTotal: byStatus[StatusUnqualified],
		OverdueTotal:     overdue,
		ReworkTotal:      reworkTotal,
		AverageScore:     round2(avgScore),
	}
	if total > 0 {
		result.QualifiedRate = round2(float64(result.QualifiedTotal) / float64(total))
	}
	return result, nil
}

// buildFilter 将查询参数转换为仓储条件。
func buildFilter(query ListQuery) (Filter, error) {
	filter := Filter{
		Keyword:  strings.TrimSpace(query.Keyword),
		FaultID:  query.FaultID,
		RepairID: query.RepairID,
		Status:   strings.TrimSpace(query.Status),
		Overdue:  query.Overdue,
	}
	if filter.Status != "" && !IsValidStatus(filter.Status) {
		return filter, apperr.BadRequest("非法的回访状态: %s", filter.Status)
	}
	switch strings.TrimSpace(query.Qualified) {
	case "":
	case "true":
		value := true
		filter.Qualified = &value
	case "false":
		value := false
		filter.Qualified = &value
	default:
		return filter, apperr.BadRequest("qualified 仅支持 true / false")
	}

	if value := strings.TrimSpace(query.StartDate); value != "" {
		from, err := parseDay(value)
		if err != nil {
			return filter, err
		}
		filter.CreatedFrom = &from
	}
	if value := strings.TrimSpace(query.EndDate); value != "" {
		to, err := parseDay(value)
		if err != nil {
			return filter, err
		}
		to = to.AddDate(0, 0, 1)
		filter.CreatedTo = &to
	}
	if filter.CreatedFrom != nil && filter.CreatedTo != nil && filter.CreatedTo.Before(*filter.CreatedFrom) {
		return filter, apperr.BadRequest("结束日期不能早于开始日期")
	}
	return filter, nil
}

// parseTime 解析时间字符串, 为空时返回 fallback。
func parseTime(value string, fallback time.Time) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, apperr.BadRequest("时间格式不正确, 建议使用 YYYY-MM-DD HH:mm:ss: %s", value)
}

// parseDay 解析 YYYY-MM-DD 日期, 返回当天零点。
func parseDay(value string) (time.Time, error) {
	date, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), time.Local)
	if err != nil {
		return time.Time{}, apperr.BadRequest("日期格式应为 YYYY-MM-DD, 当前值: %s", value)
	}
	return date, nil
}

func round2(value float64) float64 {
	if value < 0 {
		value = 0
	}
	return float64(int(value*100+0.5)) / 100
}

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
		"fault_no":   "fault_no",
		"lamp_code":  "lamp_code",
		"repair_no":  "repair_no",
		"round":      "round",
		"status":     "status",
		"result":     "result",
		"created_at": "created_at",
		"visited_at": "visited_at",
	},
	Default: "created_at",
}

// RepairReader 由维修模块仓储实现, 回访模块只读维修单信息。
type RepairReader interface {
	GetByID(ctx context.Context, id uint) (*repair.Repair, error)
	LatestByFault(ctx context.Context, faultID uint) (*repair.Repair, error)
}

// RepairFactory 由维修模块服务实现, 回访不合格时用它开出返修维修单。
type RepairFactory interface {
	CreateRework(ctx context.Context, req repair.CreateRequest) (*repair.Repair, error)
}

// Service 承载维修质量回访的业务规则。
type Service struct {
	repo    *Repository
	repairs RepairReader
	factory RepairFactory
}

// NewService 构造回访服务。
func NewService(repo *Repository, repairs RepairReader, factory RepairFactory) *Service {
	return &Service{repo: repo, repairs: repairs, factory: factory}
}

// Detail 回访任务详情, 含历次联系记录。
type Detail struct {
	Visit
	Contacts []VisitContact `json:"contacts"`
}

// Get 查询回访任务详情与联系记录。
func (s *Service) Get(ctx context.Context, id uint) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	contacts, err := s.repo.ListContacts(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Detail{Visit: *entity, Contacts: contacts}, nil
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

// ListByFault 查询某条故障的全部回访任务。
func (s *Service) ListByFault(ctx context.Context, faultID uint) ([]Visit, error) {
	return s.repo.ListByFault(ctx, faultID)
}

// Complete 执行回访:
//   - 未联系上: 只追加一条联系记录, 回访单保持待回访, 可稍后再次联系;
//   - 已联系上且合格: 回访完成, 故障具备结算条件;
//   - 已联系上但不合格: 回访完成并自动开出返修维修单(关联原维修记录),
//     返修完工后会按规则生成下一轮回访任务。
func (s *Service) Complete(ctx context.Context, id uint, req CompleteRequest) (*Detail, error) {
	entity, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity.Status == StatusCompleted {
		return nil, apperr.Conflict("回访任务 %s 已完成, 不允许重复提交", entity.VisitNo)
	}

	contactStatus := strings.TrimSpace(req.ContactStatus)
	method := strings.TrimSpace(req.ContactMethod)
	if method == "" {
		method = MethodPhone
	}
	if !IsValidContactMethod(method) {
		return nil, apperr.BadRequest("非法的联系方式: %s", method)
	}
	if contactStatus != ContactReached && contactStatus != ContactUnreached {
		return nil, apperr.BadRequest("非法的联系结果: %s", contactStatus)
	}

	now := time.Now()
	contact := &VisitContact{
		VisitID:       entity.ID,
		Result:        contactStatus,
		Method:        method,
		ContactPerson: strings.TrimSpace(req.ContactPerson),
		Phone:         strings.TrimSpace(req.Phone),
		Remark:        strings.TrimSpace(req.Remark),
		ContactedAt:   now,
	}
	if err := s.repo.CreateContact(ctx, contact); err != nil {
		return nil, err
	}
	entity.ContactAttempts++
	entity.LastContactAt = &now

	// 未联系上: 仅记录联系情况, 等待再次回访。
	if contactStatus == ContactUnreached {
		if feedback := strings.TrimSpace(req.Feedback); feedback != "" {
			entity.Feedback = feedback
		}
		if err := s.repo.Update(ctx, entity); err != nil {
			return nil, err
		}
		return s.Get(ctx, id)
	}

	// 已联系上: 必须给出回访结论与满意度。
	result := strings.TrimSpace(req.Result)
	if result != ResultQualified && result != ResultUnqualified {
		return nil, apperr.BadRequest("已联系上时必须给出回访结论: 合格或不合格")
	}
	if req.Satisfaction == nil {
		return nil, apperr.BadRequest("请记录本次回访的满意度评分(1-5)")
	}
	visitedAt := now
	if value := strings.TrimSpace(req.VisitedAt); value != "" {
		parsed, err := parseTime(value)
		if err != nil {
			return nil, err
		}
		visitedAt = parsed
	}
	if visitedAt.Before(entity.CreatedAt) {
		return nil, apperr.BadRequest("回访时间不能早于回访任务生成时间")
	}

	entity.ContactStatus = ContactReached
	entity.ContactMethod = method
	entity.Visitor = strings.TrimSpace(req.Visitor)
	entity.VisitedAt = &visitedAt
	entity.Satisfaction = req.Satisfaction
	entity.Feedback = strings.TrimSpace(req.Feedback)
	entity.Status = StatusCompleted
	entity.Result = result

	if result == ResultQualified {
		entity.UnqualifiedReason = ""
		if err := s.repo.Update(ctx, entity); err != nil {
			return nil, err
		}
		return s.Get(ctx, id)
	}

	// 不合格: 必须说明原因, 随后触发返修。
	reason := strings.TrimSpace(req.UnqualifiedReason)
	if reason == "" {
		return nil, apperr.BadRequest("回访不合格时必须填写不合格原因, 用于派工返修")
	}
	entity.UnqualifiedReason = reason

	// 先开出返修单(故障从已修复回退到维修中), 再落回访结论,
	// 避免出现"已判不合格但没有返修任务"的悬空状态。
	rework, err := s.createRework(ctx, entity, req, reason)
	if err != nil {
		return nil, err
	}
	entity.ReworkRepairID = &rework.ID

	if err := s.repo.Update(ctx, entity); err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// createRework 依据不合格回访开出返修维修单, 返修单与首次处置记录同属一条故障。
func (s *Service) createRework(ctx context.Context, entity *Visit, req CompleteRequest, reason string) (*repair.Repair, error) {
	repairman := strings.TrimSpace(req.ReworkRepairman)
	if repairman == "" {
		repairman = entity.Repairman // 默认仍派给原维修人员
	}
	if repairman == "" {
		return nil, apperr.BadRequest("返修维修人员不能为空")
	}

	content := strings.TrimSpace("质量回访不合格, 安排返修: " + reason)
	createReq := repair.CreateRequest{
		FaultID:      entity.FaultID,
		Repairman:    repairman,
		RepairTeam:   strings.TrimSpace(req.ReworkTeam),
		ContactPhone: strings.TrimSpace(req.ReworkContact),
		Content:      content,
		Materials:    strings.TrimSpace(req.ReworkMaterials),
	}
	return s.factory.CreateRework(ctx, createReq)
}

// OnRepairFinished 维修完工钩子: 按规则在"已修复"完工后自动生成回访任务。
// 同一维修单只生成一次; 返修完工后 round 递增, 首轮回访的原维修单信息始终保留。
func (s *Service) OnRepairFinished(ctx context.Context, record *repair.Repair) error {
	if record == nil || record.Result != repair.ResultFixed {
		return nil
	}
	existing, err := s.repo.GetByRepair(ctx, record.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}

	round := 1
	originRepairID := record.ID
	originRepairNo := record.RepairNo
	if latest, err := s.repo.LatestByFault(ctx, record.FaultID); err != nil {
		return err
	} else if latest != nil {
		round = latest.Round + 1
		originRepairID = latest.OriginRepairID
		originRepairNo = latest.OriginRepairNo
	}

	entity := &Visit{
		FaultID:        record.FaultID,
		FaultNo:        record.FaultNo,
		LampID:         record.LampID,
		LampCode:       record.LampCode,
		RepairID:       record.ID,
		RepairNo:       record.RepairNo,
		OriginRepairID: originRepairID,
		OriginRepairNo: originRepairNo,
		Repairman:      record.Repairman,
		Round:          round,
		Status:         StatusPending,
	}
	prefix := "HF" + record.FinishedAt.Format("20060102")
	return s.repo.CreateWithUniqueNo(ctx, entity, prefix)
}

// EnsureRepairDeletable 维修单删除守卫: 已纳入质量回访的维修记录不允许删除。
func (s *Service) EnsureRepairDeletable(ctx context.Context, repairID uint) error {
	entity, err := s.repo.GetByRepair(ctx, repairID)
	if err != nil {
		return err
	}
	if entity != nil {
		return apperr.Conflict("维修单 %s 已生成质量回访记录, 不允许删除", entity.RepairNo)
	}
	return nil
}

// EnsureSettleable 实现故障模块的结算守卫:
// 故障关闭(结算)前, 末次维修必须已完工且其回访任务结论为合格。
func (s *Service) EnsureSettleable(ctx context.Context, faultID uint) error {
	latest, err := s.repairs.LatestByFault(ctx, faultID)
	if err != nil {
		return err
	}
	if latest == nil {
		return nil // 无维修记录(如误报作废), 不要求回访
	}
	if latest.Status != repair.StatusFinished {
		return apperr.Conflict("维修单 %s 尚未完工, 质量回访未完成, 不允许结算", latest.RepairNo)
	}

	entity, err := s.repo.GetByRepair(ctx, latest.ID)
	if err != nil {
		return err
	}
	switch {
	case entity == nil:
		// 仅"已修复"完工才生成回访; 末次维修为其它结果说明问题尚未真正解决。
		if latest.Result == repair.ResultFixed {
			return apperr.Conflict("维修单 %s 的质量回访任务尚未生成, 不允许结算", latest.RepairNo)
		}
		return apperr.Conflict("维修单 %s 的维修结果为 %s, 未通过质量回访, 不允许结算",
			latest.RepairNo, repair.ResultLabel(latest.Result))
	case entity.Status != StatusCompleted:
		return apperr.Conflict("回访任务 %s 尚未完成, 不允许结算", entity.VisitNo)
	case entity.Result == ResultUnqualified:
		return apperr.Conflict("回访任务 %s 判定不合格, 返修尚未完成, 不允许结算", entity.VisitNo)
	}
	return nil
}

// Metadata 返回回访模块字典。
func (s *Service) Metadata(ctx context.Context) (*Meta, error) {
	visitors, err := s.repo.DistinctVisitors(ctx)
	if err != nil {
		return nil, err
	}
	return &Meta{
		Statuses:       Statuses(),
		Results:        Results(),
		ContactResults: ContactResults(),
		ContactMethods: ContactMethods(),
		Visitors:       visitors,
	}, nil
}

// Statistics 汇总回访质量统计, 返修次数以"不合格且已派返修单"的回访计。
func (s *Service) Statistics(ctx context.Context) (*Statistics, error) {
	byStatus, err := s.repo.CountByColumn(ctx, "status")
	if err != nil {
		return nil, err
	}
	byResult, err := s.repo.CountByColumn(ctx, "result")
	if err != nil {
		return nil, err
	}
	avgSatisfaction, err := s.repo.AverageSatisfaction(ctx)
	if err != nil {
		return nil, err
	}

	completed := byStatus[StatusCompleted]
	unqualified := byResult[ResultUnqualified]
	result := &Statistics{
		Total:            byStatus[StatusPending] + completed,
		PendingTotal:     byStatus[StatusPending],
		CompletedTotal:   completed,
		QualifiedTotal:   byResult[ResultQualified],
		UnqualifiedTotal: unqualified,
		ReworkTotal:      unqualified,
		AvgSatisfaction:  round2(avgSatisfaction),
	}
	if completed > 0 {
		result.QualifiedRate = round2(float64(result.QualifiedTotal) / float64(completed))
	}
	return result, nil
}

// buildFilter 将查询参数转换为仓储条件并解析日期区间。
func buildFilter(query ListQuery) (Filter, error) {
	filter := Filter{
		Keyword:  strings.TrimSpace(query.Keyword),
		FaultID:  query.FaultID,
		RepairID: query.RepairID,
		Status:   strings.TrimSpace(query.Status),
		Result:   strings.TrimSpace(query.Result),
		Round:    query.Round,
	}
	if filter.Status != "" && filter.Status != StatusPending && filter.Status != StatusCompleted {
		return filter, apperr.BadRequest("非法的回访状态: %s", filter.Status)
	}
	if filter.Result != "" && filter.Result != ResultQualified && filter.Result != ResultUnqualified {
		return filter, apperr.BadRequest("非法的回访结论: %s", filter.Result)
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

func parseTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, apperr.BadRequest("时间格式不正确, 建议使用 YYYY-MM-DD HH:mm:ss: %s", value)
}

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

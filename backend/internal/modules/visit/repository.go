package visit

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/apperr"
	"streetlight/pkg/pagination"
)

// openStatuses 是回访未闭环的状态集合。
var openStatuses = []string{StatusPending, StatusContacted}

// Filter 是仓储层使用的回访任务查询条件。
type Filter struct {
	Keyword        string
	FaultID        uint
	RepairID       uint
	Status         string
	Qualified      *bool
	Overdue        bool
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
}

// Repository 负责质量回访数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造回访仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// Create 新增回访任务。
func (r *Repository) Create(ctx context.Context, entity *Visit) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("创建回访任务失败: %w", err)
	}
	return nil
}

// CreateWithUniqueNo 生成唯一回访单号并落库, 冲突时自动重试。
func (r *Repository) CreateWithUniqueNo(ctx context.Context, entity *Visit, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.NextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.VisitNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		err = r.Create(ctx, entity)
		if err == nil {
			return nil
		}
		if !isUniqueViolation(err) {
			return err
		}
	}
	return apperr.Conflict("回访单号生成冲突, 请稍后重试")
}

// NextSequence 返回指定前缀下可用的下一个流水号。
func (r *Repository) NextSequence(ctx context.Context, prefix string) (int, error) {
	var latest string
	err := r.session(ctx).Model(&Visit{}).
		Where("visit_no LIKE ?", prefix+"%").
		Order("visit_no DESC").
		Limit(1).
		Pluck("visit_no", &latest).Error
	if err != nil {
		return 0, fmt.Errorf("生成回访单号失败: %w", err)
	}
	if latest == "" {
		return 1, nil
	}
	value, convErr := strconv.Atoi(strings.TrimPrefix(latest, prefix))
	if convErr != nil {
		return 1, nil
	}
	return value + 1, nil
}

// Update 保存回访任务全部字段。
func (r *Repository) Update(ctx context.Context, entity *Visit) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新回访任务失败: %w", err)
	}
	return nil
}

// GetByID 按主键查询回访任务并带出联系记录。
func (r *Repository) GetByID(ctx context.Context, id uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("回访任务不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询回访任务失败: %w", err)
	}
	logs, err := r.ListContactLogs(ctx, entity.ID)
	if err != nil {
		return nil, err
	}
	entity.ContactLogs = logs
	return &entity, nil
}

// GetByRepair 查询某次维修对应的回访任务, 不存在时返回 nil。
func (r *Repository) GetByRepair(ctx context.Context, repairID uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).Where("repair_id = ?", repairID).Order("id DESC").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询维修回访任务失败: %w", err)
	}
	return &entity, nil
}

// LatestByFault 查询某条故障最新一轮回访, 不存在时返回 nil。
func (r *Repository) LatestByFault(ctx context.Context, faultID uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).Where("fault_id = ?", faultID).Order("round DESC, id DESC").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询故障最新回访失败: %w", err)
	}
	return &entity, nil
}

// CountByFault 统计某条故障已生成的回访轮数。
func (r *Repository) CountByFault(ctx context.Context, faultID uint) (int64, error) {
	var count int64
	err := r.session(ctx).Model(&Visit{}).Where("fault_id = ?", faultID).Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("统计故障回访轮数失败: %w", err)
	}
	return count, nil
}

// ListByFault 查询某条故障的全部回访轮次, 按轮次正序, 并带出联系记录。
func (r *Repository) ListByFault(ctx context.Context, faultID uint) ([]Visit, error) {
	entities := make([]Visit, 0)
	err := r.session(ctx).Where("fault_id = ?", faultID).
		Order("round ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询故障回访记录失败: %w", err)
	}
	if len(entities) > 0 {
		ids := make([]uint, 0, len(entities))
		for _, item := range entities {
			ids = append(ids, item.ID)
		}
		logsMap, err := r.batchContactLogs(ctx, ids)
		if err != nil {
			return nil, err
		}
		for index := range entities {
			entities[index].ContactLogs = logsMap[entities[index].ID]
		}
	}
	return entities, nil
}

// List 分页查询回访任务。
func (r *Repository) List(ctx context.Context, filter Filter, page pagination.Query) ([]Visit, int64, error) {
	base := func() *gorm.DB {
		return applyFilter(r.session(ctx).Model(&Visit{}), filter)
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计回访任务失败: %w", err)
	}

	entities := make([]Visit, 0)
	if err := base().Order(page.OrderClause()).Offset(page.Offset()).Limit(page.Limit()).Find(&entities).Error; err != nil {
		return nil, 0, fmt.Errorf("查询回访任务失败: %w", err)
	}
	if len(entities) > 0 {
		ids := make([]uint, 0, len(entities))
		for _, item := range entities {
			ids = append(ids, item.ID)
		}
		logsMap, err := r.batchContactLogs(ctx, ids)
		if err != nil {
			return nil, 0, err
		}
		for index := range entities {
			entities[index].ContactLogs = logsMap[entities[index].ID]
		}
	}
	return entities, total, nil
}

// CreateContactLog 追加一条联系情况明细。
func (r *Repository) CreateContactLog(ctx context.Context, logEntry *VisitContactLog) error {
	if err := r.session(ctx).Create(logEntry).Error; err != nil {
		return fmt.Errorf("记录回访联系情况失败: %w", err)
	}
	return nil
}

// ListContactLogs 查询某次回访的全部联系记录, 按联系时间正序。
func (r *Repository) ListContactLogs(ctx context.Context, visitID uint) ([]VisitContactLog, error) {
	entities := make([]VisitContactLog, 0)
	err := r.session(ctx).Where("visit_id = ?", visitID).
		Order("contacted_at ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询回访联系记录失败: %w", err)
	}
	return entities, nil
}

// batchContactLogs 批量取出多个回访任务的联系记录。
func (r *Repository) batchContactLogs(ctx context.Context, visitIDs []uint) (map[uint][]VisitContactLog, error) {
	entities := make([]VisitContactLog, 0)
	err := r.session(ctx).Where("visit_id IN ?", visitIDs).
		Order("contacted_at ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("批量查询回访联系记录失败: %w", err)
	}
	result := make(map[uint][]VisitContactLog, len(visitIDs))
	for _, item := range entities {
		result[item.VisitID] = append(result[item.VisitID], item)
	}
	return result, nil
}

// Count 统计回访任务总数。
func (r *Repository) Count(ctx context.Context) (int64, error) {
	var total int64
	if err := r.session(ctx).Model(&Visit{}).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("统计回访任务总数失败: %w", err)
	}
	return total, nil
}

// CountByStatus 按状态分组统计。
func (r *Repository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	type row struct {
		Label string
		Total int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Visit{}).
		Select("status AS label, COUNT(*) AS total").
		Group("status").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("回访状态分组统计失败: %w", err)
	}
	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.Label] = item.Total
	}
	return result, nil
}

// CountOverdue 统计已超过回访期限仍未闭环的任务数量。
func (r *Repository) CountOverdue(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	err := r.session(ctx).Model(&Visit{}).
		Where("status IN ? AND due_at < ?", openStatuses, now).
		Count(&total).Error
	if err != nil {
		return 0, fmt.Errorf("统计超期回访失败: %w", err)
	}
	return total, nil
}

// AverageSatisfaction 统计已评定回访的平均满意度。
func (r *Repository) AverageSatisfaction(ctx context.Context) (float64, error) {
	var avg *float64
	err := r.session(ctx).Model(&Visit{}).
		Where("satisfaction IS NOT NULL").
		Select("AVG(satisfaction)").Scan(&avg).Error
	if err != nil {
		return 0, fmt.Errorf("统计平均满意度失败: %w", err)
	}
	if avg == nil {
		return 0, nil
	}
	return *avg, nil
}

// applyFilter 统一拼装回访查询条件。
func applyFilter(statement *gorm.DB, filter Filter) *gorm.DB {
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		statement = statement.Where(
			"visit_no LIKE ? OR fault_no LIKE ? OR lamp_code LIKE ? OR repair_no LIKE ?",
			like, like, like, like,
		)
	}
	if filter.FaultID > 0 {
		statement = statement.Where("fault_id = ?", filter.FaultID)
	}
	if filter.RepairID > 0 {
		statement = statement.Where("repair_id = ?", filter.RepairID)
	}
	if filter.Status != "" {
		statement = statement.Where("status = ?", filter.Status)
	}
	if filter.Qualified != nil {
		statement = statement.Where("qualified = ?", *filter.Qualified)
	}
	if filter.Overdue {
		now := time.Now()
		statement = statement.Where("status IN ? AND due_at < ?", openStatuses, now)
	}
	if filter.CreatedFrom != nil {
		statement = statement.Where("created_at >= ?", *filter.CreatedFrom)
	}
	if filter.CreatedTo != nil {
		statement = statement.Where("created_at < ?", *filter.CreatedTo)
	}
	return statement
}

// isUniqueViolation 兼容 sqlite 与 postgres 的唯一约束冲突判断。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") ||
		strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "unique violation")
}

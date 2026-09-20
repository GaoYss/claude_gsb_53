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

// Filter 是仓储层使用的回访任务查询条件。
type Filter struct {
	Keyword     string
	FaultID     uint
	RepairID    uint
	Status      string
	Result      string
	Round       int
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}

// Repository 负责回访任务的数据访问。
type Repository struct {
	db *gorm.DB
}

// NewRepository 构造回访任务仓储。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) session(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx)
}

// CreateWithUniqueNo 生成唯一回访单号并落库, 单号冲突时自动重试。
func (r *Repository) CreateWithUniqueNo(ctx context.Context, entity *Visit, prefix string) error {
	for attempt := 0; attempt < 5; attempt++ {
		sequence, err := r.NextSequence(ctx, prefix)
		if err != nil {
			return err
		}
		entity.VisitNo = fmt.Sprintf("%s%04d", prefix, sequence+attempt)
		if err = r.create(ctx, entity); err == nil {
			return nil
		} else if !isUniqueViolation(err) {
			return err
		}
	}
	return apperr.Conflict("回访单号生成冲突, 请稍后重试")
}

func (r *Repository) create(ctx context.Context, entity *Visit) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("生成回访任务失败: %w", err)
	}
	return nil
}

// CreateContact 写入一条联系记录。
func (r *Repository) CreateContact(ctx context.Context, entity *VisitContact) error {
	if err := r.session(ctx).Create(entity).Error; err != nil {
		return fmt.Errorf("记录联系情况失败: %w", err)
	}
	return nil
}

// Update 保存回访任务全部字段。
func (r *Repository) Update(ctx context.Context, entity *Visit) error {
	if err := r.session(ctx).Save(entity).Error; err != nil {
		return fmt.Errorf("更新回访任务失败: %w", err)
	}
	return nil
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

// GetByID 按主键查询回访任务。
func (r *Repository) GetByID(ctx context.Context, id uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).First(&entity, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperr.NotFound("回访任务不存在: id=%d", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询回访任务失败: %w", err)
	}
	return &entity, nil
}

// GetByRepair 查询某条维修单对应的回访任务, 不存在时返回 nil。
func (r *Repository) GetByRepair(ctx context.Context, repairID uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).Where("repair_id = ?", repairID).First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询维修单回访任务失败: %w", err)
	}
	return &entity, nil
}

// ListByFault 查询某条故障的全部回访任务, 按轮次正序。
func (r *Repository) ListByFault(ctx context.Context, faultID uint) ([]Visit, error) {
	entities := make([]Visit, 0)
	err := r.session(ctx).Where("fault_id = ?", faultID).Order("round ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询故障回访任务失败: %w", err)
	}
	return entities, nil
}

// ListContacts 查询回访任务的联系记录。
func (r *Repository) ListContacts(ctx context.Context, visitID uint) ([]VisitContact, error) {
	entities := make([]VisitContact, 0)
	err := r.session(ctx).Where("visit_id = ?", visitID).Order("contacted_at ASC, id ASC").Find(&entities).Error
	if err != nil {
		return nil, fmt.Errorf("查询联系记录失败: %w", err)
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
		return nil, 0, fmt.Errorf("查询回访任务列表失败: %w", err)
	}
	return entities, total, nil
}

// LatestByFault 查询某条故障最近一轮回访任务, 不存在时返回 nil。
func (r *Repository) LatestByFault(ctx context.Context, faultID uint) (*Visit, error) {
	var entity Visit
	err := r.session(ctx).Where("fault_id = ?", faultID).Order("round DESC, id DESC").First(&entity).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询最新回访任务失败: %w", err)
	}
	return &entity, nil
}

// CountByColumn 按列分组统计。
func (r *Repository) CountByColumn(ctx context.Context, column string) (map[string]int64, error) {
	type row struct {
		Label string
		Total int64
	}
	rows := make([]row, 0)
	err := r.session(ctx).Model(&Visit{}).
		Select(column + " AS label, COUNT(*) AS total").
		Group(column).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("分组统计 %s 失败: %w", column, err)
	}
	result := make(map[string]int64, len(rows))
	for _, item := range rows {
		result[item.Label] = item.Total
	}
	return result, nil
}

// AverageSatisfaction 统计已完成回访的平均满意度。
func (r *Repository) AverageSatisfaction(ctx context.Context) (float64, error) {
	var avg float64
	err := r.session(ctx).Model(&Visit{}).
		Where("satisfaction IS NOT NULL").
		Select("COALESCE(AVG(satisfaction), 0)").
		Scan(&avg).Error
	if err != nil {
		return 0, fmt.Errorf("统计平均满意度失败: %w", err)
	}
	return avg, nil
}

// DistinctVisitors 返回历史回访人, 用于下拉选项。
func (r *Repository) DistinctVisitors(ctx context.Context) ([]string, error) {
	values := make([]string, 0)
	err := r.session(ctx).Model(&Visit{}).
		Where("visitor <> ''").
		Distinct().
		Order("visitor").
		Pluck("visitor", &values).Error
	if err != nil {
		return nil, fmt.Errorf("查询回访人选项失败: %w", err)
	}
	return values, nil
}

// applyFilter 统一拼装回访任务查询条件。
func applyFilter(statement *gorm.DB, filter Filter) *gorm.DB {
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		statement = statement.Where(
			"visit_no LIKE ? OR fault_no LIKE ? OR lamp_code LIKE ? OR repair_no LIKE ? OR repairman LIKE ?",
			like, like, like, like, like,
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
	if filter.Result != "" {
		statement = statement.Where("result = ?", filter.Result)
	}
	if filter.Round > 0 {
		statement = statement.Where("round = ?", filter.Round)
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

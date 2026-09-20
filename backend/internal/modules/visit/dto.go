package visit

import "streetlight/pkg/pagination"

// ContactRequest 记录一次回访联系情况, 仅追加联系明细, 可多次提交。
type ContactRequest struct {
	ContactResult string `json:"contact_result" binding:"required,oneof=connected no_answer unreachable deferred"`
	ContactName   string `json:"contact_name" binding:"max=64"`
	ContactPhone  string `json:"contact_phone" binding:"max=32"`
	Content       string `json:"content" binding:"omitempty,max=512"`
	ContactedAt   string `json:"contacted_at" binding:"omitempty,max=32"`
}

// ReworkInfo 回访不合格时发起返修所需的信息, 留空字段沿用原维修记录。
type ReworkInfo struct {
	Repairman    string `json:"repairman" binding:"omitempty,max=64"`
	RepairTeam   string `json:"repair_team" binding:"omitempty,max=64"`
	ContactPhone string `json:"contact_phone" binding:"omitempty,max=32"`
	StartedAt    string `json:"started_at" binding:"omitempty,max=32"`
	Content      string `json:"content" binding:"omitempty,max=512"`
	Remark       string `json:"remark" binding:"omitempty,max=255"`
}

// EvaluateRequest 回访评定请求: 登记联系情况、满意度并判定是否合格, 不合格时自动发起返修。
type EvaluateRequest struct {
	ContactResult string      `json:"contact_result" binding:"required,oneof=connected no_answer unreachable deferred"`
	ContactName   string      `json:"contact_name" binding:"max=64"`
	ContactPhone  string      `json:"contact_phone" binding:"max=32"`
	Satisfaction  int         `json:"satisfaction" binding:"required,min=1,max=5"`
	Qualified     *bool       `json:"qualified" binding:"required"`
	Content       string      `json:"content" binding:"omitempty,max=512"`
	Remark        string      `json:"remark" binding:"omitempty,max=255"`
	ContactedAt   string      `json:"contacted_at" binding:"omitempty,max=32"`
	Rework        *ReworkInfo `json:"rework"`
}

// ListQuery 回访任务查询条件。
type ListQuery struct {
	pagination.Params
	Keyword   string `form:"keyword"` // 回访单号 / 故障单号 / 路灯编号 / 维修单号
	Status    string `form:"status"`
	FaultID   uint   `form:"fault_id"`
	RepairID  uint   `form:"repair_id"`
	Qualified string `form:"qualified"` // true / false / 空
	Overdue   bool   `form:"overdue"`   // 仅看已超期未闭环
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
}

// Meta 回访模块字典。
type Meta struct {
	Statuses       []string `json:"statuses"`
	ContactResults []string `json:"contact_results"`
}

// Statistics 回访与返修统计结果。
type Statistics struct {
	Total          int64   `json:"total"`
	PendingTotal   int64   `json:"pending_total"`   // 待回访(含已联系待评定)
	QualifiedTotal int64   `json:"qualified_total"` // 合格闭环
	UnqualifiedTotal int64 `json:"unqualified_total"`
	OverdueTotal   int64   `json:"overdue_total"`
	ReworkTotal    int64   `json:"rework_total"` // 返修次数(来自维修模块)
	QualifiedRate  float64 `json:"qualified_rate"`
	AverageScore   float64 `json:"average_score"`
}

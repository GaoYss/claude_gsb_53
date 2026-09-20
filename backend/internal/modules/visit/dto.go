package visit

import "streetlight/pkg/pagination"

// CompleteRequest 执行回访请求。
// contact_status 为 unreached 时只登记一次联系情况, 回访单保持待回访;
// 为 reached 时必须给出回访结论, 结论为不合格时同时触发返修。
type CompleteRequest struct {
	ContactStatus     string `json:"contact_status" binding:"required,oneof=reached unreached"`
	ContactMethod     string `json:"contact_method" binding:"omitempty,oneof=phone onsite wechat other"`
	ContactPerson     string `json:"contact_person" binding:"omitempty,max=64"`
	Phone             string `json:"phone" binding:"omitempty,max=32"`
	Visitor           string `json:"visitor" binding:"omitempty,max=64"`
	VisitedAt         string `json:"visited_at" binding:"omitempty,max=32"`
	Satisfaction      *int   `json:"satisfaction" binding:"omitempty,min=1,max=5"`
	Result            string `json:"result" binding:"omitempty,oneof=qualified unqualified"`
	UnqualifiedReason string `json:"unqualified_reason" binding:"omitempty,max=255"`
	Feedback          string `json:"feedback" binding:"omitempty,max=512"`
	Remark            string `json:"remark" binding:"omitempty,max=255"`

	// 回访不合格触发返修时的返修派工信息。
	ReworkRepairman string `json:"rework_repairman" binding:"omitempty,max=64"`
	ReworkTeam      string `json:"rework_team" binding:"omitempty,max=64"`
	ReworkContact   string `json:"rework_contact_phone" binding:"omitempty,max=32"`
	ReworkMaterials string `json:"rework_materials" binding:"omitempty,max=255"`
}

// ListQuery 回访任务查询条件。
type ListQuery struct {
	pagination.Params
	Keyword   string `form:"keyword"` // 回访单号 / 故障单号 / 路灯编号 / 维修单号 / 维修人员
	FaultID   uint   `form:"fault_id"`
	RepairID  uint   `form:"repair_id"`
	Status    string `form:"status"`
	Result    string `form:"result"`
	Round     int    `form:"round"`
	StartDate string `form:"start_date"`
	EndDate   string `form:"end_date"`
}

// Meta 回访模块字典。
type Meta struct {
	Statuses       []string `json:"statuses"`
	Results        []string `json:"results"`
	ContactResults []string `json:"contact_results"`
	ContactMethods []string `json:"contact_methods"`
	Visitors       []string `json:"visitors"`
}

// Statistics 回访质量统计。
type Statistics struct {
	Total            int64   `json:"total"`
	PendingTotal     int64   `json:"pending_total"`
	CompletedTotal   int64   `json:"completed_total"`
	QualifiedTotal   int64   `json:"qualified_total"`
	UnqualifiedTotal int64   `json:"unqualified_total"`
	ReworkTotal      int64   `json:"rework_total"` // 返修次数 = 回访不合格次数
	QualifiedRate    float64 `json:"qualified_rate"`
	AvgSatisfaction  float64 `json:"avg_satisfaction"`
}

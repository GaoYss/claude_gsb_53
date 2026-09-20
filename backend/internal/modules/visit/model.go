package visit

import "time"

// 回访任务状态。
const (
	StatusPending    = "pending"     // 待回访
	StatusContacted  = "contacted"   // 已联系, 待评定
	StatusQualified  = "qualified"   // 回访合格
	StatusUnqualified = "unqualified" // 回访不合格(已触发返修)
)

// 联系结果。
const (
	ContactConnected   = "connected"   // 已联系上
	ContactNoAnswer    = "no_answer"   // 无人接听
	ContactUnreachable = "unreachable" // 无法接通
	ContactDeferred    = "deferred"    // 约定再次联系
)

// DefaultDeadline 是完工后生成回访任务的默认回访期限。
const DefaultDeadline = 48 * time.Hour

// Statuses 返回全部回访任务状态。
func Statuses() []string {
	return []string{StatusPending, StatusContacted, StatusQualified, StatusUnqualified}
}

// ContactResults 返回全部联系结果取值。
func ContactResults() []string {
	return []string{ContactConnected, ContactNoAnswer, ContactUnreachable, ContactDeferred}
}

// IsValidStatus 校验回访状态取值。
func IsValidStatus(status string) bool {
	for _, item := range Statuses() {
		if item == status {
			return true
		}
	}
	return false
}

// IsValidContactResult 校验联系结果取值。
func IsValidContactResult(result string) bool {
	for _, item := range ContactResults() {
		if item == result {
			return true
		}
	}
	return false
}

// IsClosed 判断回访是否已终态(合格闭环或不合格已转返修)。
func (v *Visit) IsClosed() bool {
	return v.Status == StatusQualified || v.Status == StatusUnqualified
}

// Visit 维修质量回访任务, 一条记录对应一次维修完工后的一轮回访。
type Visit struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	VisitNo   string `gorm:"size:64;uniqueIndex;not null" json:"visit_no"`
	FaultID   uint   `gorm:"index;not null" json:"fault_id"`
	FaultNo   string `gorm:"size:64;index" json:"fault_no"`
	LampID    uint   `gorm:"index" json:"lamp_id"`
	LampCode  string `gorm:"size:64;index" json:"lamp_code"`
	RepairID  uint   `gorm:"index;not null" json:"repair_id"` // 本轮回访对应的维修(或返修)记录
	RepairNo  string `gorm:"size:64;index" json:"repair_no"`
	Round     int    `gorm:"not null;default:1" json:"round"` // 第几轮回访, 返修后递增
	DueAt     time.Time `gorm:"index;not null" json:"due_at"` // 回访期限
	Status    string    `gorm:"size:32;index;not null;default:pending" json:"status"`

	ContactName   string     `gorm:"size:64" json:"contact_name"`
	ContactPhone  string     `gorm:"size:32" json:"contact_phone"`
	ContactResult string     `gorm:"size:32;index" json:"contact_result"`
	ContactedAt   *time.Time `json:"contacted_at"`
	Satisfaction  *int       `gorm:"index" json:"satisfaction"` // 满意度 1-5
	Qualified     *bool      `gorm:"index" json:"qualified"`
	Content       string     `gorm:"size:512" json:"content"`
	Remark        string     `gorm:"size:255" json:"remark"`

	// 不合格时触发的返修维修单, 返修单是独立的新记录, 原维修记录不被改写。
	ReworkRepairID *uint `gorm:"index" json:"rework_repair_id,omitempty"`
	ReworkRepairNo string `gorm:"size:64" json:"rework_repair_no"`

	// ContactLogs 仅用于响应展示的联系过程记录, 不落库到本表。
	ContactLogs []VisitContactLog `gorm:"-" json:"contact_logs,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Visit) TableName() string { return "quality_visit" }

// VisitContactLog 回访联系情况明细, 一次回访可多次联系。
type VisitContactLog struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	VisitID       uint      `gorm:"index;not null" json:"visit_id"`
	ContactResult string    `gorm:"size:32;not null" json:"contact_result"`
	ContactName   string    `gorm:"size:64" json:"contact_name"`
	ContactPhone  string    `gorm:"size:32" json:"contact_phone"`
	Content       string    `gorm:"size:512" json:"content"`
	ContactedAt   time.Time `gorm:"index;not null" json:"contacted_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// TableName 指定表名。
func (VisitContactLog) TableName() string { return "quality_visit_contact" }

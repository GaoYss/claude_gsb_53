package visit

import "time"

// 回访任务状态。
const (
	StatusPending   = "pending"   // 待回访
	StatusCompleted = "completed" // 已完成
)

// 回访结论。
const (
	ResultQualified   = "qualified"   // 合格(满意)
	ResultUnqualified = "unqualified" // 不合格(触发返修)
)

// 联系结果。
const (
	ContactReached   = "reached"   // 已联系上
	ContactUnreached = "unreached" // 未联系上
)

// 联系方式。
const (
	MethodPhone  = "phone"  // 电话
	MethodOnsite = "onsite" // 现场
	MethodWechat = "wechat" // 微信
	MethodOther  = "other"  // 其它
)

// Statuses 返回全部回访任务状态。
func Statuses() []string {
	return []string{StatusPending, StatusCompleted}
}

// Results 返回全部回访结论。
func Results() []string {
	return []string{ResultQualified, ResultUnqualified}
}

// ContactResults 返回全部联系结果。
func ContactResults() []string {
	return []string{ContactReached, ContactUnreached}
}

// ContactMethods 返回全部联系方式。
func ContactMethods() []string {
	return []string{MethodPhone, MethodOnsite, MethodWechat, MethodOther}
}

// IsValidContactMethod 校验联系方式取值。
func IsValidContactMethod(value string) bool {
	for _, item := range ContactMethods() {
		if item == value {
			return true
		}
	}
	return false
}

// Visit 维修质量回访任务。维修完工(已修复)后自动生成; 回访不合格时触发返修,
// 返修完工后按轮次再生成一条回访任务, 直到回访合格才允许故障结算。
type Visit struct {
	ID      uint   `gorm:"primaryKey" json:"id"`
	VisitNo string `gorm:"size:64;uniqueIndex;not null" json:"visit_no"`

	FaultID  uint   `gorm:"index;not null" json:"fault_id"`
	FaultNo  string `gorm:"size:64;index" json:"fault_no"`
	LampID   uint   `gorm:"index" json:"lamp_id"`
	LampCode string `gorm:"size:64;index" json:"lamp_code"`

	// 本轮回访对应的维修单; OriginRepair 始终指向首次处置的维修单, 用于返修溯源,
	// 返修不会改写首次维修单的完工时间与处置过程。
	RepairID       uint   `gorm:"uniqueIndex;not null" json:"repair_id"`
	RepairNo       string `gorm:"size:64" json:"repair_no"`
	OriginRepairID uint   `gorm:"index;not null" json:"origin_repair_id"`
	OriginRepairNo string `gorm:"size:64" json:"origin_repair_no"`
	Repairman      string `gorm:"size:64" json:"repairman"`

	Round  int    `gorm:"not null;default:1" json:"round"` // 回访轮次, 从 1 开始
	Status string `gorm:"size:32;index;not null;default:pending" json:"status"`
	Result string `gorm:"size:32;index" json:"result"`

	// 不合格回访触发返修后, 记录返修维修单 ID, 返修完工后生成下一轮回访。
	ReworkRepairID *uint `json:"rework_repair_id,omitempty"`

	// 联系情况。
	ContactStatus   string     `gorm:"size:32" json:"contact_status"`
	ContactMethod   string     `gorm:"size:32" json:"contact_method"`
	ContactAttempts int        `gorm:"not null;default:0" json:"contact_attempts"`
	LastContactAt   *time.Time `json:"last_contact_at"`

	// 回访评价。
	Visitor           string     `gorm:"size:64" json:"visitor"`
	VisitedAt         *time.Time `json:"visited_at"`
	Satisfaction      *int       `json:"satisfaction"` // 满意度 1-5
	UnqualifiedReason string     `gorm:"size:255" json:"unqualified_reason"`
	Feedback          string     `gorm:"size:512" json:"feedback"`
	Remark            string     `gorm:"size:255" json:"remark"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (Visit) TableName() string { return "quality_visit" }

// VisitContact 记录单次联系情况, 一张回访单可多次联系(如多次未接通)。
type VisitContact struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	VisitID       uint      `gorm:"index;not null" json:"visit_id"`
	Result        string    `gorm:"size:32;index;not null" json:"result"`
	Method        string    `gorm:"size:32" json:"method"`
	ContactPerson string    `gorm:"size:64" json:"contact_person"`
	Phone         string    `gorm:"size:32" json:"phone"`
	Remark        string    `gorm:"size:255" json:"remark"`
	ContactedAt   time.Time `gorm:"index;not null" json:"contacted_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// TableName 指定表名。
func (VisitContact) TableName() string { return "quality_visit_contact" }

package visit

var statusLabels = map[string]string{
	StatusPending:   "待回访",
	StatusCompleted: "已完成",
}

var resultLabels = map[string]string{
	ResultQualified:   "合格",
	ResultUnqualified: "不合格",
}

var contactStatusLabels = map[string]string{
	ContactReached:   "已联系上",
	ContactUnreached: "未联系上",
}

var contactMethodLabels = map[string]string{
	MethodPhone:  "电话",
	MethodOnsite: "现场",
	MethodWechat: "微信",
	MethodOther:  "其它",
}

// StatusLabel 返回回访任务状态的中文名称。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// ResultLabel 返回回访结论的中文名称。
func ResultLabel(result string) string {
	if label, ok := resultLabels[result]; ok {
		return label
	}
	return result
}

// ContactStatusLabel 返回联系结果的中文名称。
func ContactStatusLabel(value string) string {
	if label, ok := contactStatusLabels[value]; ok {
		return label
	}
	return value
}

// ContactMethodLabel 返回联系方式的中文名称。
func ContactMethodLabel(value string) string {
	if label, ok := contactMethodLabels[value]; ok {
		return label
	}
	return value
}

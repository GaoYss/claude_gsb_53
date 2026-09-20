package visit

var statusLabels = map[string]string{
	StatusPending:     "待回访",
	StatusContacted:   "已联系待评定",
	StatusQualified:   "回访合格",
	StatusUnqualified: "回访不合格",
}

var contactLabels = map[string]string{
	ContactConnected:   "已联系上",
	ContactNoAnswer:    "无人接听",
	ContactUnreachable: "无法接通",
	ContactDeferred:    "约定再联",
}

// StatusLabel 返回回访状态的中文名称。
func StatusLabel(status string) string {
	if label, ok := statusLabels[status]; ok {
		return label
	}
	return status
}

// ContactLabel 返回联系结果的中文名称。
func ContactLabel(result string) string {
	if label, ok := contactLabels[result]; ok {
		return label
	}
	return result
}

package bootstrap

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"streetlight/internal/modules/fault"
	"streetlight/internal/modules/lamp"
	"streetlight/internal/modules/repair"
	"streetlight/internal/modules/visit"
)

const hour = time.Hour

// seedRepairCase 描述一条演示维修记录, 时间字段为距当前时刻的时长。
type seedRepairCase struct {
	repairman   string
	team        string
	startedAgo  time.Duration
	finishedAgo time.Duration // 为 0 表示仍在维修中
	result      string
	content     string
	materials   string
	cost        float64
	isRework    bool // 是否为回访不合格触发的返修单
}

// seedFaultCase 描述一条演示故障记录。
type seedFaultCase struct {
	lampIndex   int
	faultType   string
	level       string
	source      string
	description string
	reporter    string
	reportedAgo time.Duration
	status      string
	closed      bool
	visitState  string // 回访场景: 空/qualified=合格, contacted=已联系待评定, pending=待回访(超期), rework=返修链路
	repairs     []seedRepairCase
}

// seed 在数据库为空时写入演示数据, 便于启动后立即体验完整业务流程。
func seed(db *gorm.DB) error {
	var count int64
	if err := db.Model(&lamp.Lamp{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := time.Now()
	lamps := buildSeedLamps(now)
	if err := db.Create(&lamps).Error; err != nil {
		return fmt.Errorf("写入路灯台账演示数据失败: %w", err)
	}

	cases := seedFaultCases()
	faults := make([]fault.Fault, 0, len(cases))
	sequences := map[string]int{}
	for _, item := range cases {
		device := lamps[item.lampIndex]
		reportedAt := now.Add(-item.reportedAgo)
		prefix := "GD" + reportedAt.Format("20060102")
		sequences[prefix]++

		faults = append(faults, fault.Fault{
			FaultNo:       fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
			LampID:        device.ID,
			LampCode:      device.Code,
			RoadName:      device.RoadName,
			FaultType:     item.faultType,
			FaultLevel:    item.level,
			Source:        item.source,
			Description:   item.description,
			Reporter:      item.reporter,
			ReporterPhone: "13800001234",
			ReportedAt:    reportedAt,
			Status:        item.status,
		})
	}
	if err := db.Create(&faults).Error; err != nil {
		return fmt.Errorf("写入故障演示数据失败: %w", err)
	}

	repairs := make([]repair.Repair, 0)
	repairRanges := make([][2]int, len(cases))
	sequences = map[string]int{}
	for index, item := range cases {
		device := lamps[item.lampIndex]
		start := len(repairs)
		for _, expect := range item.repairs {
			startedAt := now.Add(-expect.startedAgo)
			prefix := "WX" + startedAt.Format("20060102")
			sequences[prefix]++

			record := repair.Repair{
				RepairNo:     fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
				FaultID:      faults[index].ID,
				FaultNo:      faults[index].FaultNo,
				LampID:       device.ID,
				LampCode:     device.Code,
				Repairman:    expect.repairman,
				RepairTeam:   expect.team,
				ContactPhone: "13900005678",
				StartedAt:    startedAt,
				Status:       repair.StatusOngoing,
				Content:      expect.content,
				Materials:    expect.materials,
				Cost:         expect.cost,
			}
			if expect.finishedAgo > 0 {
				finishedAt := now.Add(-expect.finishedAgo)
				record.FinishedAt = &finishedAt
				record.Status = repair.StatusFinished
				record.Result = expect.result
			}
			repairs = append(repairs, record)
		}
		repairRanges[index] = [2]int{start, len(repairs)}
	}
	if len(repairs) > 0 {
		if err := db.Create(&repairs).Error; err != nil {
			return fmt.Errorf("写入维修记录演示数据失败: %w", err)
		}
	}

	// 回填返修单与原维修记录的关联(返修是独立的新记录, 原记录保持不变)。
	for index, item := range cases {
		if item.visitState != "rework" {
			continue
		}
		start := repairRanges[index][0]
		originalID := repairs[start].ID
		for offset, expect := range item.repairs {
			if !expect.isRework {
				continue
			}
			reworkID := repairs[start+offset].ID
			if err := db.Model(&repair.Repair{}).Where("id = ?", reworkID).
				Update("original_repair_id", originalID).Error; err != nil {
				return fmt.Errorf("回填返修关联失败: %w", err)
			}
		}
	}

	visits := buildSeedVisits(now, cases, faults, repairs, repairRanges)
	if len(visits) > 0 {
		if err := db.Create(&visits).Error; err != nil {
			return fmt.Errorf("写入质量回访演示数据失败: %w", err)
		}
		visitLogs := buildSeedVisitLogs(now, visits)
		if len(visitLogs) > 0 {
			if err := db.Create(&visitLogs).Error; err != nil {
				return fmt.Errorf("写入回访联系记录演示数据失败: %w", err)
			}
		}
	}

	for index, item := range cases {
		start, end := repairRanges[index][0], repairRanges[index][1]
		columns := map[string]any{"repair_count": end - start}
		if end > start {
			columns["latest_repair_id"] = repairs[end-1].ID
		}
		if item.closed {
			closedAt := now.Add(-item.reportedAgo / 2)
			columns["closed_at"] = closedAt
			columns["close_remark"] = "现场已恢复照明并复核确认, 故障闭环"
		}
		if err := db.Model(&fault.Fault{}).Where("id = ?", faults[index].ID).Updates(columns).Error; err != nil {
			return fmt.Errorf("回填故障演示数据失败: %w", err)
		}
	}

	if err := syncSeedLampStatus(db, faults, lamps); err != nil {
		return err
	}

	slog.Info("演示数据初始化完成",
		"路灯", len(lamps),
		"故障", len(faults),
		"维修记录", len(repairs),
	)
	return nil
}

// buildSeedVisits 依据故障的回访场景生成演示回访任务。
//
// 覆盖: qualified=合格闭环; contacted=已联系待评定; overdue=超期未回访;
// rework=首轮不合格触发返修、返修后重新回访合格(两轮)。
func buildSeedVisits(now time.Time, cases []seedFaultCase, faults []fault.Fault, repairs []repair.Repair, ranges [][2]int) []visit.Visit {
	result := make([]visit.Visit, 0)
	sequences := map[string]int{}

	newVisit := func(base repair.Repair, round int, dueAt time.Time) visit.Visit {
		prefixDate := base.StartedAt
		createdAt := base.StartedAt
		if base.FinishedAt != nil {
			prefixDate = *base.FinishedAt
			createdAt = *base.FinishedAt
		}
		prefix := "HF" + prefixDate.Format("20060102")
		sequences[prefix]++
		return visit.Visit{
			VisitNo:  fmt.Sprintf("%s%04d", prefix, sequences[prefix]),
			FaultID:  base.FaultID,
			FaultNo:  base.FaultNo,
			LampID:   base.LampID,
			LampCode: base.LampCode,
			RepairID: base.ID,
			RepairNo: base.RepairNo,
			Round:    round,
			DueAt:    dueAt,
			Status:   visit.StatusPending,
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
	}

	for index, item := range cases {
		state := item.visitState
		if state == "" {
			continue
		}
		start, end := ranges[index][0], ranges[index][1]
		caseRepairs := repairs[start:end]

		// 找到最近一次"已修复完工"的维修记录作为回访对象。
		lastFixed := -1
		for offset := len(caseRepairs) - 1; offset >= 0; offset-- {
			if caseRepairs[offset].Status == repair.StatusFinished && caseRepairs[offset].Result == repair.ResultFixed {
				lastFixed = offset
				break
			}
		}
		if lastFixed < 0 || caseRepairs[lastFixed].FinishedAt == nil {
			continue
		}
		fixedRecord := caseRepairs[lastFixed]
		dueAt := fixedRecord.FinishedAt.Add(visit.DefaultDeadline)

		switch state {
		case "rework":
			// 首轮回访不合格, 关联返修单。
			first := newVisit(caseRepairs[0], 1, caseRepairs[0].FinishedAt.Add(visit.DefaultDeadline))
			contactedAt := now.Add(-19 * hour)
			score := 2
			unqualified := false
			first.Status = visit.StatusUnqualified
			first.ContactResult = visit.ContactConnected
			first.ContactName = "张伟"
			first.ContactPhone = "13800001234"
			first.ContactedAt = &contactedAt
			first.Satisfaction = &score
			first.Qualified = &unqualified
			first.Content = "回访发现当晚仍有灯具不亮, 维修效果未达预期"
			reworkID := fixedRecord.ID
			first.ReworkRepairID = &reworkID
			first.ReworkRepairNo = fixedRecord.RepairNo
			result = append(result, first)

			// 返修完工后第二轮回访合格。
			second := newVisit(fixedRecord, 2, dueAt)
			contactedAt2 := now.Add(-13 * hour)
			score2 := 5
			qualified := true
			second.Status = visit.StatusQualified
			second.ContactResult = visit.ContactConnected
			second.ContactName = "张伟"
			second.ContactPhone = "13800001234"
			second.ContactedAt = &contactedAt2
			second.Satisfaction = &score2
			second.Qualified = &qualified
			second.Content = "返修后连续两晚照明正常, 回访合格"
			result = append(result, second)

		case "qualified":
			entity := newVisit(fixedRecord, 1, dueAt)
			contactedAt := fixedRecord.FinishedAt.Add(2 * time.Hour)
			score := 5
			qualified := true
			entity.Status = visit.StatusQualified
			entity.ContactResult = visit.ContactConnected
			entity.ContactName = item.reporter
			entity.ContactPhone = "13800001234"
			entity.ContactedAt = &contactedAt
			entity.Satisfaction = &score
			entity.Qualified = &qualified
			entity.Content = "现场已恢复正常, 对维修结果满意"
			result = append(result, entity)

		case "contacted":
			entity := newVisit(fixedRecord, 1, dueAt)
			contactedAt := now.Add(-2 * hour)
			entity.Status = visit.StatusContacted
			entity.ContactResult = visit.ContactDeferred
			entity.ContactName = item.reporter
			entity.ContactPhone = "13800001234"
			entity.ContactedAt = &contactedAt
			entity.Content = "已联系, 居民表示晚间再观察一晚, 约定明日评定"
			result = append(result, entity)

		case "overdue":
			// 完工已超过回访期限仍未联系, 任务保持待回访且 due_at 在过去。
			entity := newVisit(fixedRecord, 1, dueAt)
			result = append(result, entity)
		}
	}
	return result
}

// buildSeedVisitLogs 为已产生联系的回访任务补充联系情况明细, 与任务结论保持一致。
func buildSeedVisitLogs(now time.Time, visits []visit.Visit) []visit.VisitContactLog {
	logs := make([]visit.VisitContactLog, 0, len(visits))
	for _, item := range visits {
		if item.ContactedAt == nil {
			continue
		}
		result := item.ContactResult
		if result == "" {
			result = visit.ContactConnected
		}
		logs = append(logs, visit.VisitContactLog{
			VisitID:       item.ID,
			ContactResult: result,
			ContactName:   item.ContactName,
			ContactPhone:  item.ContactPhone,
			Content:       item.Content,
			ContactedAt:   *item.ContactedAt,
		})

		// 不合格场景补一条首次未接通的联系记录, 体现多次联系过程。
		if item.Status == visit.StatusUnqualified {
			logs = append(logs, visit.VisitContactLog{
				VisitID:       item.ID,
				ContactResult: visit.ContactNoAnswer,
				ContactName:   item.ContactName,
				ContactPhone:  item.ContactPhone,
				Content:       "首次拨打无人接听",
				ContactedAt:   item.ContactedAt.Add(-30 * time.Minute),
			})
		}
	}
	return logs
}

// buildSeedLamps 生成 6 条道路共 30 盏路灯的台账数据。
func buildSeedLamps(now time.Time) []lamp.Lamp {
	roads := []struct {
		name     string
		district string
	}{
		{"中山路", "城东区"},
		{"建设大道", "城东区"},
		{"园区北路", "高新区"},
		{"滨江路", "城西区"},
		{"解放路", "城西区"},
		{"学院路", "高新区"},
	}
	types := []string{lamp.LampTypeLED, lamp.LampTypeSodium, lamp.LampTypeMetal, lamp.LampTypeSolar}
	powers := []int{60, 100, 150, 200, 250}

	result := make([]lamp.Lamp, 0, len(roads)*5)
	index := 0
	for roadIndex, road := range roads {
		for position := 0; position < 5; position++ {
			index++
			installDate := time.Date(
				now.Year()-1-roadIndex%2, time.Month(1+position), 15,
				0, 0, 0, 0, now.Location(),
			)
			result = append(result, lamp.Lamp{
				Code:        fmt.Sprintf("LD-%05d", index),
				Name:        fmt.Sprintf("%s%d号灯杆", road.name, position+1),
				RoadName:    road.name,
				District:    road.district,
				Address:     fmt.Sprintf("%s%d号", road.name, (position+1)*100),
				Longitude:   116.40 + float64(roadIndex)*0.01 + float64(position)*0.001,
				Latitude:    39.90 + float64(roadIndex)*0.01 + float64(position)*0.001,
				LampType:    types[(roadIndex+position)%len(types)],
				Power:       powers[(roadIndex+position)%len(powers)],
				PoleHeight:  8 + float64(position%3)*1.5,
				RunStatus:   lamp.RunStatusNormal,
				InstallDate: &installDate,
				Remark:      "演示数据",
			})
		}
	}
	return result
}

// seedFaultCases 返回演示故障场景, 覆盖待处理 / 维修中 / 已修复 / 已关闭四种状态。
func seedFaultCases() []seedFaultCase {
	return []seedFaultCase{
		{
			lampIndex: 0, faultType: "灯不亮", level: fault.LevelHigh, source: fault.SourceInspection,
			description: "夜间巡检发现整灯不亮, 相邻灯杆照明正常", reporter: "王建国",
			reportedAgo: 6 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 1, faultType: "灯光闪烁", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "市民来电反馈该路段灯光持续闪烁, 影响行车视线", reporter: "李梅",
			reportedAgo: 30 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 2, faultType: "灯具常亮", level: fault.LevelLow, source: fault.SourceMonitoring,
			description: "控制平台监测到该灯具白天仍处于点亮状态", reporter: "监控中心",
			reportedAgo: 40 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 3, faultType: "线路故障", level: fault.LevelUrgent, source: fault.SourceInspection,
			description: "电缆接头烧蚀, 该支路 3 盏路灯同时失电", reporter: "赵强",
			reportedAgo: 3 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 2 * hour,
					content: "已到场排查, 确认电缆接头烧蚀, 正在更换接头", materials: "防水接头 2 套",
					cost: 180,
				},
			},
		},
		{
			lampIndex: 4, faultType: "控制箱故障", level: fault.LevelHigh, source: fault.SourceMonitoring,
			description: "控制箱通讯中断, 远程无法下发开关灯指令", reporter: "监控中心",
			reportedAgo: 5 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明二班", startedAgo: 1 * hour,
					content: "检查控制箱通讯模块, 疑似模块损坏", materials: "通讯模块 1 个", cost: 260,
				},
			},
		},
		{
			lampIndex: 5, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "该灯杆夜间不亮, 疑似驱动电源损坏", reporter: "张伟",
			reportedAgo: 26 * hour, status: fault.StatusRepaired, visitState: "rework",
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 25 * hour, finishedAgo: 20 * hour,
					result: repair.ResultFixed, content: "更换 LED 驱动电源并复测绝缘",
					materials: "驱动电源 1 个", cost: 220,
				},
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 18 * hour, finishedAgo: 14 * hour,
					result: repair.ResultFixed, content: "返修: 复检发现接线端子松动, 重新压接并试亮",
					materials: "接线端子 2 个", cost: 30, isRework: true,
				},
			},
		},
		{
			lampIndex: 6, faultType: "灯具破损", level: fault.LevelNormal, source: fault.SourceInspection,
			description: "灯具外罩被外物击碎, 需整体更换灯头", reporter: "王建国",
			reportedAgo: 50 * hour, status: fault.StatusRepaired, visitState: "qualified",
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 48 * hour, finishedAgo: 44 * hour,
					result: repair.ResultFixed, content: "更换灯头总成并密封处理",
					materials: "LED 灯头 1 套", cost: 460,
				},
			},
		},
		{
			lampIndex: 7, faultType: "灯杆倾斜", level: fault.LevelHigh, source: fault.SourceCitizen,
			description: "车辆剐蹭导致灯杆倾斜约 8 度, 存在安全隐患", reporter: "孙倩",
			reportedAgo: 72 * hour, status: fault.StatusClosed, closed: true, visitState: "qualified",
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 70 * hour, finishedAgo: 60 * hour,
					result: repair.ResultFixed, content: "重新浇筑基础法兰并校正灯杆垂直度",
					materials: "基础法兰 1 套", cost: 980,
				},
			},
		},
		{
			lampIndex: 8, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceMonitoring,
			description: "平台告警该灯杆回路电流为零", reporter: "监控中心",
			reportedAgo: 96 * hour, status: fault.StatusClosed, closed: true, visitState: "qualified",
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明一班", startedAgo: 94 * hour, finishedAgo: 90 * hour,
					result: repair.ResultFixed, content: "更换熔断器并紧固接线端子",
					materials: "熔断器 1 只", cost: 60,
				},
			},
		},
		{
			lampIndex: 9, faultType: "灯光闪烁", level: fault.LevelNormal, source: fault.SourceInspection,
			description: "灯具有明显频闪, 疑似驱动电源老化", reporter: "王建国",
			reportedAgo: 120 * hour, status: fault.StatusClosed, closed: true, visitState: "qualified",
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 118 * hour, finishedAgo: 112 * hour,
					result: repair.ResultFixed, content: "更换驱动电源, 频闪消除",
					materials: "驱动电源 1 个", cost: 220,
				},
			},
		},
		{
			lampIndex: 10, faultType: "线路故障", level: fault.LevelUrgent, source: fault.SourceInspection,
			description: "地埋电缆绝缘老化, 绝缘电阻不达标", reporter: "赵强",
			reportedAgo: 150 * hour, status: fault.StatusClosed, closed: true, visitState: "qualified",
			repairs: []seedRepairCase{
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 148 * hour, finishedAgo: 140 * hour,
					result: repair.ResultPendingParts, content: "检测确认需整段更换电缆, 等待物料到场",
					materials: "电缆 40 米", cost: 120,
				},
				{
					repairman: "周涛", team: "市政照明二班", startedAgo: 130 * hour, finishedAgo: 120 * hour,
					result: repair.ResultFixed, content: "更换老化电缆并做绝缘测试, 测试合格",
					materials: "电缆 40 米, 热缩管 4 套", cost: 1560,
				},
			},
		},
		{
			lampIndex: 11, faultType: "灯具常亮", level: fault.LevelLow, source: fault.SourceOther,
			description: "白天常亮, 疑似接触器粘连", reporter: "社区网格员",
			reportedAgo: 10 * hour, status: fault.StatusRepaired, visitState: "contacted",
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明二班", startedAgo: 9 * hour, finishedAgo: 7 * hour,
					result: repair.ResultFixed, content: "更换接触器, 恢复远程开关灯控制",
					materials: "交流接触器 1 只", cost: 150,
				},
			},
		},
		{
			lampIndex: 12, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "居民反映该灯杆连续两晚不亮", reporter: "李梅",
			reportedAgo: 8 * hour, status: fault.StatusPending,
		},
		{
			lampIndex: 13, faultType: "控制箱故障", level: fault.LevelHigh, source: fault.SourceMonitoring,
			description: "控制箱电流异常波动, 疑似内部接触不良", reporter: "监控中心",
			reportedAgo: 15 * hour, status: fault.StatusProcessing,
			repairs: []seedRepairCase{
				{
					repairman: "刘志强", team: "市政照明一班", startedAgo: 12 * hour,
					content: "拆检控制箱, 正在逐路测量回路电流", materials: "万用表、绝缘胶带", cost: 40,
				},
			},
		},
		{
			lampIndex: 14, faultType: "灯不亮", level: fault.LevelNormal, source: fault.SourceCitizen,
			description: "灯具时亮时灭, 已更换光源但仍未回访确认", reporter: "周敏",
			reportedAgo: 60 * hour, status: fault.StatusRepaired, visitState: "overdue",
			repairs: []seedRepairCase{
				{
					repairman: "陈鹏", team: "市政照明二班", startedAgo: 58 * hour, finishedAgo: 54 * hour,
					result: repair.ResultFixed, content: "更换灯具光源模组并试亮",
					materials: "LED 光源模组 1 套", cost: 190,
				},
			},
		},
	}
}

// syncSeedLampStatus 依据演示故障数据回填路灯运行状态, 保证台账与故障一致。
func syncSeedLampStatus(db *gorm.DB, faults []fault.Fault, lamps []lamp.Lamp) error {
	statuses := map[uint]string{}
	for _, item := range faults {
		switch item.Status {
		case fault.StatusProcessing:
			statuses[item.LampID] = lamp.RunStatusMaintenance
		case fault.StatusPending:
			if statuses[item.LampID] != lamp.RunStatusMaintenance {
				statuses[item.LampID] = lamp.RunStatusFault
			}
		}
	}

	for index, device := range lamps {
		if index >= 18 && index%5 == 4 {
			statuses[device.ID] = lamp.RunStatusOffline
		}
	}

	for lampID, status := range statuses {
		err := db.Model(&lamp.Lamp{}).Where("id = ?", lampID).Update("run_status", status).Error
		if err != nil {
			return fmt.Errorf("同步演示路灯运行状态失败: %w", err)
		}
	}
	return nil
}

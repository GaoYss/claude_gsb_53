<template>
  <div class="page">
    <PageHeader title="维修质量回访" description="完工后自动生成回访任务, 记录联系情况与满意度; 不合格触发返修并重新回访, 回访未完成不可结算">
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </PageHeader>

    <div class="card-grid">
      <StatCard label="待回访" :value="stats.pending_total" suffix="单" icon="Bell" color="#e6a23c" hint="需尽快联系报修人" />
      <StatCard label="回访合格" :value="stats.qualified_total" suffix="单" icon="CircleCheck" color="#67c23a" :hint="`合格率 ${percent(stats.qualified_rate)}`" />
      <StatCard label="回访不合格" :value="stats.unqualified_total" suffix="单" icon="CircleClose" color="#f56c6c" hint="均已触发返修" />
      <StatCard label="返修次数" :value="stats.rework_total" suffix="次" icon="RefreshRight" color="#909399" hint="进入运行概览统计" />
      <StatCard label="平均满意度" :value="stats.avg_satisfaction" suffix="分" icon="Star" color="#409eff" hint="按已完成回访计算" />
    </div>

    <el-card shadow="never">
      <div class="filter-bar">
        <el-input v-model="query.keyword" placeholder="回访单号 / 故障单号 / 路灯编号 / 维修单号 / 维修人员" clearable @keyup.enter="handleSearch" />
        <el-select v-model="query.status" placeholder="回访状态" clearable @change="handleSearch">
          <el-option v-for="(item, key) in VISIT_STATUS" :key="key" :label="item.label" :value="key" />
        </el-select>
        <el-select v-model="query.result" placeholder="回访结论" clearable @change="handleSearch">
          <el-option v-for="(item, key) in VISIT_RESULT" :key="key" :label="item.label" :value="key" />
        </el-select>
        <el-select v-model="query.round" placeholder="回访轮次" clearable @change="handleSearch">
          <el-option :value="1" label="首轮回访" />
          <el-option :value="2" label="第 2 轮（返修后）" />
          <el-option :value="3" label="第 3 轮" />
        </el-select>
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          value-format="YYYY-MM-DD"
          range-separator="至"
          start-placeholder="生成开始日期"
          end-placeholder="生成结束日期"
          @change="handleSearch"
        />
        <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
        <el-button :icon="RefreshLeft" @click="handleReset">重置</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="visit_no" label="回访单号" width="140" fixed="left" />
        <el-table-column label="轮次" width="90" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="row.round > 1 ? 'warning' : 'info'" effect="plain">第 {{ row.round }} 轮</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="fault_no" label="故障单号" width="140" />
        <el-table-column prop="lamp_code" label="路灯编号" width="100" />
        <el-table-column prop="repair_no" label="维修单号" width="140" />
        <el-table-column prop="repairman" label="维修人员" width="100" />
        <el-table-column label="回访状态" width="90">
          <template #default="{ row }"><StatusTag :dict="VISIT_STATUS" :value="row.status" /></template>
        </el-table-column>
        <el-table-column label="结论" width="90">
          <template #default="{ row }">
            <StatusTag v-if="row.result" :dict="VISIT_RESULT" :value="row.result" />
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="满意度" width="120">
          <template #default="{ row }">
            <el-rate v-if="row.satisfaction" :model-value="row.satisfaction" disabled />
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="联系次数" width="90" align="center">
          <template #default="{ row }">{{ row.contact_attempts || 0 }}</template>
        </el-table-column>
        <el-table-column label="生成时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.created_at) }}</template>
        </el-table-column>
        <el-table-column label="回访时间" width="150">
          <template #default="{ row }">{{ formatDateTime(row.visited_at) }}</template>
        </el-table-column>
        <el-table-column prop="unqualified_reason" label="不合格原因 / 反馈" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.unqualified_reason || row.feedback || '-' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button v-if="row.status === 'pending'" link type="success" @click="openComplete(row)">执行回访</el-button>
            <el-button link type="info" @click="goTrack(row)">链路</el-button>
          </template>
        </el-table-column>
      </el-table>
      <DataPagination
        :page="query.page"
        :page-size="query.page_size"
        :total="total"
        @page-change="changePage"
        @size-change="changePageSize"
      />
    </el-card>

    <CompleteVisitDialog v-model="completeVisible" :model="completing" :visitors="visitors" @saved="handleSaved" />
    <VisitDetailDrawer v-model="detailVisible" :visit-id="activeVisitId" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatCard from '@/components/common/StatCard.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import CompleteVisitDialog from './components/CompleteVisitDialog.vue'
import VisitDetailDrawer from './components/VisitDetailDrawer.vue'
import { visitApi } from '@/api/visit'
import { VISIT_RESULT, VISIT_STATUS } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'
import { useListPage } from '@/composables/useListPage'

const router = useRouter()

const { loading, rows, total, query, load, search, reset, changePage, changePageSize } = useListPage(visitApi.list, {
  keyword: '',
  status: '',
  result: '',
  round: '',
  start_date: '',
  end_date: '',
})

const dateRange = ref([])
const completeVisible = ref(false)
const detailVisible = ref(false)
const completing = ref(null)
const activeVisitId = ref(null)
const visitors = ref([])
const stats = ref({ pending_total: 0, qualified_total: 0, unqualified_total: 0, rework_total: 0, qualified_rate: 0, avg_satisfaction: 0 })

function percent(value) {
  return `${(Number(value ?? 0) * 100).toFixed(0)}%`
}

function applyDateRange() {
  query.start_date = dateRange.value?.[0] ?? ''
  query.end_date = dateRange.value?.[1] ?? ''
}

function handleSearch() {
  applyDateRange()
  search()
}

function handleReset() {
  dateRange.value = []
  reset()
}

function openComplete(row) {
  completing.value = { ...row }
  completeVisible.value = true
}

function openDetail(row) {
  activeVisitId.value = row.id
  detailVisible.value = true
}

function goTrack(row) {
  router.push({ path: '/status/track', query: { fault_no: row.fault_no } })
}

async function loadStats() {
  try {
    stats.value = await visitApi.statistics()
  } catch (error) {
    // 保持零值
  }
}

async function loadMeta() {
  try {
    const meta = await visitApi.meta()
    visitors.value = meta.visitors ?? []
  } catch (error) {
    visitors.value = []
  }
}

function handleSaved() {
  load()
  loadStats()
  loadMeta()
}

onMounted(() => {
  loadStats()
  loadMeta()
})
</script>

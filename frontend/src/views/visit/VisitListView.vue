<template>
  <div class="page">
    <PageHeader title="维修质量回访" description="完工后自动生成回访任务, 记录联系情况与满意度; 不合格自动触发返修并重新回访, 回访未合格不可结算">
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </PageHeader>

    <el-card shadow="never">
      <div class="filter-bar">
        <el-input v-model="query.keyword" placeholder="回访单号 / 故障单号 / 维修单号 / 路灯编号" clearable @keyup.enter="handleSearch" />
        <el-select v-model="query.status" placeholder="回访状态" clearable @change="handleSearch">
          <el-option v-for="(item, key) in VISIT_STATUS" :key="key" :label="item.label" :value="key" />
        </el-select>
        <el-select v-model="qualifiedFilter" placeholder="评定结果" clearable @change="handleQualifiedFilter">
          <el-option label="合格" value="true" />
          <el-option label="不合格" value="false" />
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
        <el-checkbox v-model="query.overdue" @change="handleSearch">仅看超期未闭环</el-checkbox>
        <el-button type="primary" :icon="Search" @click="handleSearch">查询</el-button>
        <el-button :icon="RefreshLeft" @click="handleReset">重置</el-button>
      </div>
    </el-card>

    <el-card shadow="never">
      <el-table v-loading="loading" :data="rows" stripe>
        <el-table-column prop="visit_no" label="回访单号" width="150" fixed="left" />
        <el-table-column label="轮次" width="70" align="center">
          <template #default="{ row }">第{{ row.round }}轮</template>
        </el-table-column>
        <el-table-column prop="fault_no" label="故障单号" width="140" />
        <el-table-column prop="repair_no" label="维修单号" width="140" />
        <el-table-column prop="lamp_code" label="路灯编号" width="110" />
        <el-table-column label="状态" width="120">
          <template #default="{ row }"><StatusTag :dict="VISIT_STATUS" :value="row.status" /></template>
        </el-table-column>
        <el-table-column label="回访期限" width="160">
          <template #default="{ row }">
            <span :class="{ 'overdue-text': isOverdue(row) }">{{ formatDateTime(row.due_at) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="最后联系" width="160">
          <template #default="{ row }">{{ formatDateTime(row.contacted_at) }}</template>
        </el-table-column>
        <el-table-column label="联系结果" width="100">
          <template #default="{ row }">
            <StatusTag v-if="row.contact_result" :dict="VISIT_CONTACT" :value="row.contact_result" />
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="满意度" width="150">
          <template #default="{ row }">
            <el-rate v-if="row.satisfaction" :model-value="row.satisfaction" disabled />
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="返修单" width="140">
          <template #default="{ row }">
            <el-button v-if="row.rework_repair_no" link type="warning" @click="goRework(row)">{{ row.rework_repair_no }}</el-button>
            <span v-else class="text-muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="230" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDetail(row)">详情</el-button>
            <el-button v-if="!isClosed(row)" link type="info" @click="openContact(row)">记录联系</el-button>
            <el-button v-if="!isClosed(row)" link type="success" @click="openEvaluate(row)">回访评定</el-button>
            <el-button link type="warning" @click="goTrack(row)">链路</el-button>
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

    <VisitContactDialog v-model="contactVisible" :model="contacting" @saved="handleSaved" />
    <VisitEvaluateDialog v-model="evaluateVisible" :model="evaluating" @saved="handleSaved" />
    <VisitDetailDrawer v-model="detailVisible" :visit-id="activeVisitId" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, RefreshLeft, Search } from '@element-plus/icons-vue'
import PageHeader from '@/components/common/PageHeader.vue'
import StatusTag from '@/components/common/StatusTag.vue'
import DataPagination from '@/components/common/DataPagination.vue'
import VisitContactDialog from './components/VisitContactDialog.vue'
import VisitEvaluateDialog from './components/VisitEvaluateDialog.vue'
import VisitDetailDrawer from './components/VisitDetailDrawer.vue'
import { visitApi } from '@/api/visit'
import { VISIT_CONTACT, VISIT_STATUS } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'
import { useListPage } from '@/composables/useListPage'

const router = useRouter()

const { loading, rows, total, query, load, search, reset, changePage, changePageSize } = useListPage(visitApi.list, {
  keyword: '',
  status: '',
  qualified: '',
  overdue: false,
  start_date: '',
  end_date: '',
})

const dateRange = ref([])
const qualifiedFilter = ref('')
const contactVisible = ref(false)
const evaluateVisible = ref(false)
const detailVisible = ref(false)
const contacting = ref(null)
const evaluating = ref(null)
const activeVisitId = ref(null)

const isClosed = (row) => row.status === 'qualified' || row.status === 'unqualified'
const isOverdue = (row) => !isClosed(row) && row.due_at && new Date(row.due_at).getTime() < Date.now()

function applyDateRange() {
  query.start_date = dateRange.value?.[0] ?? ''
  query.end_date = dateRange.value?.[1] ?? ''
}

function handleSearch() {
  applyDateRange()
  search()
}

function handleQualifiedFilter() {
  query.qualified = qualifiedFilter.value
  search()
}

function handleReset() {
  dateRange.value = []
  qualifiedFilter.value = ''
  reset()
}

function openContact(row) {
  contacting.value = { ...row }
  contactVisible.value = true
}

function openEvaluate(row) {
  evaluating.value = { ...row }
  evaluateVisible.value = true
}

function openDetail(row) {
  activeVisitId.value = row.id
  detailVisible.value = true
}

function goTrack(row) {
  router.push({ path: '/status/track', query: { fault_no: row.fault_no } })
}

function goRework(row) {
  router.push({ path: '/repairs', query: { keyword: row.rework_repair_no } })
}

function handleSaved() {
  load()
}
</script>

<style scoped>
.overdue-text {
  color: var(--el-color-danger);
  font-weight: 600;
}
</style>

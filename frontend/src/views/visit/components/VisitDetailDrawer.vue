<template>
  <el-drawer
    :model-value="modelValue"
    title="质量回访详情"
    size="560px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div v-loading="loading">
      <template v-if="detail.visit_no">
        <el-descriptions :column="2" border size="small" title="回访信息">
          <el-descriptions-item label="回访单号">{{ detail.visit_no }}</el-descriptions-item>
          <el-descriptions-item label="回访轮次">第 {{ detail.round }} 轮</el-descriptions-item>
          <el-descriptions-item label="回访状态">
            <StatusTag :dict="VISIT_STATUS" :value="detail.status" />
          </el-descriptions-item>
          <el-descriptions-item label="回访结论">
            <StatusTag v-if="detail.result" :dict="VISIT_RESULT" :value="detail.result" />
            <span v-else class="text-muted">-</span>
          </el-descriptions-item>
          <el-descriptions-item label="故障单号">{{ detail.fault_no }}</el-descriptions-item>
          <el-descriptions-item label="路灯编号">{{ detail.lamp_code }}</el-descriptions-item>
          <el-descriptions-item label="本论维修单">{{ detail.repair_no }}</el-descriptions-item>
          <el-descriptions-item label="首次维修单">{{ detail.origin_repair_no }}</el-descriptions-item>
          <el-descriptions-item label="维修人员">{{ detail.repairman || '-' }}</el-descriptions-item>
          <el-descriptions-item label="回访人">{{ detail.visitor || '-' }}</el-descriptions-item>
          <el-descriptions-item v-if="detail.visited_at" label="回访时间" :span="2">
            {{ formatDateTime(detail.visited_at) }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.satisfaction" label="满意度" :span="2">
            <el-rate :model-value="detail.satisfaction" disabled />
            <span class="text-muted">{{ detail.satisfaction }} 分</span>
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.unqualified_reason" label="不合格原因" :span="2">
            {{ detail.unqualified_reason }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.feedback" label="回访反馈" :span="2">
            {{ detail.feedback }}
          </el-descriptions-item>
          <el-descriptions-item v-if="detail.rework_repair_id" label="返修维修单" :span="2">
            <el-link type="primary" @click="$router.push({ path: '/repairs' })">查看返修单 #{{ detail.rework_repair_id }}</el-link>
          </el-descriptions-item>
        </el-descriptions>

        <div class="section-title drawer-block">联系情况（共 {{ detail.contact_attempts || detail.contacts?.length || 0 }} 次）</div>
        <el-timeline v-if="detail.contacts?.length">
          <el-timeline-item
            v-for="(item, index) in detail.contacts"
            :key="index"
            :timestamp="formatDateTime(item.contacted_at)"
            :type="item.result === 'reached' ? 'success' : 'info'"
          >
            <div class="timeline-title">
              <StatusTag :dict="CONTACT_STATUS" :value="item.result" />
              <span class="text-muted method">{{ dictLabel(CONTACT_METHOD, item.method) }}</span>
            </div>
            <div class="text-muted timeline-detail">
              <span v-if="item.contact_person">联系人: {{ item.contact_person }}　</span>
              {{ item.remark || '-' }}
            </div>
          </el-timeline-item>
        </el-timeline>
        <el-empty v-else description="暂无联系记录" :image-size="60" />
      </template>
      <el-empty v-else description="暂无回访数据" />
    </div>
  </el-drawer>
</template>

<script setup>
import { ref } from 'vue'
import StatusTag from '@/components/common/StatusTag.vue'
import { visitApi } from '@/api/visit'
import { CONTACT_METHOD, CONTACT_STATUS, VISIT_RESULT, VISIT_STATUS, dictLabel } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  visitId: { type: [Number, String], default: null },
})

defineEmits(['update:modelValue'])

const loading = ref(false)
const detail = ref({})

async function load() {
  if (!props.visitId) return
  loading.value = true
  try {
    detail.value = await visitApi.detail(props.visitId)
  } catch (error) {
    detail.value = {}
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.drawer-block {
  margin-top: 20px;
}

.timeline-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}

.method {
  font-size: 12px;
}

.timeline-detail {
  font-size: 13px;
  margin-top: 2px;
}
</style>

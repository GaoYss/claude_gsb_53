<template>
  <el-drawer
    :model-value="modelValue"
    title="质量回访详情"
    size="600px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="load"
  >
    <div v-loading="loading">
      <template v-if="detail.id">
        <el-descriptions :column="2" border size="small" title="回访信息">
          <el-descriptions-item label="回访单号">{{ detail.visit_no }}</el-descriptions-item>
          <el-descriptions-item label="回访轮次">第 {{ detail.round }} 轮</el-descriptions-item>
          <el-descriptions-item label="状态">
            <StatusTag :dict="VISIT_STATUS" :value="detail.status" />
          </el-descriptions-item>
          <el-descriptions-item label="回访期限">{{ formatDateTime(detail.due_at) }}</el-descriptions-item>
          <el-descriptions-item label="故障单号">{{ detail.fault_no }}</el-descriptions-item>
          <el-descriptions-item label="维修单号">{{ detail.repair_no }}</el-descriptions-item>
          <el-descriptions-item label="路灯编号">{{ detail.lamp_code }}</el-descriptions-item>
          <el-descriptions-item label="最后联系">{{ formatDateTime(detail.contacted_at) }}</el-descriptions-item>
          <el-descriptions-item label="联系人">{{ detail.contact_name || '-' }}</el-descriptions-item>
          <el-descriptions-item label="联系电话">{{ detail.contact_phone || '-' }}</el-descriptions-item>
          <el-descriptions-item label="满意度">
            <el-rate v-if="detail.satisfaction" :model-value="detail.satisfaction" disabled />
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="是否合格">
            <el-tag v-if="detail.qualified === true" type="success" effect="plain">合格</el-tag>
            <el-tag v-else-if="detail.qualified === false" type="danger" effect="plain">不合格</el-tag>
            <span v-else>-</span>
          </el-descriptions-item>
          <el-descriptions-item label="回访情况" :span="2">{{ detail.content || '-' }}</el-descriptions-item>
          <el-descriptions-item v-if="detail.remark" label="备注" :span="2">{{ detail.remark }}</el-descriptions-item>
        </el-descriptions>

        <el-alert
          v-if="detail.rework_repair_no"
          class="drawer-block"
          type="warning"
          :closable="false"
          show-icon
          :title="`本轮回访不合格, 已触发返修单 ${detail.rework_repair_no}, 返修完工后需重新回访`"
        />

        <div class="section-title drawer-block">联系记录</div>
        <el-timeline v-if="detail.contact_logs?.length">
          <el-timeline-item
            v-for="log in detail.contact_logs"
            :key="log.id"
            :timestamp="formatDateTime(log.contacted_at)"
            :type="dictType(VISIT_CONTACT, log.contact_result)"
          >
            <div class="timeline-title">
              <StatusTag :dict="VISIT_CONTACT" :value="log.contact_result" />
              <span class="contact-name">{{ log.contact_name || '未署名' }}</span>
            </div>
            <div class="text-muted timeline-detail">{{ log.content || '-' }}</div>
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
import { VISIT_CONTACT, VISIT_STATUS, dictType } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  visitId: { type: [Number, String], default: null },
})

defineEmits(['update:modelValue'])

const loading = ref(false)
const detail = ref({})

// 打开抽屉时按回访 ID 拉取详情与联系记录。
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

.contact-name {
  font-weight: 400;
}

.timeline-detail {
  font-size: 13px;
  margin-top: 2px;
}
</style>

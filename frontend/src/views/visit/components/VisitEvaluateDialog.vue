<template>
  <el-dialog
    :model-value="modelValue"
    title="回访评定"
    width="600px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-descriptions v-if="model" :column="2" border size="small" class="visit-summary">
      <el-descriptions-item label="回访单号">{{ model.visit_no }}</el-descriptions-item>
      <el-descriptions-item label="回访轮次">第 {{ model.round }} 轮</el-descriptions-item>
      <el-descriptions-item label="故障单号">{{ model.fault_no }}</el-descriptions-item>
      <el-descriptions-item label="维修单号">{{ model.repair_no }}</el-descriptions-item>
      <el-descriptions-item label="路灯编号">{{ model.lamp_code }}</el-descriptions-item>
      <el-descriptions-item label="回访期限">{{ formatDateTime(model.due_at) }}</el-descriptions-item>
    </el-descriptions>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="联系结果" prop="contact_result">
        <el-select v-model="form.contact_result" style="width: 100%">
          <el-option v-for="(item, key) in VISIT_CONTACT" :key="key" :label="item.label" :value="key" />
        </el-select>
      </el-form-item>
      <el-form-item label="联系人" prop="contact_name">
        <el-input v-model="form.contact_name" maxlength="64" placeholder="受访人姓名" />
      </el-form-item>
      <el-form-item label="联系电话" prop="contact_phone">
        <el-input v-model="form.contact_phone" maxlength="32" />
      </el-form-item>
      <el-form-item label="联系时间" prop="contacted_at">
        <el-date-picker
          v-model="form.contacted_at"
          type="datetime"
          value-format="YYYY-MM-DD HH:mm:ss"
          placeholder="默认取当前时间"
          style="width: 100%"
        />
      </el-form-item>
      <el-form-item label="满意度" prop="satisfaction">
        <el-rate v-model="form.satisfaction" :texts="scoreTexts" show-text />
      </el-form-item>
      <el-form-item label="是否合格" prop="qualified">
        <el-switch
          v-model="form.qualified"
          active-text="合格, 回访闭环"
          inactive-text="不合格, 触发返修"
          inline-prompt
          style="--el-switch-on-color: #67c23a; --el-switch-off-color: #f56c6c"
        />
      </el-form-item>
      <el-form-item label="回访情况" prop="content">
        <el-input v-model="form.content" type="textarea" :rows="3" maxlength="512" show-word-limit
          placeholder="维修效果确认、遗留问题等" />
      </el-form-item>

      <template v-if="!form.qualified">
        <el-alert type="warning" :closable="false" show-icon class="rework-tip"
          title="评定不合格后将自动创建返修维修单(关联本维修单), 故障回退为维修中; 返修完工后需重新回访, 合格前不允许结算。" />
        <el-form-item label="返修人员" prop="rework.repairman">
          <el-input v-model="form.rework.repairman" maxlength="64" placeholder="留空默认沿用原维修人员" />
        </el-form-item>
        <el-form-item label="返修班组" prop="rework.repair_team">
          <el-input v-model="form.rework.repair_team" maxlength="64" placeholder="留空默认沿用原班组" />
        </el-form-item>
        <el-form-item label="返修说明" prop="rework.content">
          <el-input v-model="form.rework.content" type="textarea" :rows="2" maxlength="512" show-word-limit
            placeholder="留空将自动带入本次回访不合格原因" />
        </el-form-item>
      </template>

      <el-form-item label="备注" prop="remark">
        <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="255" show-word-limit />
      </el-form-item>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">提交评定</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { visitApi } from '@/api/visit'
import { VISIT_CONTACT } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  model: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)

const scoreTexts = ['很不满意', '不满意', '一般', '满意', '非常满意']

const createForm = () => ({
  contact_result: 'connected',
  contact_name: '',
  contact_phone: '',
  contacted_at: '',
  satisfaction: 5,
  qualified: true,
  content: '',
  remark: '',
  rework: { repairman: '', repair_team: '', content: '' },
})

const form = reactive(createForm())

const rules = {
  contact_result: [{ required: true, message: '请选择联系结果', trigger: 'change' }],
  satisfaction: [{ required: true, message: '请选择满意度', trigger: 'change' }],
}

function syncForm() {
  Object.assign(form, createForm())
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  submitting.value = true
  try {
    const payload = { ...form }
    if (!payload.contacted_at) delete payload.contacted_at
    if (payload.qualified) {
      delete payload.rework
    } else if (payload.rework && !payload.rework.repairman && !payload.rework.repair_team && !payload.rework.content) {
      delete payload.rework
    }
    await visitApi.evaluate(props.model.id, payload)
    ElMessage.success(payload.qualified ? '回访已评定为合格' : '回访不合格, 已自动发起返修')
    emit('update:modelValue', false)
    emit('saved')
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
.visit-summary {
  margin-bottom: 16px;
}

.rework-tip {
  margin: 0 0 16px 100px;
}
</style>

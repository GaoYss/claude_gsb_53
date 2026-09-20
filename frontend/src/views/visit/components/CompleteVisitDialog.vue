<template>
  <el-dialog
    :model-value="modelValue"
    title="执行质量回访"
    width="640px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-descriptions v-if="model" :column="2" border size="small" class="visit-summary">
      <el-descriptions-item label="回访单号">{{ model.visit_no }}</el-descriptions-item>
      <el-descriptions-item label="回访轮次">第 {{ model.round }} 轮</el-descriptions-item>
      <el-descriptions-item label="故障单号">{{ model.fault_no }}</el-descriptions-item>
      <el-descriptions-item label="路灯编号">{{ model.lamp_code }}</el-descriptions-item>
      <el-descriptions-item label="关联维修单">{{ model.repair_no }}</el-descriptions-item>
      <el-descriptions-item label="维修人员">{{ model.repairman || '-' }}</el-descriptions-item>
      <el-descriptions-item v-if="model.round > 1" label="首次维修单" :span="2">
        {{ model.origin_repair_no }}（返修不改变首次处置记录）
      </el-descriptions-item>
    </el-descriptions>

    <el-alert
      v-if="model?.contact_attempts > 0"
      :title="`此前已联系 ${model.contact_attempts} 次, 最近联系时间 ${formatDateTime(model.last_contact_at)}`"
      type="info"
      :closable="false"
      class="visit-alert"
    />

    <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
      <el-form-item label="联系结果" prop="contact_status">
        <el-radio-group v-model="form.contact_status">
          <el-radio-button value="reached">已联系上</el-radio-button>
          <el-radio-button value="unreached">未联系上</el-radio-button>
        </el-radio-group>
        <div class="form-hint text-muted">选择"未联系上"仅登记一次联系情况, 回访单保持待回访, 可稍后再次联系。</div>
      </el-form-item>
      <el-form-item label="联系方式" prop="contact_method">
        <el-select v-model="form.contact_method" style="width: 200px">
          <el-option v-for="(item, key) in CONTACT_METHOD" :key="key" :label="item.label" :value="key" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="form.contact_status === 'unreached'" label="联系备注">
        <el-input v-model="form.remark" placeholder="例如: 拨打 3 次无人接听, 明日再访" maxlength="255" />
      </el-form-item>

      <template v-if="form.contact_status === 'reached'">
        <el-form-item label="回访人" prop="visitor">
          <el-input v-model="form.visitor" list="visitors-list" placeholder="执行回访的人员" maxlength="64" style="width: 240px" />
          <datalist id="visitors-list">
            <option v-for="name in visitors" :key="name" :value="name" />
          </datalist>
        </el-form-item>
        <el-form-item label="回访结论" prop="result">
          <el-radio-group v-model="form.result">
            <el-radio value="qualified">合格</el-radio>
            <el-radio value="unqualified">不合格（触发返修）</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="满意度" prop="satisfaction">
          <el-rate v-model="form.satisfaction" :max="5" show-text :texts="['很不满意', '不满意', '一般', '满意', '很满意']" />
        </el-form-item>
        <el-form-item label="回访时间">
          <el-date-picker
            v-model="form.visited_at"
            type="datetime"
            value-format="YYYY-MM-DD HH:mm:ss"
            placeholder="默认取当前时间"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item v-if="form.result === 'unqualified'" label="不合格原因" prop="unqualified_reason">
          <el-input
            v-model="form.unqualified_reason"
            type="textarea"
            :rows="2"
            placeholder="请描述回访发现的问题, 将随返修单派发给维修人员"
            maxlength="255"
            show-word-limit
          />
        </el-form-item>
        <el-form-item label="回访反馈">
          <el-input v-model="form.feedback" type="textarea" :rows="2" maxlength="512" show-word-limit />
        </el-form-item>

        <el-divider v-if="form.result === 'unqualified'" content-position="left">返修派工信息（可选, 默认派给原维修人员）</el-divider>
        <template v-if="form.result === 'unqualified'">
          <el-form-item label="返修人员">
            <el-input v-model="form.rework_repairman" :placeholder="model?.repairman ? `默认: ${model.repairman}` : '请输入返修人员'" maxlength="64" style="width: 240px" />
          </el-form-item>
          <el-form-item label="返修班组">
            <el-input v-model="form.rework_team" placeholder="默认沿用原班组请留空" maxlength="64" style="width: 240px" />
          </el-form-item>
          <el-form-item label="预留耗材">
            <el-input v-model="form.rework_materials" placeholder="可选, 如: 防水胶圈 2 个" maxlength="255" />
          </el-form-item>
        </template>
      </template>
    </el-form>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">提交回访</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { visitApi } from '@/api/visit'
import { CONTACT_METHOD } from '@/constants/dict'
import { formatDateTime } from '@/utils/format'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  model: { type: Object, default: null },
  visitors: { type: Array, default: () => [] },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)

const createForm = () => ({
  contact_status: 'reached',
  contact_method: 'phone',
  visitor: '',
  result: 'qualified',
  satisfaction: 5,
  visited_at: '',
  unqualified_reason: '',
  feedback: '',
  remark: '',
  rework_repairman: '',
  rework_team: '',
  rework_contact_phone: '',
  rework_materials: '',
})

const form = reactive(createForm())

const validateReached = (_rule, value, callback) => {
  if (form.contact_status !== 'reached') return callback()
  if (value !== 'qualified' && value !== 'unqualified') return callback(new Error('请给出回访结论'))
  callback()
}

const rules = {
  contact_status: [{ required: true, message: '请选择联系结果', trigger: 'change' }],
  result: [{ validator: validateReached, trigger: 'change' }],
  unqualified_reason: [
    {
      validator: (_rule, value, callback) => {
        if (form.contact_status === 'reached' && form.result === 'unqualified' && !value?.trim()) {
          return callback(new Error('回访不合格时必须填写原因'))
        }
        callback()
      },
      trigger: 'blur',
    },
  ],
}

function syncForm() {
  Object.assign(form, createForm())
}

async function handleSubmit() {
  const valid = await formRef.value.validate().catch(() => false)
  if (!valid) return

  const payload = { ...form }
  if (payload.contact_status === 'unreached') {
    // 未联系上不上报评价字段。
    delete payload.result
    delete payload.satisfaction
    delete payload.visitor
    delete payload.visited_at
    delete payload.unqualified_reason
    delete payload.feedback
    delete payload.rework_repairman
    delete payload.rework_team
    delete payload.rework_contact_phone
    delete payload.rework_materials
  } else {
    delete payload.remark
    if (!payload.visited_at) delete payload.visited_at
    if (payload.result === 'qualified') {
      delete payload.unqualified_reason
      delete payload.rework_repairman
      delete payload.rework_team
      delete payload.rework_contact_phone
      delete payload.rework_materials
    }
    if (!payload.rework_repairman) delete payload.rework_repairman
    if (!payload.rework_team) delete payload.rework_team
    if (!payload.rework_materials) delete payload.rework_materials
    if (!payload.rework_contact_phone) delete payload.rework_contact_phone
  }

  submitting.value = true
  try {
    await visitApi.complete(props.model.id, payload)
    ElMessage.success(payload.contact_status === 'unreached' ? '联系情况已登记' : '回访已提交')
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

.visit-alert {
  margin-bottom: 16px;
}

.form-hint {
  font-size: 12px;
  line-height: 1.6;
}
</style>

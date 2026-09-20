<template>
  <el-dialog
    :model-value="modelValue"
    title="记录联系情况"
    width="520px"
    @update:model-value="$emit('update:modelValue', $event)"
    @open="syncForm"
  >
    <el-descriptions v-if="model" :column="1" border size="small" class="visit-summary">
      <el-descriptions-item label="回访任务">{{ model.visit_no }} · 第 {{ model.round }} 轮</el-descriptions-item>
    </el-descriptions>

    <el-form ref="formRef" :model="form" :rules="rules" label-width="90px">
      <el-form-item label="联系结果" prop="contact_result">
        <el-select v-model="form.contact_result" style="width: 100%">
          <el-option v-for="(item, key) in VISIT_CONTACT" :key="key" :label="item.label" :value="key" />
        </el-select>
      </el-form-item>
      <el-form-item label="联系人" prop="contact_name">
        <el-input v-model="form.contact_name" maxlength="64" />
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
      <el-form-item label="情况说明" prop="content">
        <el-input v-model="form.content" type="textarea" :rows="3" maxlength="512" show-word-limit
          placeholder="例如: 拨打两次无人接听, 约定次日再联" />
      </el-form-item>
    </el-form>

    <div class="form-hint text-muted">仅记录联系过程, 不影响回访结论; 完成满意度评定请使用"回访评定"。</div>

    <template #footer>
      <el-button @click="$emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="submitting" @click="handleSubmit">保存联系记录</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { visitApi } from '@/api/visit'
import { VISIT_CONTACT } from '@/constants/dict'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  model: { type: Object, default: null },
})

const emit = defineEmits(['update:modelValue', 'saved'])

const formRef = ref(null)
const submitting = ref(false)

const createForm = () => ({
  contact_result: 'no_answer',
  contact_name: '',
  contact_phone: '',
  contacted_at: '',
  content: '',
})

const form = reactive(createForm())

const rules = {
  contact_result: [{ required: true, message: '请选择联系结果', trigger: 'change' }],
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
    await visitApi.contact(props.model.id, payload)
    ElMessage.success('联系情况已记录')
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

.form-hint {
  font-size: 12px;
  line-height: 1.6;
}
</style>

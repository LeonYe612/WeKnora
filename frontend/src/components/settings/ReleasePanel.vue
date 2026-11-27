<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  getReleaseCandidates,
  getReleaseStatus,
  rollbackRelease,
  triggerRelease,
  type ReleaseCandidate,
  type ReleaseStatus,
} from '@/api/system'

const props = defineProps<{ visible: boolean; deployEnv?: string }>()
const emit = defineEmits<{ (e: 'update:visible', v: boolean): void }>()

const { locale } = useI18n()
const isZh = computed(() => String(locale.value).toLowerCase().startsWith('zh'))

const T = computed(() => (isZh.value
  ? {
      title: '发布与更新',
      envProd: '生产环境',
      envDev: '测试环境',
      envUnknown: '未知环境',
      current: '当前版本',
      latest: '最新构建',
      newAvailable: '有新版本',
      upToDate: '已是最新',
      noData: '暂无数据',
      publish: '发布到生产',
      rollback: '回滚上一版',
      refresh: '刷新',
      running: '执行中',
      confirmTitle: '确认发布',
      confirmBody: (sha: string) => `将把生产切到 ${sha}。过程：拉镜像 → 备份数据库 → 改写配置 → 重建容器 → 健康检查，失败自动回滚。`,
      confirmOk: '开始发布',
      confirmCancel: '取消',
      rollbackTitle: '确认回滚',
      rollbackBody: '回滚到上一个成功发布的版本，数据库不会随之回退。',
      containerOk: '正常',
      containerBad: '异常',
      steps: '执行记录',
      devNotice: '测试栈使用 bind mount 实时编译，保存代码即生效，不需要镜像发布。改动前端需重启 wktc-frontend。',
      agentDown: '发布服务不可用（宿主 release-agent 未启动或未配置）',
      published: '已开始发布，可关闭窗口，进度会记录在最近一次结果中',
      error: '操作失败',
    }
  : {
      title: 'Release & Update',
      envProd: 'Production',
      envDev: 'Devstack',
      envUnknown: 'Unknown env',
      current: 'Current',
      latest: 'Latest build',
      newAvailable: 'Update available',
      upToDate: 'Up to date',
      noData: 'No data',
      publish: 'Deploy to production',
      rollback: 'Roll back',
      refresh: 'Refresh',
      running: 'Running',
      confirmTitle: 'Confirm deploy',
      confirmBody: (sha: string) => `Switch production to ${sha}. Steps: pull → backup DB → patch config → recreate → health check, auto rollback on failure.`,
      confirmOk: 'Start',
      confirmCancel: 'Cancel',
      rollbackTitle: 'Confirm rollback',
      rollbackBody: 'Roll back to the previous successful release. Database schema will NOT be reverted.',
      containerOk: 'OK',
      containerBad: 'Down',
      steps: 'Steps',
      devNotice: 'Devstack uses bind mount + live recompile. Saving code is enough; restart wktc-frontend after frontend changes.',
      agentDown: 'Release agent unavailable (host service not running or not configured)',
      published: 'Deploy started. You can close this window.',
      error: 'Failed',
    }))

const status = ref<ReleaseStatus | null>(null)
const candidates = ref<ReleaseCandidate[]>([])
const loading = ref(false)
const candidatesLoading = ref(false)
const errorMsg = ref('')
const selectedSha = ref('')

let pollTimer: ReturnType<typeof setInterval> | null = null

const isProd = computed(() => (props.deployEnv || status.value?.env || '') === 'production')
const envLabel = computed(() => (isProd.value ? T.value.envProd : T.value.envDev))
const currentSha = computed(() => status.value?.current?.sha7 || '')
const latestSha = computed(() => candidates.value[0]?.sha7 || '')
const hasUpdate = computed(() => !!latestSha.value && latestSha.value !== currentSha.value)
const busy = computed(() => !!status.value?.busy)
const steps = computed(() => status.value?.last?.steps || [])
const canPublish = computed(() => isProd.value && !busy.value && !!selectedSha.value && selectedSha.value !== currentSha.value)
const canRollback = computed(() => isProd.value && !busy.value && !!status.value?.previous?.sha7)

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

function startPoll() {
  stopPoll()
  pollTimer = setInterval(async () => {
    await loadStatus()
    if (!status.value?.busy) {
      stopPoll()
      await loadCandidates()
    }
  }, 3000)
}

async function loadStatus() {
  try {
    status.value = await getReleaseStatus()
    errorMsg.value = ''
  } catch (e: any) {
    errorMsg.value = e?.message || T.value.agentDown
  }
}

async function loadCandidates() {
  candidatesLoading.value = true
  try {
    const res = await getReleaseCandidates()
    candidates.value = res?.data?.candidates || []
    if (!selectedSha.value && candidates.value.length) {
      selectedSha.value = candidates.value[0].sha7
    }
  } catch (e: any) {
    candidates.value = []
  } finally {
    candidatesLoading.value = false
  }
}

async function refresh() {
  loading.value = true
  await Promise.all([loadStatus(), loadCandidates()])
  loading.value = false
}

async function onPublish() {
  if (!canPublish.value) return
  const sha = selectedSha.value
  try {
    await triggerRelease(sha)
    MessagePlugin.success(T.value.published)
    startPoll()
  } catch (e: any) {
    MessagePlugin.error(`${T.value.error}: ${e?.message || ''}`)
  }
}

async function onRollback() {
  if (!canRollback.value) return
  try {
    await rollbackRelease()
    MessagePlugin.success(T.value.published)
    startPoll()
  } catch (e: any) {
    MessagePlugin.error(`${T.value.error}: ${e?.message || ''}`)
  }
}

watch(() => props.visible, (v) => {
  if (v) {
    refresh()
  } else {
    stopPoll()
  }
})

onUnmounted(stopPoll)

defineExpose({ refresh })
</script>

<template>
  <t-dialog
    :visible="props.visible"
    :header="T.title"
    :footer="false"
    width="720px"
    placement="center"
    :close-on-overlay-click="!busy"
    @update:visible="(v: boolean) => emit('update:visible', v)"
  >
    <div class="release-panel">
      <div class="rp-head">
        <t-tag :theme="isProd ? 'danger' : 'warning'" variant="light" size="small">{{ envLabel }}</t-tag>
        <span v-if="status" class="rp-version">
          {{ T.current }}: <code>{{ currentSha || '—' }}</code>
        </span>
        <t-tag v-if="isProd && hasUpdate" theme="primary" variant="light" size="small">{{ T.newAvailable }}</t-tag>
        <t-tag v-else-if="isProd && currentSha" theme="success" variant="light" size="small">{{ T.upToDate }}</t-tag>
        <span class="rp-spacer" />
        <t-button size="small" variant="text" :loading="loading" @click="refresh">{{ T.refresh }}</t-button>
      </div>

      <t-alert v-if="errorMsg" theme="error" :message="errorMsg" class="rp-alert" />

      <div v-if="!isProd" class="rp-dev">
        <t-icon name="info-circle" size="16px" />
        <span>{{ T.devNotice }}</span>
      </div>

      <template v-if="isProd">
        <div class="rp-section">
          <div class="rp-section-title">{{ T.latest }}</div>
          <div class="rp-candidates">
            <t-loading v-if="candidatesLoading" size="small" />
            <div v-else-if="!candidates.length" class="rp-empty">{{ T.noData }}</div>
            <label
              v-for="c in candidates.slice(0, 5)"
              :key="c.sha"
              class="rp-cand"
              :class="{ 'is-current': c.sha7 === currentSha, 'is-selected': c.sha7 === selectedSha }"
            >
              <input v-model="selectedSha" type="radio" :value="c.sha7" :disabled="busy" class="rp-radio" />
              <code class="rp-sha">{{ c.sha7 }}</code>
              <span class="rp-title">{{ c.title || '—' }}</span>
              <span class="rp-time">{{ (c.at || '').replace('T', ' ').slice(0, 16) }}</span>
              <t-tag v-if="c.sha7 === currentSha" size="small" variant="outline">{{ T.current }}</t-tag>
            </label>
          </div>
        </div>

        <div class="rp-section">
          <div class="rp-section-title">{{ T.steps }}</div>
          <div class="rp-containers">
            <span
              v-for="ct in status?.containers || []"
              :key="ct.name"
              class="rp-container"
              :class="{ ok: ct.state === 'running' }"
            >
              <i class="rp-dot" />
              {{ ct.name }} · {{ ct.state === 'running' ? (ct.health || T.containerOk) : T.containerBad }}
            </span>
          </div>
          <ol v-if="steps.length" class="rp-steps">
            <li v-for="(s, i) in steps" :key="i" :class="{ fail: !s.ok }">
              <t-icon :name="s.ok ? 'check-circle-filled' : 'close-circle-filled'" size="14px" />
              <span class="rp-step-name">{{ s.name }}</span>
              <span class="rp-step-detail">{{ s.detail }}</span>
            </li>
          </ol>
          <div v-else-if="busy" class="rp-running">
            <t-loading size="small" /> {{ T.running }} — {{ status?.stage }}
          </div>
          <div v-else-if="status?.last" class="rp-last" :class="{ fail: !status.last.ok }">
            {{ status.last.message }}
          </div>
        </div>

        <div class="rp-actions">
          <t-button theme="danger" :disabled="!canPublish" :loading="busy" @click="onPublish">
            {{ busy ? T.running : T.publish }}
          </t-button>
          <t-popconfirm :content="T.rollbackBody" :confirm-btn="T.rollback" @confirm="onRollback">
            <t-button variant="outline" :disabled="!canRollback">
              {{ T.rollback }}<template v-if="status?.previous?.sha7"> ({{ status.previous.sha7 }})</template>
            </t-button>
          </t-popconfirm>
        </div>
      </template>
    </div>
  </t-dialog>
</template>

<style scoped>
.release-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 220px;
}

.rp-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rp-version {
  font-size: 13px;
  color: var(--td-text-color-secondary);
}

.rp-version code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  background: var(--td-bg-color-secondarycontainer);
  padding: 1px 6px;
  border-radius: 4px;
}

.rp-spacer { flex: 1; }

.rp-alert { margin: 0; }

.rp-dev {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--td-warning-color-light);
  color: var(--td-text-color-primary);
  font-size: 13px;
  line-height: 1.6;
}

.rp-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--td-text-color-secondary);
  margin-bottom: 8px;
}

.rp-candidates {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.rp-cand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px;
  border: 1px solid var(--td-component-border);
  border-radius: 6px;
  cursor: pointer;
  font-size: 13px;
}

.rp-cand.is-selected {
  border-color: var(--td-brand-color);
  background: var(--td-brand-color-light);
}

.rp-cand.is-current { opacity: 0.75; }

.rp-radio { margin: 0; }

.rp-sha {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  min-width: 62px;
}

.rp-title {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--td-text-color-primary);
}

.rp-time {
  font-size: 12px;
  color: var(--td-text-color-placeholder);
  white-space: nowrap;
}

.rp-containers {
  display: flex;
  gap: 16px;
  font-size: 13px;
  margin-bottom: 8px;
}

.rp-container {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--td-text-color-secondary);
}

.rp-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--td-error-color);
}

.rp-container.ok .rp-dot { background: var(--td-success-color); }

.rp-steps {
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.rp-steps li {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--td-text-color-secondary);
}

.rp-steps li.fail { color: var(--td-error-color); }

.rp-step-name { min-width: 84px; color: var(--td-text-color-primary); }

.rp-step-detail {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 460px;
}

.rp-running {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--td-brand-color);
}

.rp-last {
  font-size: 13px;
  color: var(--td-success-color);
}

.rp-last.fail { color: var(--td-error-color); }

.rp-actions {
  display: flex;
  gap: 10px;
  justify-content: flex-end;
  padding-top: 4px;
  border-top: 1px solid var(--td-component-stroke);
}

.rp-empty {
  font-size: 13px;
  color: var(--td-text-color-placeholder);
}
</style>

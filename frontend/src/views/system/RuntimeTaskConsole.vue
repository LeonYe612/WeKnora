<template>
  <div class="rtc">
    <section class="rtc-toolbar">
      <div
        class="rq-task-state-filter"
        role="tablist"
        :aria-label="t('system.globalSettings.runtime.tasks.stateFilter')"
      >
        <button
          v-for="state in taskStates"
          :key="state"
          type="button"
          role="tab"
          class="rq-task-state-option"
          :class="{ 'is-active': taskState === state }"
          :aria-selected="taskState === state"
          @click="selectState(state)"
        >
          <span class="rq-task-state-option__label">{{ stateLabel(state) }}</span>
          <span class="rq-task-state-option__count" :class="{ 'has-value': stateTotal(state) > 0 }">
            {{ stateTotal(state) }}
          </span>
        </button>
      </div>
      <div class="rtc-toolbar-side">
        <t-select
          v-model="queueFilter"
          class="rtc-queue-select"
          :options="queueOptions"
          :aria-label="t('system.globalSettings.runtime.console.queueFilter')"
        />
        <t-button
          variant="outline"
          size="small"
          :loading="loading"
          @click="reload"
        >
          <template #icon><t-icon name="refresh" /></template>
          {{ t('system.globalSettings.runtime.refresh') }}
        </t-button>
      </div>
    </section>

    <p class="rtc-guide">{{ t(`system.globalSettings.runtime.tasks.guides.${taskState}`) }}</p>

    <section v-if="bulkActions.length > 0" class="rtc-bulk">
      <span class="rtc-bulk-hint">
        {{ t('system.globalSettings.runtime.console.bulkScope', { count: actionableTasks.length }) }}
      </span>
      <div class="rtc-bulk-actions">
        <t-popconfirm
          v-if="bulkActions.includes('cancel')"
          theme="danger"
          :content="t('system.globalSettings.runtime.console.bulkCancelConfirm', { count: bulkCount('cancel') })"
          @confirm="runBulk('cancel')"
        >
          <t-button theme="danger" variant="outline" size="small" :loading="bulkRunning === 'cancel'" :disabled="Boolean(bulkRunning)">
            <template #icon><t-icon name="close-circle" /></template>
            {{ t('system.globalSettings.runtime.console.bulkCancel') }}
          </t-button>
        </t-popconfirm>
        <t-popconfirm
          v-if="bulkActions.includes('run_now')"
          theme="warning"
          :content="t('system.globalSettings.runtime.console.bulkRunNowConfirm', { count: bulkCount('run_now') })"
          @confirm="runBulk('run_now')"
        >
          <t-button theme="warning" variant="outline" size="small" :loading="bulkRunning === 'run_now'" :disabled="Boolean(bulkRunning)">
            <template #icon><t-icon name="refresh" /></template>
            {{ t('system.globalSettings.runtime.console.bulkRunNow') }}
          </t-button>
        </t-popconfirm>
        <t-popconfirm
          v-if="bulkActions.includes('delete')"
          theme="danger"
          :content="t('system.globalSettings.runtime.console.bulkDeleteConfirm', { count: bulkCount('delete') })"
          @confirm="runBulk('delete')"
        >
          <t-button theme="danger" variant="outline" size="small" :loading="bulkRunning === 'delete'" :disabled="Boolean(bulkRunning)">
            <template #icon><t-icon name="delete" /></template>
            {{ t('system.globalSettings.runtime.console.bulkDelete') }}
          </t-button>
        </t-popconfirm>
      </div>
    </section>
    <p v-if="bulkProgress" class="rtc-bulk-progress" aria-live="polite">{{ bulkProgress }}</p>

    <div v-if="loading && tasks.length === 0" class="rq-failed-loading">
      <t-loading size="small" />
      <span>{{ t('system.globalSettings.runtime.loading') }}</span>
    </div>
    <div v-else-if="error" class="rq-failed-error-state">
      <span>{{ error }}</span>
      <t-button size="small" variant="outline" @click="reload">
        {{ t('system.globalSettings.runtime.retry') }}
      </t-button>
    </div>
    <t-empty
      v-else-if="tasks.length === 0"
      :description="t('system.globalSettings.runtime.tasks.empty', { state: stateLabel(taskState) })"
    />
    <div v-else class="rq-failed-list-panel">
      <article v-for="task in tasks" :key="`${task.queue}:${task.id}`" class="rq-failed-row">
        <div class="rq-failed-row-content">
          <div class="rq-failed-row-summary">
            <span class="rtc-queue-pill">{{ queueLabel(task.queue) }}</span>
            <span class="rq-failed-row-type">{{ taskTypeLabel(task.type) }}</span>
            <span class="rq-failed-row-sep" aria-hidden="true">·</span>
            <span class="rq-task-state-pill" :class="`rq-task-state-pill--${task.state}`">
              {{ stateLabel(task.state) }}
            </span>
            <span class="rq-failed-row-sep" aria-hidden="true">·</span>
            <span class="rq-failed-row-stat">
              {{ t('system.globalSettings.runtime.tasks.attempts', { current: task.retried + 1, max: task.max_retry + 1 }) }}
            </span>
          </div>
          <dl v-if="taskMeta(task).length > 0" class="rq-failed-row-refs">
            <div v-for="ref in taskMeta(task)" :key="ref.key" class="rq-failed-ref">
              <dt>{{ ref.label }}</dt>
              <dd :title="ref.value">{{ ref.value }}</dd>
            </div>
          </dl>
          <p v-else class="rq-failed-row-unknown">
            {{ t('system.globalSettings.runtime.tasks.unknownTarget') }}
          </p>
          <p v-if="task.last_error" class="rq-failed-row-error">{{ task.last_error }}</p>
        </div>

        <div v-if="task.allowed_actions.length > 0" class="rq-failed-row-actions">
          <t-popconfirm
            v-if="task.allowed_actions.includes('cancel')"
            theme="danger"
            :content="t('system.globalSettings.runtime.tasks.cancelConfirm')"
            @confirm="runAction(task, 'cancel')"
          >
            <t-button
              shape="square" variant="text" size="small" theme="danger" class="rq-failed-icon-btn"
              :title="t('system.globalSettings.runtime.tasks.cancel')"
              :aria-label="t('system.globalSettings.runtime.tasks.cancel')"
              :loading="actionKey === `${task.queue}:${task.id}:cancel`"
              :disabled="Boolean(actionKey) || Boolean(bulkRunning)"
            ><t-icon name="close-circle" /></t-button>
          </t-popconfirm>
          <t-popconfirm
            v-if="task.allowed_actions.includes('run_now')"
            theme="warning"
            :content="t('system.globalSettings.runtime.tasks.runNowConfirm')"
            @confirm="runAction(task, 'run_now')"
          >
            <t-button
              shape="square" variant="text" size="small" class="rq-failed-icon-btn"
              :title="t('system.globalSettings.runtime.tasks.runNow')"
              :aria-label="t('system.globalSettings.runtime.tasks.runNow')"
              :loading="actionKey === `${task.queue}:${task.id}:run_now`"
              :disabled="Boolean(actionKey) || Boolean(bulkRunning)"
            ><t-icon name="refresh" /></t-button>
          </t-popconfirm>
          <t-popconfirm
            v-if="task.allowed_actions.includes('delete')"
            theme="danger"
            :content="t('system.globalSettings.runtime.tasks.deleteConfirm')"
            @confirm="runAction(task, 'delete')"
          >
            <t-button
              shape="square" variant="text" size="small" theme="danger" class="rq-failed-icon-btn"
              :title="t('system.globalSettings.runtime.tasks.deleteRecord')"
              :aria-label="t('system.globalSettings.runtime.tasks.deleteRecord')"
              :loading="actionKey === `${task.queue}:${task.id}:delete`"
              :disabled="Boolean(actionKey) || Boolean(bulkRunning)"
            ><t-icon name="delete" /></t-button>
          </t-popconfirm>
        </div>
      </article>

      <div class="rq-failed-list-footer">
        <span class="rq-failed-list-status">
          <template v-if="hasMore">
            {{ t('system.globalSettings.runtime.console.moreHint', { count: tasks.length }) }}
          </template>
          <template v-else>
            {{ t('system.globalSettings.runtime.tasks.loadedAll', { count: tasks.length }) }}
          </template>
        </span>
        <t-button v-if="hasMore" variant="outline" block :loading="loadingMore" @click="loadMore">
          {{ t('system.globalSettings.runtime.tasks.loadMore') }}
        </t-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { mergeRuntimeTaskPage } from './runtimeTaskPagination'
import {
  getAllRuntimeTasks,
  getRuntimeTasks,
  mutateRuntimeTask,
  type QueueStat,
  type RuntimeTask,
  type RuntimeTaskAction,
  type RuntimeTaskState,
} from '@/api/system'

const props = defineProps<{
  queues: QueueStat[]
  refreshToken?: string
}>()

const { t, te, locale } = useI18n()

const POLL_INTERVAL_MS = 5000
const PAGE_SIZE = 50

const taskStates: RuntimeTaskState[] = ['active', 'pending', 'scheduled', 'retry', 'archived', 'completed']
const taskState = ref<RuntimeTaskState>('pending')
const queueFilter = ref('')
const tasks = ref<RuntimeTask[]>([])
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')
const hasMore = ref(false)
const actionKey = ref('')
const bulkRunning = ref<RuntimeTaskAction | ''>('')
const bulkProgress = ref('')

// Continuation cursors per queue for the merged view. The cross-queue
// endpoint only serves first pages; deeper pages drain one queue at a time
// in the same busiest-first order the first page established.
const queueCursors = ref<Map<string, string>>(new Map())
const drainedQueues = ref<Set<string>>(new Set())

let requestID = 0
let pollTimer: ReturnType<typeof setInterval> | null = null

const runtimeTaskTypeKeys: Record<string, string> = {
  'document:process': 'documentProcess',
  'manual:process': 'manualProcess',
  'temporary_document:process': 'temporaryDocumentProcess',
  'knowledge:post_process': 'postProcess',
  'summary:generation': 'summary',
  'datatable:summary': 'tableSummary',
  'question:generation': 'question',
  'image:multimodal': 'multimodal',
  'chunk:extract': 'graph',
  'datasource:sync': 'sync',
  'faq:import': 'faqImport',
  'knowledge:list_reparse': 'batchReparse',
  'knowledge:list_delete': 'batchDelete',
  'knowledge:move': 'move',
  'index:delete': 'indexDelete',
  'kb:clone': 'kbClone',
  'kb:delete': 'kbDelete',
  'wiki:ingest': 'wikiIngest',
  'wiki:finalize': 'wikiFinalize',
}

const queueOptions = computed(() => [
  { label: t('system.globalSettings.runtime.console.allQueues'), value: '' },
  ...props.queues.map((queue) => ({ label: queueLabel(queue.name), value: queue.name })),
])

const actionableTasks = computed(() =>
  tasks.value.filter((task) => task.allowed_actions.length > 0),
)

const bulkActions = computed(() => {
  const actions = new Set<RuntimeTaskAction>()
  for (const task of actionableTasks.value) {
    for (const action of task.allowed_actions) actions.add(action)
  }
  return (['cancel', 'run_now', 'delete'] as RuntimeTaskAction[]).filter((action) => actions.has(action))
})

function bulkCount(action: RuntimeTaskAction): number {
  return actionableTasks.value.filter((task) => task.allowed_actions.includes(action)).length
}

function stateTotal(state: RuntimeTaskState): number {
  return props.queues
    .filter((queue) => !queueFilter.value || queue.name === queueFilter.value)
    .reduce((sum, queue) => sum + (queue[state] ?? 0), 0)
}

function queueLabel(name: string): string {
  const path = `system.globalSettings.runtime.queueNames.${name}`
  return te(path) ? (t(path) as string) : name
}

function taskTypeLabel(type: string): string {
  const key = runtimeTaskTypeKeys[type]
  if (!key) return type
  const path = `system.globalSettings.runtime.tasks.taskTypes.${key}`
  return te(path) ? (t(path) as string) : type
}

function stateLabel(state: RuntimeTaskState): string {
  return t(`system.globalSettings.runtime.tasks.states.${state}`)
}

function formatTaskTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(locale.value, {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
  })
}

function taskMeta(task: RuntimeTask): Array<{ key: string; label: string; value: string }> {
  const refs: Array<{ key: string; label: string; value: string }> = []
  const push = (key: string, labelKey: string, value?: string | number) => {
    if (value === undefined || value === '' || value === 0) return
    refs.push({ key, label: t(`system.globalSettings.runtime.tasks.${labelKey}`), value: String(value) })
  }
  push('knowledge', 'knowledgeLabel', task.knowledge_id)
  push('kb', 'knowledgeBaseLabel', task.knowledge_base_id)
  push('tenant', 'tenantLabel', task.tenant_id)
  push('task', 'taskIDLabel', task.task_id)
  push('knowledge-count', 'knowledgeCountLabel', task.knowledge_count)
  push('source', 'sourceLabel', task.source_id)
  push('target', 'targetLabel', task.target_id)
  push('datasource', 'dataSourceLabel', task.data_source_id)
  if (task.enqueued_at) refs.push({ key: 'enqueued', label: t('system.globalSettings.runtime.tasks.enqueuedAt'), value: formatTaskTime(task.enqueued_at) })
  if (task.started_at) refs.push({ key: 'started', label: t('system.globalSettings.runtime.tasks.startedAt'), value: formatTaskTime(task.started_at) })
  if (task.next_process_at) refs.push({ key: 'next', label: t('system.globalSettings.runtime.tasks.nextProcessAt'), value: formatTaskTime(task.next_process_at) })
  if (task.last_failed_at) refs.push({ key: 'failed', label: t('system.globalSettings.runtime.tasks.lastFailedAt'), value: formatTaskTime(task.last_failed_at) })
  if (task.completed_at) refs.push({ key: 'completed', label: t('system.globalSettings.runtime.tasks.completedAt'), value: formatTaskTime(task.completed_at) })
  if (task.worker) refs.push({ key: 'worker', label: t('system.globalSettings.runtime.tasks.worker'), value: task.worker })
  if (task.is_orphaned) refs.push({ key: 'orphaned', label: t('system.globalSettings.runtime.tasks.health'), value: t('system.globalSettings.runtime.tasks.orphaned') })
  return refs
}

async function fetchFirstPage(silent = false) {
  const requestedState = taskState.value
  const requestedQueue = queueFilter.value
  const id = ++requestID
  if (!silent) loading.value = true
  error.value = ''
  try {
    const response = await getAllRuntimeTasks(requestedState, PAGE_SIZE, requestedQueue)
    if (id !== requestID || taskState.value !== requestedState || queueFilter.value !== requestedQueue) return
    if (!response.available) {
      error.value = t('system.globalSettings.runtime.tasks.unavailable')
      return
    }
    tasks.value = response.tasks
    hasMore.value = response.has_more
    queueCursors.value = new Map()
    drainedQueues.value = new Set()
    if (silent) error.value = ''
  } catch (err: any) {
    if (id !== requestID) return
    if (!silent) error.value = err?.message || t('system.globalSettings.runtime.tasks.loadError')
  } finally {
    if (id === requestID && !silent) loading.value = false
  }
}

// Deeper pages drain queues in first-page order. Once a queue's cursor is
// exhausted it is marked drained; queues absent from the first page (small
// backlogs pushed out by busier queues) are picked up afterwards from the
// live queue stats so nothing stays hidden.
async function loadMore() {
  if (loading.value || loadingMore.value || !hasMore.value) return
  const requestedState = taskState.value
  const id = ++requestID
  loadingMore.value = true
  try {
    const seenOrder: string[] = []
    for (const task of tasks.value) {
      if (!seenOrder.includes(task.queue)) seenOrder.push(task.queue)
    }
    const remaining = props.queues
      .filter((queue) => (!queueFilter.value || queue.name === queueFilter.value) && (queue[requestedState] ?? 0) > 0)
      .sort((left, right) => (right[requestedState] ?? 0) - (left[requestedState] ?? 0))
      .map((queue) => queue.name)
    for (const name of remaining) {
      if (!seenOrder.includes(name)) seenOrder.push(name)
    }
    let appended = false
    for (const queue of seenOrder) {
      if (drainedQueues.value.has(queue)) continue
      const cursor = queueCursors.value.get(queue) ?? ''
      const response = await getRuntimeTasks(queue, requestedState, cursor, PAGE_SIZE)
      if (id !== requestID || taskState.value !== requestedState) return
      const before = tasks.value.length
      tasks.value = mergeRuntimeTaskPage(tasks.value, response.tasks)
      appended = appended || tasks.value.length > before
      if (response.has_more && response.next_cursor) {
        queueCursors.value.set(queue, response.next_cursor)
      } else {
        drainedQueues.value.add(queue)
      }
      if (appended) break
    }
    hasMore.value = seenOrder.some((queue) => !drainedQueues.value.has(queue))
  } catch (err: any) {
    if (id !== requestID) return
    if (err?.code === 'runtime_task_cursor_expired') {
      await fetchFirstPage()
      return
    }
    MessagePlugin.error(err?.message || t('system.globalSettings.runtime.tasks.loadError'))
  } finally {
    if (id === requestID) loadingMore.value = false
  }
}

function selectState(state: RuntimeTaskState) {
  if (taskState.value === state) return
  taskState.value = state
  tasks.value = []
  void fetchFirstPage()
}

function reload() {
  return fetchFirstPage()
}

async function runAction(task: RuntimeTask, action: RuntimeTaskAction) {
  actionKey.value = `${task.queue}:${task.id}:${action}`
  try {
    await mutateRuntimeTask(task.queue, task.id, action)
    MessagePlugin.success(t(`system.globalSettings.runtime.tasks.actionSuccess.${action}`))
    await fetchFirstPage()
  } catch (err: any) {
    MessagePlugin.error(err?.message || t(`system.globalSettings.runtime.tasks.actionError.${action}`))
  } finally {
    actionKey.value = ''
  }
}

// ponytail: sequential bulk with a fixed page snapshot; a server-side bulk
// endpoint is the upgrade path if thousand-task batches become routine.
async function runBulk(action: RuntimeTaskAction) {
  if (bulkRunning.value) return
  const targets = actionableTasks.value.filter((task) => task.allowed_actions.includes(action))
  if (targets.length === 0) return
  bulkRunning.value = action
  let succeeded = 0
  let failed = 0
  try {
    for (const [index, task] of targets.entries()) {
      bulkProgress.value = t('system.globalSettings.runtime.console.bulkProgress', {
        done: index + 1, total: targets.length,
      })
      try {
        await mutateRuntimeTask(task.queue, task.id, action)
        succeeded += 1
      } catch {
        failed += 1
      }
    }
    const summary = t('system.globalSettings.runtime.console.bulkResult', { succeeded, failed })
    if (failed > 0) MessagePlugin.warning(summary)
    else MessagePlugin.success(summary)
  } finally {
    bulkRunning.value = ''
    bulkProgress.value = ''
    await fetchFirstPage()
  }
}

watch(queueFilter, () => {
  tasks.value = []
  void fetchFirstPage()
})

watch(() => props.refreshToken, () => {
  if (!loading.value && !loadingMore.value && !bulkRunning.value && !actionKey.value) {
    void fetchFirstPage(true)
  }
})

onMounted(() => {
  void fetchFirstPage()
  pollTimer = setInterval(() => {
    if (document.visibilityState === 'hidden') return
    if (loading.value || loadingMore.value || bulkRunning.value || actionKey.value) return
    void fetchFirstPage(true)
  }, POLL_INTERVAL_MS)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style lang="less" scoped>
.rtc-toolbar {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}

.rtc-toolbar-side {
  display: flex;
  align-items: center;
  gap: 8px;
}

.rtc-queue-select {
  width: 180px;
}

.rtc-guide {
  margin: 10px 0 0;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}

.rtc-bulk {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 14px;
  padding: 10px 14px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-container-hover);
}

.rtc-bulk-hint {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}

.rtc-bulk-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.rtc-bulk-progress {
  margin: 8px 0 0;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
}

.rtc-queue-pill {
  padding: 1px 8px;
  border-radius: var(--app-radius-pill);
  background: var(--td-brand-color-light);
  color: var(--td-brand-color);
  font-size: var(--app-text-xs, 12px);
  white-space: nowrap;
}

.rq-task-state-filter {
  display: flex;
  gap: 4px;
  flex-wrap: wrap;
}

.rq-task-state-option {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border: none;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  cursor: pointer;

  &.is-active {
    color: var(--td-brand-color);
    border-bottom-color: var(--td-brand-color);
    font-weight: 600;
  }
}

.rq-task-state-option__count {
  font-variant-numeric: tabular-nums;
  color: var(--td-text-color-placeholder);

  &.has-value {
    color: var(--td-text-color-primary);
    font-weight: 600;
  }
}

.rq-failed-loading,
.rq-failed-error-state {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 24px 0;
  color: var(--td-text-color-secondary);
}

.rq-failed-list-panel {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.rq-failed-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--app-radius-md);
}

.rq-failed-row-content {
  min-width: 0;
  flex: 1;
}

.rq-failed-row-summary {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.rq-failed-row-type {
  font-weight: 600;
}

.rq-failed-row-sep {
  color: var(--td-text-color-placeholder);
}

.rq-task-state-pill {
  padding: 1px 8px;
  border-radius: var(--app-radius-pill);
  font-size: var(--app-text-xs, 12px);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);

  &--active { background: var(--td-success-color-light); color: var(--td-success-color); }
  &--retry, &--scheduled { background: var(--td-warning-color-light); color: var(--td-warning-color); }
  &--archived { background: var(--td-error-color-light); color: var(--td-error-color); }
}

.rq-failed-row-stat {
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-sm);
  font-variant-numeric: tabular-nums;
}

.rq-failed-row-refs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 18px;
  margin: 8px 0 0;
}

.rq-failed-ref {
  display: flex;
  gap: 8px;
  font-size: var(--app-text-sm);

  dt { color: var(--td-text-color-placeholder); }
  dd {
    margin: 0;
    color: var(--td-text-color-secondary);
    font-family: var(--app-font-mono, monospace);
    max-width: 340px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}

.rq-failed-row-unknown {
  margin: 8px 0 0;
  color: var(--td-text-color-placeholder);
  font-size: var(--app-text-sm);
}

.rq-failed-row-error {
  margin: 8px 0 0;
  padding: 8px 10px;
  border-radius: var(--app-radius-sm);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-family: var(--app-font-mono, monospace);
  font-size: var(--app-text-xs, 12px);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 120px;
  overflow: auto;
}

.rq-failed-row-actions {
  display: flex;
  gap: 4px;
  flex-shrink: 0;
}

.rq-failed-list-footer {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 6px 0 2px;
}

.rq-failed-list-status {
  color: var(--td-text-color-placeholder);
  font-size: var(--app-text-sm);
}
</style>

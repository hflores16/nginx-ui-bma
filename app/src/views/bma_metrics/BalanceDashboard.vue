<script setup lang="ts">
import type { EChartsOption } from 'echarts'
import type { BackendSummary, MetricsPeriod, MetricsResponse } from '@/api/bma_metrics'
import { ReloadOutlined } from '@ant-design/icons-vue'
import { message } from 'ant-design-vue'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { storeToRefs } from 'pinia'
import VChart from 'vue-echarts'
import bmaMetrics from '@/api/bma_metrics'
import { useSettingsStore } from '@/pinia'

use([BarChart, LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

const loading = ref(false)
const data = ref<MetricsResponse | null>(null)
const period = ref<MetricsPeriod>('5m')
const serviceFilter = ref('')
const portFilter = ref('')
let refreshTimer: ReturnType<typeof setInterval> | undefined

const settings = useSettingsStore()
const { theme } = storeToRefs(settings)

const periods = [
  { label: '1 min', value: '1m' },
  { label: '5 min', value: '5m' },
  { label: '15 min', value: '15m' },
  { label: '1 hora', value: '1h' },
  { label: '6 horas', value: '6h' },
  { label: '24 horas', value: '24h' },
]

const serviceOptions = computed(() => [
  { label: $gettext('Todos los servicios'), value: '' },
  ...(data.value?.available_services ?? []).map(service => ({
    label: service.toUpperCase(),
    value: service,
  })),
])

const portOptions = computed(() => [
  { label: $gettext('Todos los puertos'), value: '' },
  ...(data.value?.available_ports ?? []).map(port => ({
    label: `:${port}`,
    value: port,
  })),
])

const backends = computed(() => data.value?.backends ?? [])

const chartTextColor = computed(() => theme.value === 'dark' ? '#b4b4b4' : '#595959')
const chartGridColor = computed(() => theme.value === 'dark' ? '#303030' : '#f0f0f0')

function backendLabel(item: BackendSummary) {
  return item.backend_name || item.backend
}

function formatNumber(value: number) {
  return new Intl.NumberFormat().format(value)
}

function formatDecimal(value: number, digits = 1) {
  return Number.isFinite(value) ? value.toFixed(digits) : '0.0'
}

function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0)
    return '0 B'

  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0

  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }

  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

async function loadData(showSpinner = true) {
  if (showSpinner)
    loading.value = true

  try {
    data.value = await bmaMetrics.getMetrics({
      period: period.value,
      service: serviceFilter.value || undefined,
      port: portFilter.value || undefined,
    })
  }
  catch (error) {
    console.error(error)
    message.error($gettext('No fue posible cargar las métricas de balanceo'))
  }
  finally {
    loading.value = false
  }
}

function resetRefreshTimer() {
  if (refreshTimer)
    clearInterval(refreshTimer)

  const interval = ['1m', '5m', '15m'].includes(period.value) ? 15000 : 60000
  refreshTimer = setInterval(loadData, interval, false)
}

watch(period, () => {
  resetRefreshTimer()
  loadData()
})

watch(serviceFilter, () => {
  portFilter.value = ''
  loadData()
})

watch(portFilter, () => {
  loadData()
})

onMounted(async () => {
  await loadData()
  resetRefreshTimer()
})

onUnmounted(() => {
  if (refreshTimer)
    clearInterval(refreshTimer)
})

const clientDistributionOption = computed<EChartsOption>(() => {
  const rows = [...backends.value].sort((a, b) => b.active_clients - a.active_clients)

  return {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: params => {
        const item = Array.isArray(params) ? params[0] : params
        const row = rows[item.dataIndex]
        if (!row)
          return ''
        return `<strong>${backendLabel(row)}</strong><br/>${$gettext('Clientes activos')}: ${row.active_clients}<br/>${$gettext('Distribución')}: ${formatDecimal(row.client_percent)}%`
      },
    },
    grid: { left: 130, right: 30, top: 15, bottom: 30 },
    xAxis: {
      type: 'value',
      minInterval: 1,
      axisLabel: { color: chartTextColor.value },
      splitLine: { lineStyle: { color: chartGridColor.value } },
    },
    yAxis: {
      type: 'category',
      data: rows.map(backendLabel),
      axisLabel: { color: chartTextColor.value },
    },
    series: [
      {
        name: $gettext('Clientes activos'),
        type: 'bar',
        data: rows.map(row => row.active_clients),
        barMaxWidth: 28,
        label: {
          show: true,
          position: 'right',
          formatter: ({ dataIndex }) => {
            const row = rows[dataIndex]
            return row ? `${row.active_clients} (${formatDecimal(row.client_percent)}%)` : ''
          },
          color: chartTextColor.value,
        },
      },
    ],
  }
})

const latencyOption = computed<EChartsOption>(() => {
  const rows = [...backends.value].sort((a, b) => b.avg_response_ms - a.avg_response_ms)

  return {
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      formatter: params => {
        const item = Array.isArray(params) ? params[0] : params
        const row = rows[item.dataIndex]
        if (!row)
          return ''
        return `<strong>${backendLabel(row)}</strong><br/>${$gettext('Respuesta promedio')}: ${formatDecimal(row.avg_response_ms)} ms<br/>${$gettext('Conexión promedio')}: ${formatDecimal(row.avg_connect_ms)} ms`
      },
    },
    grid: { left: 130, right: 30, top: 15, bottom: 30 },
    xAxis: {
      type: 'value',
      axisLabel: {
        color: chartTextColor.value,
        formatter: '{value} ms',
      },
      splitLine: { lineStyle: { color: chartGridColor.value } },
    },
    yAxis: {
      type: 'category',
      data: rows.map(backendLabel),
      axisLabel: { color: chartTextColor.value },
    },
    series: [
      {
        name: $gettext('Latencia'),
        type: 'bar',
        data: rows.map(row => Number(row.avg_response_ms.toFixed(2))),
        barMaxWidth: 28,
        label: {
          show: true,
          position: 'right',
          formatter: ({ value }) => `${value} ms`,
          color: chartTextColor.value,
        },
      },
    ],
  }
})

const historyOption = computed<EChartsOption>(() => {
  const points = data.value?.history ?? []
  const topBackends = [...backends.value]
    .sort((a, b) => b.requests - a.requests)
    .slice(0, 8)

  const timestamps = [...new Set(points.map(point => point.timestamp))].sort()

  const series = topBackends.map(backend => {
    const byTime = new Map(
      points
        .filter(point => point.service === backend.service && point.backend === backend.backend)
        .map(point => [point.timestamp, point]),
    )

    return {
      name: backendLabel(backend),
      type: 'line' as const,
      smooth: true,
      showSymbol: false,
      data: timestamps.map(timestamp => byTime.get(timestamp)?.requests ?? 0),
    }
  })

  return {
    tooltip: { trigger: 'axis' },
    legend: {
      type: 'scroll',
      textStyle: { color: chartTextColor.value },
    },
    grid: { left: 60, right: 25, top: 50, bottom: 55 },
    xAxis: {
      type: 'category',
      data: timestamps.map(timestamp => new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })),
      axisLabel: { color: chartTextColor.value },
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLabel: { color: chartTextColor.value },
      splitLine: { lineStyle: { color: chartGridColor.value } },
    },
    series,
  }
})
</script>

<template>
  <div class="bma-balance-dashboard">
    <div class="dashboard-header">
      <div>
        <h2>{{ $gettext('Balanceo de carga') }}</h2>
        <div class="subtitle">
          {{ $gettext('Clientes, tráfico, latencia y errores por backend en tiempo real') }}
        </div>
      </div>

      <AButton :loading="loading" @click="loadData()">
        <template #icon>
          <ReloadOutlined />
        </template>
        {{ $gettext('Actualizar') }}
      </AButton>
    </div>

    <ACard class="filters-card">
      <div class="filters-grid">
        <div>
          <div class="filter-label">
            {{ $gettext('Servicio') }}
          </div>
          <ASelect
            v-model:value="serviceFilter"
            class="w-full"
            :options="serviceOptions"
            show-search
            option-filter-prop="label"
          />
        </div>

        <div>
          <div class="filter-label">
            {{ $gettext('Puerto') }}
          </div>
          <ASelect
            v-model:value="portFilter"
            class="w-full"
            :options="portOptions"
          />
        </div>

        <div>
          <div class="filter-label">
            {{ $gettext('Periodo') }}
          </div>
          <ASelect
            v-model:value="period"
            class="w-full"
            :options="periods"
          />
        </div>

        <div class="updated-box">
          <div class="filter-label">
            {{ $gettext('Última actualización') }}
          </div>
          <div>{{ data?.generated_at ? new Date(data.generated_at).toLocaleString() : '-' }}</div>
        </div>
      </div>
    </ACard>

    <AAlert
      v-if="data?.partial_window"
      class="mb-4"
      type="warning"
      show-icon
      :message="$gettext('Ventana parcial')"
      :description="$gettext('El archivo de métricas es muy grande y solo se leyó la parte más reciente. Los periodos cortos continúan siendo representativos.')"
    />

    <div class="summary-grid">
      <ACard>
        <AStatistic
          :title="$gettext('Clientes activos')"
          :value="data?.summary.active_clients ?? 0"
        />
      </ACard>
      <ACard>
        <AStatistic
          :title="$gettext('Requests')"
          :value="data?.summary.requests ?? 0"
        />
        <div class="stat-foot">
          {{ formatDecimal(data?.summary.requests_per_second ?? 0, 2) }} req/s
        </div>
      </ACard>
      <ACard>
        <AStatistic
          :title="$gettext('Respuesta promedio')"
          :value="Number(formatDecimal(data?.summary.avg_response_ms ?? 0))"
          suffix="ms"
        />
        <div class="stat-foot">
          {{ $gettext('Conexión') }}: {{ formatDecimal(data?.summary.avg_connect_ms ?? 0) }} ms
        </div>
      </ACard>
      <ACard>
        <AStatistic
          :title="$gettext('Errores 5XX')"
          :value="data?.summary.status_5xx ?? 0"
        />
        <div class="stat-foot">
          4XX: {{ formatNumber(data?.summary.status_4xx ?? 0) }}
        </div>
      </ACard>
      <ACard>
        <AStatistic
          :title="$gettext('Transferido')"
          :value="formatBytes(data?.summary.bytes ?? 0)"
        />
      </ACard>
    </div>

    <ACard class="mb-4" :title="$gettext('Nodos / Backends')" :loading="loading">
      <AEmpty
        v-if="!loading && backends.length === 0"
        :description="$gettext('No hay tráfico para los filtros seleccionados')"
      />

      <div v-else class="backend-grid">
        <div
          v-for="backend in backends"
          :key="`${backend.service}-${backend.backend}`"
          class="backend-card"
        >
          <div class="backend-title-row">
            <div>
              <div class="backend-name">
                {{ backendLabel(backend) }}
              </div>
              <div class="backend-service">
                {{ backend.service.toUpperCase() }}
                <span v-if="backend.upstreams?.length">
                  · {{ backend.upstreams.join(', ') }}
                </span>
              </div>
            </div>

            <ATag
              v-if="backend.online === true"
              color="success"
              :bordered="false"
            >
              {{ $gettext('Online') }}
            </ATag>
            <ATag
              v-else-if="backend.online === false"
              color="error"
              :bordered="false"
            >
              {{ $gettext('Offline') }}
            </ATag>
            <ATag v-else :bordered="false">
              {{ $gettext('Sin health data') }}
            </ATag>
          </div>

          <div class="client-value">
            {{ backend.active_clients }}
          </div>
          <div class="client-caption">
            {{ $gettext('clientes activos') }} · {{ formatDecimal(backend.client_percent) }}%
          </div>

          <AProgress
            :percent="Number(backend.client_percent.toFixed(1))"
            :show-info="false"
          />

          <div class="backend-metrics">
            <div>
              <span>{{ $gettext('Requests') }}</span>
              <strong>{{ formatNumber(backend.requests) }}</strong>
            </div>
            <div>
              <span>{{ $gettext('Req/s') }}</span>
              <strong>{{ formatDecimal(backend.requests_per_second, 2) }}</strong>
            </div>
            <div>
              <span>{{ $gettext('Respuesta') }}</span>
              <strong>{{ formatDecimal(backend.avg_response_ms) }} ms</strong>
            </div>
            <div>
              <span>{{ $gettext('Conexión') }}</span>
              <strong>{{ formatDecimal(backend.avg_connect_ms) }} ms</strong>
            </div>
            <div>
              <span>4XX</span>
              <strong>{{ backend.status_4xx }}</strong>
            </div>
            <div>
              <span>5XX</span>
              <strong :class="{ 'error-value': backend.status_5xx > 0 }">
                {{ backend.status_5xx }}
              </strong>
            </div>
          </div>

          <div class="backend-address">
            {{ backend.backend }}
          </div>
        </div>
      </div>
    </ACard>

    <div class="charts-grid">
      <ACard :title="$gettext('Clientes activos por nodo')" :loading="loading">
        <VChart
          v-if="backends.length"
          :option="clientDistributionOption"
          autoresize
          class="chart"
        />
        <AEmpty v-else />
      </ACard>

      <ACard :title="$gettext('Latencia promedio por nodo')" :loading="loading">
        <VChart
          v-if="backends.length"
          :option="latencyOption"
          autoresize
          class="chart"
        />
        <AEmpty v-else />
      </ACard>
    </div>

    <ACard class="mb-4" :title="$gettext('Requests a través del tiempo')" :loading="loading">
      <VChart
        v-if="data?.history?.length"
        :option="historyOption"
        autoresize
        class="history-chart"
      />
      <AEmpty v-else />
    </ACard>

    <ACard :title="$gettext('Resumen por servicio')" :loading="loading">
      <div class="service-table">
        <div class="service-row service-head">
          <span>{{ $gettext('Servicio') }}</span>
          <span>{{ $gettext('Clientes') }}</span>
          <span>{{ $gettext('Requests') }}</span>
          <span>{{ $gettext('Latencia') }}</span>
          <span>4XX</span>
          <span>5XX</span>
        </div>
        <div
          v-for="service in data?.services ?? []"
          :key="service.service"
          class="service-row"
        >
          <strong>{{ service.service.toUpperCase() }}</strong>
          <span>{{ service.summary.active_clients }}</span>
          <span>{{ formatNumber(service.summary.requests) }}</span>
          <span>{{ formatDecimal(service.summary.avg_response_ms) }} ms</span>
          <span>{{ service.summary.status_4xx }}</span>
          <span :class="{ 'error-value': service.summary.status_5xx > 0 }">
            {{ service.summary.status_5xx }}
          </span>
        </div>
      </div>
    </ACard>
  </div>
</template>

<style scoped lang="less">
.bma-balance-dashboard {
  padding-bottom: 24px;
}

.dashboard-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;

  h2 {
    margin: 0;
    font-size: 24px;
    font-weight: 650;
  }

  .subtitle {
    margin-top: 4px;
    color: var(--ant-color-text-secondary);
  }
}

.filters-card {
  margin-bottom: 16px;
}

.filters-grid {
  display: grid;
  grid-template-columns: minmax(180px, 2fr) minmax(130px, 1fr) minmax(130px, 1fr) minmax(190px, 1fr);
  gap: 16px;
  align-items: end;
}

.filter-label {
  margin-bottom: 6px;
  color: var(--ant-color-text-secondary);
  font-size: 12px;
}

.updated-box {
  min-width: 0;
  color: var(--ant-color-text);
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 16px;
}

.stat-foot {
  margin-top: 6px;
  color: var(--ant-color-text-secondary);
  font-size: 12px;
}

.backend-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(285px, 1fr));
  gap: 16px;
}

.backend-card {
  border: 1px solid var(--ant-color-border-secondary);
  border-radius: 10px;
  padding: 16px;
  background: var(--ant-color-bg-container);
}

.backend-title-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.backend-name {
  font-size: 16px;
  font-weight: 650;
}

.backend-service {
  margin-top: 2px;
  color: var(--ant-color-text-secondary);
  font-size: 12px;
}

.client-value {
  margin-top: 18px;
  font-size: 34px;
  line-height: 1;
  font-weight: 700;
}

.client-caption {
  margin-top: 5px;
  margin-bottom: 10px;
  color: var(--ant-color-text-secondary);
}

.backend-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  margin-top: 16px;

  > div {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    border-bottom: 1px dashed var(--ant-color-border-secondary);
    padding-bottom: 5px;
  }

  span {
    color: var(--ant-color-text-secondary);
    font-size: 12px;
  }
}

.backend-address {
  margin-top: 12px;
  color: var(--ant-color-text-tertiary);
  font-family: Monaco, Menlo, 'Ubuntu Mono', monospace;
  font-size: 11px;
}

.error-value {
  color: var(--ant-color-error);
  font-weight: 650;
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 16px;
}

.chart {
  width: 100%;
  height: 360px;
}

.history-chart {
  width: 100%;
  height: 380px;
}

.service-table {
  overflow-x: auto;
}

.service-row {
  display: grid;
  grid-template-columns: minmax(180px, 2fr) repeat(5, minmax(95px, 1fr));
  gap: 12px;
  align-items: center;
  min-width: 720px;
  padding: 10px 8px;
  border-bottom: 1px solid var(--ant-color-border-secondary);
}

.service-head {
  color: var(--ant-color-text-secondary);
  font-size: 12px;
  font-weight: 600;
}

@media (max-width: 1100px) {
  .summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .charts-grid {
    grid-template-columns: 1fr;
  }

  .filters-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .dashboard-header {
    flex-direction: column;
  }

  .summary-grid,
  .filters-grid {
    grid-template-columns: 1fr;
  }
}
</style>

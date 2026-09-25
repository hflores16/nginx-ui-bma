import { http } from '@uozi-admin/request'

export type MetricsPeriod = '1m' | '5m' | '15m' | '30m' | '1h' | '6h' | '24h'

export interface MetricsSummary {
  active_clients: number
  active_sessions: number
  requests: number
  requests_per_second: number
  avg_response_ms: number
  avg_connect_ms: number
  status_4xx: number
  status_5xx: number
  bytes: number
}

export interface ServiceSummary {
  service: string
  summary: MetricsSummary
}

export interface BackendSummary {
  service: string
  backend: string
  backend_name: string
  port: string
  upstreams?: string[]
  active_clients: number
  active_sessions: number
  client_percent: number
  request_percent: number
  requests: number
  requests_per_second: number
  avg_response_ms: number
  avg_connect_ms: number
  status_4xx: number
  status_5xx: number
  bytes: number
  configured: boolean
  has_traffic: boolean
  online?: boolean
  health_latency_ms?: number
}

export interface HistoryPoint {
  timestamp: string
  service: string
  backend: string
  backend_name: string
  active_clients: number
  requests: number
  requests_per_second: number
  avg_response_ms: number
  avg_connect_ms: number
  status_4xx: number
  status_5xx: number
  bytes: number
}

export interface MetricsResponse {
  generated_at: string
  period: MetricsPeriod
  service_filter?: string
  port_filter?: string
  metrics_path: string
  partial_window: boolean
  summary: MetricsSummary
  services: ServiceSummary[]
  backends: BackendSummary[]
  history: HistoryPoint[]
  available_services: string[]
  available_ports: string[]
}

export interface MetricsQuery {
  period?: MetricsPeriod
  service?: string
  port?: string
}

const bmaMetrics = {
  getMetrics(params: MetricsQuery = {}): Promise<MetricsResponse> {
    return http.get('/bma/metrics', { params })
  },
}

export default bmaMetrics

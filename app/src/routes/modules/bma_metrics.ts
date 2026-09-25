import type { RouteRecordRaw } from 'vue-router'
import { BarChartOutlined } from '@ant-design/icons-vue'

export const bmaMetricsRoutes: RouteRecordRaw[] = [
  {
    path: 'balanceo',
    name: 'BMA Load Balancing',
    component: () => import('@/views/bma_metrics/BalanceDashboard.vue'),
    meta: {
      name: () => $gettext('Balanceo'),
      icon: BarChartOutlined,
    },
  },
]

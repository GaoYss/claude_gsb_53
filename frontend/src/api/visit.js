import request from './request'

// 维修质量回访接口。
export const visitApi = {
  list: (params) => request.get('/visits', { params }),
  detail: (id) => request.get(`/visits/${id}`),
  listByFault: (faultId) => request.get(`/visits/fault/${faultId}`),
  contact: (id, data) => request.post(`/visits/${id}/contact`, data),
  evaluate: (id, data) => request.post(`/visits/${id}/evaluate`, data),
  meta: () => request.get('/visits/meta'),
  statistics: () => request.get('/visits/statistics'),
}

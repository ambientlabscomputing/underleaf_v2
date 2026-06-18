export interface BaseQueryRequest {
  limit?: number;
  offset?: number;
  order?: 'asc' | 'desc';
  orderBy?: string;
}

export interface BaseQueryResponse {
  total: number;
  count: number;
}
// Shared pagination/list types used across all services
export interface ListResponse<T> {
  items: T[];
  total: number;
}

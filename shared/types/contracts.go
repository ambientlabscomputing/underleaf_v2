package types

type BaseQueryRequest struct {
	Limit   int    `json:"limit"`
	Offset  int    `json:"offset"`
	OrderBy string `json:"order_by"`
	Order   string `json:"order" validate:"oneof=asc desc"`
}

type BaseListResponse struct {
	Results []interface{}    `json:"results"`
	Count   int              `json:"count"`
	Total   int              `json:"total"`
	Query   BaseQueryRequest `json:"query"`
}

type CreateNodeRequest struct {
	Name   string `json:"name"`
	IPAddr string `json:"ip_address"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

type QueryNodesRequest struct {
	BaseQueryRequest
	Search string `json:"search"`
	Name   string `json:"name"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

type GetNodesResponse struct {
	Results []*Node           `json:"results"`
	Count   int               `json:"count"`
	Total   int               `json:"total"`
	Query   QueryNodesRequest `json:"query"`
}

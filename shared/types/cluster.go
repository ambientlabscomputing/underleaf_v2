package types

type Cluster struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	ManagerNodeID      string `json:"manager_node_id,omitempty"`
	PrincipalAccountID string `json:"principal_account_id,omitempty"`
}

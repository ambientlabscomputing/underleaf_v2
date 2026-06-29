package types

type CertFileName string

const (
	CertFileNameClusterCert CertFileName = "cluster_cert.pem"
	CertFileNameClusterKey  CertFileName = "cluster_key.pem"
	CertFileNameCAChain     CertFileName = "ca_chain.pem"
	CertFileNameNodeCert    CertFileName = "node_cert.pem"
)

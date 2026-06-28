package utils

type HttpConfig struct {
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type ConnectionsConfig struct {
	GatewayPort         int    `yaml:"gateway_port"`
	NodeConnectionsPort int    `yaml:"node_connections_port"`
	ReadTimeout         string `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout        string `yaml:"write_timeout"`
	IdleTimeout         string `yaml:"idle_timeout"`
	Domain              string `yaml:"domain"` // Domain for the gateway server (e.g., "underleafapp.com")
}

type RedisConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Password   string `yaml:"password"`
	DB         int    `yaml:"db"`
	TTLSeconds int    `yaml:"ttl_seconds"`
}

type GRPCConfig struct {
	Port int `yaml:"port"`
}

type Config struct {
	Http                     HttpConfig         `yaml:"http"`
	Connections              *ConnectionsConfig `yaml:"gateway"`
	Redis                    *RedisConfig       `yaml:"redis"`
	GRPC                     *GRPCConfig        `yaml:"grpc"`
	DBPath                   string             `yaml:"db_path"`
	ContainerSyncIntervalSec int                `yaml:"container_sync_interval_sec"`
	CloudAPIBaseURL          string             `yaml:"cloud_api_base_url"`
	AccountUIBaseURL         string             `yaml:"account_ui_base_url"`
	CertDir                  string             `yaml:"cert_dir"`
}

var defaultOrchConfig Config
var defaultAgentConfig Config
var defaultConnWorkerConfig Config

func init() {
	defaultOrchConfig = Config{
		Http: HttpConfig{
			Port:         9090,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
		},
		DBPath:                   "orchestrator.db",
		ContainerSyncIntervalSec: 60,
		CloudAPIBaseURL:          "http://localhost:8080",
		AccountUIBaseURL:         "http://localhost:5173",
		CertDir:                  "./certs",
	}
	defaultAgentConfig = Config{
		Http: HttpConfig{
			Port:         9091,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
		},
		DBPath:                   "agent.db",
		ContainerSyncIntervalSec: 60,
	}
	defaultConnWorkerConfig = Config{
		Http: HttpConfig{
			Port:         9070,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
		},
		Connections: &ConnectionsConfig{
			NodeConnectionsPort: 9021,
			GatewayPort:         9020,
			ReadTimeout:         "5s",
			WriteTimeout:        "10s",
			IdleTimeout:         "15s",
			Domain:              "underleafapp.com",
		},
		Redis: &RedisConfig{
			Host:       "localhost",
			Port:       6379,
			Password:   "",
			DB:         0,
			TTLSeconds: 3600 * 24, // 1 day
		},
		GRPC: &GRPCConfig{
			Port: 50102,
		},
		DBPath:                   "conn_worker.db",
		ContainerSyncIntervalSec: 60,
	}
}

type ConfigType string

const (
	OrchestratorConfig ConfigType = "OrchestratorConfig"
	AgentConfig        ConfigType = "AgentConfig"
	ConnWorkerConfig   ConfigType = "ConnWorkerConfig"
)

func GetConfig(configType ConfigType) Config {
	switch configType {
	case OrchestratorConfig:
		return defaultOrchConfig
	case AgentConfig:
		return defaultAgentConfig
	case ConnWorkerConfig:
		return defaultConnWorkerConfig
	default:
		return defaultOrchConfig
	}
}

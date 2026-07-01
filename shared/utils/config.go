package utils

import (
	"fmt"
	"strconv"
)

type NamedPort int

func (np NamedPort) String() string {
	return strconv.Itoa(int(np))
}

func (np NamedPort) Int() int {
	return int(np)
}

const (
	CloudHttpPort          NamedPort = 8080
	OrchestratorHttpPort   NamedPort = 9090
	ConnectionsNodePort    NamedPort = 9021
	ConnectionsGatewayPort NamedPort = 9020
	ConnWorkerHttpPort     NamedPort = 9070
	AgentHttpPort          NamedPort = 9091
	OrchestratorGRPCPort   NamedPort = 50101
	ConnWorkerGRPCPort     NamedPort = 50102
	AgentGRPCPort          NamedPort = 50103
)

type BasePath string

func (bp BasePath) String() string {
	return string(bp)
}

const (
	// cloud components -- shared an nginx reverse proxy and domain
	// so these paths need to be unique across all cloud components
	ConnWorkerBasePath BasePath = "/api/v2/connections"
	CloudAPIBasePath   BasePath = "/api/v2/cloud"

	// edge components do not share a reverse proxy or domain, so these paths only need to be unique within the component
	OrchestratorBasePath BasePath = "/api/v1/"
	AgentBasePath        BasePath = "/api/v1/"
)

type HttpConfig struct {
	Port         NamedPort `yaml:"port"`
	ReadTimeout  string    `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout string    `yaml:"write_timeout"`
	IdleTimeout  string    `yaml:"idle_timeout"`
	BasePath     BasePath  `yaml:"base_path"`
}

type ConnectionsConfig struct {
	GatewayPort         NamedPort `yaml:"gateway_port"`
	NodeConnectionsPort NamedPort `yaml:"node_connections_port"`
	ReadTimeout         string    `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout        string    `yaml:"write_timeout"`
	IdleTimeout         string    `yaml:"idle_timeout"`
	Domain              string    `yaml:"domain"` // Domain for the gateway server (e.g., "underleafapp.com")
}

type RedisConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Password   string `yaml:"password"`
	DB         int    `yaml:"db"`
	TTLSeconds int    `yaml:"ttl_seconds"`
}

type GRPCConfig struct {
	Port NamedPort `yaml:"port"`
}

type ConnectionsClient struct {
	Host string    `yaml:"host"`
	Port NamedPort `yaml:"port"`
}

type Config struct {
	ConfigType               ConfigType         `yaml:"config_type"`
	Http                     HttpConfig         `yaml:"http"`
	Connections              *ConnectionsConfig `yaml:"gateway"`
	ConnectionsClient        *ConnectionsClient `yaml:"connections_client"`
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
		ConfigType: OrchestratorConfig,
		Http: HttpConfig{
			Port:         OrchestratorHttpPort,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
			BasePath:     OrchestratorBasePath,
		},
		DBPath:                   "orchestrator.db",
		ContainerSyncIntervalSec: 60,
		CloudAPIBaseURL:          fmt.Sprintf("http://localhost:%d%s", CloudHttpPort.Int(), CloudAPIBasePath.String()),
		AccountUIBaseURL:         "http://localhost:5173",
		CertDir:                  "./certs",
	}
	defaultAgentConfig = Config{
		ConfigType: AgentConfig,
		ConnectionsClient: &ConnectionsClient{
			Host: "localhost",
			Port: ConnectionsNodePort,
		},
		Http: HttpConfig{
			Port:         AgentHttpPort,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
			BasePath:     AgentBasePath,
		},
		DBPath:                   "agent.db",
		ContainerSyncIntervalSec: 60,
	}
	defaultConnWorkerConfig = Config{
		ConfigType: ConnWorkerConfig,
		Http: HttpConfig{
			Port:         ConnWorkerHttpPort,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
			BasePath:     ConnWorkerBasePath,
		},
		Connections: &ConnectionsConfig{
			NodeConnectionsPort: ConnectionsNodePort,
			GatewayPort:         ConnectionsGatewayPort,
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
			Port: ConnWorkerGRPCPort,
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

package utils

type HttpConfig struct {
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type Config struct {
	Http                     HttpConfig `yaml:"http"`
	DBPath                   string     `yaml:"db_path"`
	ContainerSyncIntervalSec int        `yaml:"container_sync_interval_sec"`
}

var defaultOrchConfig Config
var defaultAgentConfig Config

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
}

const (
	OrchestratorConfig = "OrchestratorConfig"
	AgentConfig        = "AgentConfig"
)

func GetConfig(configType string) Config {
	switch configType {
	case OrchestratorConfig:
		return defaultOrchConfig
	case AgentConfig:
		return defaultAgentConfig
	default:
		return defaultAgentConfig
	}
}

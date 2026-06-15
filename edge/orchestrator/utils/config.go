package utils

type HttpConfig struct {
	Port         int    `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"` // Duration string (e.g., "30s", "1m") for read timeout
	WriteTimeout string `yaml:"write_timeout"`
	IdleTimeout  string `yaml:"idle_timeout"`
}

type Config struct {
	Http   HttpConfig `yaml:"http"`
	DBPath string     `yaml:"db_path"`
}

var defaultConfig Config

func init() {
	defaultConfig = Config{
		Http: HttpConfig{
			Port:         9090,
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "15s",
		},
		DBPath: "orchestrator.db",
	}
}

func GetConfig() Config {
	return defaultConfig
}

package service

// Service defines the interface for the edge agent service.
type Service interface {
	Start() error
	Stop() error
}

// AppService is the full agent service implementation for the daemon.
type AppService struct{}

func (s *AppService) Start() error { return nil }
func (s *AppService) Stop() error  { return nil }

func NewService() Service {
	return &AppService{}
}

package service

type Service interface {
	Start() error
	Stop() error
}

type AppService struct {
	// add fields as needed
}

func (s *AppService) Start() error {
	// implement start logic
	return nil
}

func (s *AppService) Stop() error {
	// implement stop logic
	return nil
}

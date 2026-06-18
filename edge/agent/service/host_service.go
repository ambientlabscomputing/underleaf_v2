package service

import (
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

type HostService struct{}

type HostInfo struct {
	Hostname string `json:"hostname"`
	IPAddr   string `json:"ip_address"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
}

func (s *HostService) GetHostInfo() (*HostInfo, error) {
	hostname, err := s.GetHostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}

	ipAddr, err := s.getPrimaryIP()
	if err != nil {
		return nil, fmt.Errorf("failed to get primary IP: %w", err)
	}

	os, arch := s.GetOSAndArch()

	return &HostInfo{
		Hostname: hostname,
		IPAddr:   ipAddr,
		OS:       os,
		Arch:     arch,
	}, nil
}

func (s *HostService) GetHostname() (string, error) {
	return os.Hostname()
}

func (s *HostService) GetOSAndArch() (string, string) {
	return runtime.GOOS, runtime.GOARCH
}

func (s *HostService) getPrimaryIP() (string, error) {
	// Dialing a UDP connection to an external address
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		utils.Logger.Error("host: failed to determine primary IP", "error", err)
		return "", fmt.Errorf("failed to determine primary IP: %w", err)
	}
	defer conn.Close()

	// Extract local address from the connection
	localAddr := conn.LocalAddr().(*net.UDPAddr)

	fmt.Println("Primary IP:", localAddr.IP)
	return localAddr.IP.String(), nil
}

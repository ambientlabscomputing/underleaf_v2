package service

import (
	"context"
	"os"
	"testing"
	"time"

	dockerapi "github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Runs against a real Docker daemon; opt in with UNDERLEAF_DOCKER_INTEGRATION=1.
// Uses UNDERLEAF_WORKLOAD_NETWORK (set it to a throwaway name) and
// UNDERLEAF_IT_IMAGE (default nginx:stable-alpine), and cleans up after itself.
func TestIntegrationWorkloadLandsOnWorkloadNetworkNotDefaultBridge(t *testing.T) {
	if os.Getenv("UNDERLEAF_DOCKER_INTEGRATION") != "1" {
		t.Skip("set UNDERLEAF_DOCKER_INTEGRATION=1 to run against a real Docker daemon")
	}
	image := os.Getenv("UNDERLEAF_IT_IMAGE")
	if image == "" {
		image = "nginx:stable-alpine"
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal(err)
	}
	netName, err := workloadNetworkName()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	svc := &DockerService{docker: cli}
	name := "rfd5-it-workload"
	id, err := svc.CreateContainer(ctx, ContainerCreateOptions{Name: name, Image: image})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cli.ContainerRemove(context.Background(), id, dockertypes.RemoveOptions{Force: true})
		_ = cli.NetworkRemove(context.Background(), netName)
	})

	info, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := info.NetworkSettings.Networks[netName]; !ok {
		t.Fatalf("container is not on %q: %v", netName, info.NetworkSettings.Networks)
	}
	if _, ok := info.NetworkSettings.Networks["bridge"]; ok {
		t.Fatal("workload must not be attached to the default bridge")
	}
	if len(info.NetworkSettings.Networks) != 1 {
		t.Fatalf("workload should be on exactly one network, got %v", info.NetworkSettings.Networks)
	}
	nw, err := cli.NetworkInspect(ctx, netName, dockerapi.NetworkInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if nw.Driver != "bridge" || nw.Labels[managedNetworkLabel] != "true" {
		t.Fatalf("unexpected network: driver=%s labels=%v", nw.Driver, nw.Labels)
	}
}

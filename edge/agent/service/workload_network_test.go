package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockerapi "github.com/docker/docker/api/types"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
)

type fakeNetworks struct {
	existing   map[string]string // name -> driver
	created    []dockerapi.NetworkCreate
	createdAs  []string
	inspectErr error
	createErr  error
	// raceOnCreate makes NetworkCreate fail after registering the network, as
	// if another caller won the creation race.
	raceOnCreate bool
}

func (f *fakeNetworks) NetworkInspect(_ context.Context, id string, _ dockerapi.NetworkInspectOptions) (dockerapi.NetworkResource, error) {
	if f.inspectErr != nil {
		return dockerapi.NetworkResource{}, f.inspectErr
	}
	drv, ok := f.existing[id]
	if !ok {
		return dockerapi.NetworkResource{}, errdefs.NotFound(errors.New("no such network"))
	}
	return dockerapi.NetworkResource{Name: id, Driver: drv}, nil
}

func (f *fakeNetworks) NetworkCreate(_ context.Context, name string, o dockerapi.NetworkCreate) (dockerapi.NetworkCreateResponse, error) {
	if f.raceOnCreate {
		if f.existing == nil {
			f.existing = map[string]string{}
		}
		f.existing[name] = "bridge"
		return dockerapi.NetworkCreateResponse{}, errors.New("network with name already exists")
	}
	if f.createErr != nil {
		return dockerapi.NetworkCreateResponse{}, f.createErr
	}
	if f.existing == nil {
		f.existing = map[string]string{}
	}
	f.existing[name] = o.Driver
	f.created = append(f.created, o)
	f.createdAs = append(f.createdAs, name)
	return dockerapi.NetworkCreateResponse{ID: "id-" + name}, nil
}

func TestEnsureNetworkCreatesBridgeWhenMissing(t *testing.T) {
	f := &fakeNetworks{}
	if err := ensureNetwork(context.Background(), f, "wl"); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || f.createdAs[0] != "wl" || f.created[0].Driver != "bridge" {
		t.Fatalf("want one user-defined bridge named wl, got %v %v", f.createdAs, f.created)
	}
	if f.created[0].Internal {
		t.Fatal("workload network must not be internal: workloads need outbound access")
	}
}

func TestEnsureNetworkReusesExistingBridge(t *testing.T) {
	f := &fakeNetworks{existing: map[string]string{"wl": "bridge"}}
	if err := ensureNetwork(context.Background(), f, "wl"); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 0 {
		t.Fatalf("must not recreate an existing network, created %v", f.createdAs)
	}
}

func TestEnsureNetworkRefusesNonBridge(t *testing.T) {
	for _, drv := range []string{"host", "null", "overlay", "macvlan"} {
		f := &fakeNetworks{existing: map[string]string{"wl": drv}}
		err := ensureNetwork(context.Background(), f, "wl")
		if err == nil || !strings.Contains(err.Error(), drv) {
			t.Fatalf("driver %s: want refusal, got %v", drv, err)
		}
	}
}

func TestEnsureNetworkSurfacesDockerErrors(t *testing.T) {
	if err := ensureNetwork(context.Background(), &fakeNetworks{inspectErr: errors.New("daemon down")}, "wl"); err == nil {
		t.Fatal("inspect failure must surface")
	}
	if err := ensureNetwork(context.Background(), &fakeNetworks{createErr: errors.New("no address space")}, "wl"); err == nil {
		t.Fatal("create failure must surface")
	}
}

func TestEnsureNetworkToleratesCreateRace(t *testing.T) {
	if err := ensureNetwork(context.Background(), &fakeNetworks{raceOnCreate: true}, "wl"); err != nil {
		t.Fatalf("losing a creation race to a real bridge is fine: %v", err)
	}
}

func TestWorkloadNetworkNameRejectsIsolationDefeatingNames(t *testing.T) {
	for _, bad := range []string{"bridge", "host", "none", "default", ControlNetworkName, "container:abc123"} {
		t.Setenv(EnvWorkloadNetwork, bad)
		if name, err := workloadNetworkName(); err == nil {
			t.Fatalf("%q must be rejected, got %q", bad, name)
		}
	}
}

func TestWorkloadNetworkNameDefaultAndOverride(t *testing.T) {
	t.Setenv(EnvWorkloadNetwork, "")
	if got, err := workloadNetworkName(); err != nil || got != DefaultWorkloadNetwork {
		t.Fatalf("default: got %q, %v", got, err)
	}
	t.Setenv(EnvWorkloadNetwork, " my-workloads ")
	if got, err := workloadNetworkName(); err != nil || got != "my-workloads" {
		t.Fatalf("override: got %q, %v", got, err)
	}
}

func TestWorkloadNetEnsureCachesSuccessNotFailure(t *testing.T) {
	t.Setenv(EnvWorkloadNetwork, "wl")
	f := &fakeNetworks{createErr: errors.New("boom")}
	var w workloadNet
	if _, err := w.ensure(context.Background(), f); err == nil {
		t.Fatal("want error")
	}
	f.createErr = nil
	name, err := w.ensure(context.Background(), f) // retried, not wedged
	if err != nil || name != "wl" {
		t.Fatalf("retry: %q %v", name, err)
	}
	n := len(f.created)
	if _, err := w.ensure(context.Background(), f); err != nil || len(f.created) != n {
		t.Fatal("success must be cached")
	}
}

func TestWorkloadNetEnsureRejectsBadConfigBeforeTouchingDocker(t *testing.T) {
	t.Setenv(EnvWorkloadNetwork, "bridge")
	f := &fakeNetworks{}
	var w workloadNet
	if _, err := w.ensure(context.Background(), f); err == nil {
		t.Fatal("want error for default bridge")
	}
	if len(f.created) != 0 {
		t.Fatal("must not create anything")
	}
}

func TestWorkloadHostConfigNeverUsesDefaultBridge(t *testing.T) {
	hc := workloadHostConfig([]string{"v:/data"}, nat.PortMap{}, "wl")
	if string(hc.NetworkMode) != "wl" {
		t.Fatalf("NetworkMode = %q, want wl", hc.NetworkMode)
	}
	if hc.NetworkMode.IsDefault() || hc.NetworkMode.IsBridge() || hc.NetworkMode.IsHost() {
		t.Fatalf("workload must not be on default/bridge/host: %q", hc.NetworkMode)
	}
	if !hc.NetworkMode.IsUserDefined() {
		t.Fatalf("workload network must be user-defined: %q", hc.NetworkMode)
	}
	if hc.Binds[0] != "v:/data" || hc.Tmpfs["/tmp"] == "" || hc.RestartPolicy.Name == "" {
		t.Fatalf("existing hardening lost: %+v", hc)
	}
}

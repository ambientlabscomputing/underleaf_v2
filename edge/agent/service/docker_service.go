package service

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	dockerapi "github.com/docker/docker/api/types"
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"

	"github.com/ambientlabscomputing/underleaf_v2/edge/agent/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

const (
	containerStabilityWait  = 3 * time.Second
	containerStabilityTailN = "20"
	maxBuildArchiveBytes    = 500 * 1024 * 1024
	buildDownloadTimeout      = 5 * time.Minute
)

// DockerService ingests the local node's Docker containers into the agent repository
// and syncs them to the orchestrator.
type DockerService struct {
	docker     *client.Client
	repository *repository.Repository
	orchClient *clients.OrchestratorClient
	logs       *LogCollector
}

// IngestContainers reads running (and stopped) containers from the local Docker daemon,
// persists them to the agent repository, pushes them to the orchestrator, and returns
// the full list.
func (s *DockerService) IngestContainers(ctx context.Context) ([]*types.Container, error) {
	node, err := s.repository.Node.GetNode()
	if err != nil {
		return nil, fmt.Errorf("docker: get local node: %w", err)
	}

	dockerContainers, err := s.docker.ContainerList(ctx, dockertypes.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}

	containers := make([]*types.Container, 0, len(dockerContainers))
	for _, dc := range dockerContainers {
		c := types.NewContainer(dc.ID, dc.Image, types.ForeignKey(node.ID))
		c.Status = dc.Status
		c.Uptime = dc.Created // Unix timestamp of container creation (approximates uptime origin)
		if len(dc.Names) > 0 {
			c.Name = strings.TrimPrefix(dc.Names[0], "/")
		}

		if err := s.repository.Containers.UpsertContainer(c); err != nil {
			return nil, fmt.Errorf("docker: upsert container %s: %w", dc.ID[:12], err)
		}
		containers = append(containers, c)
	}

	// Push to orchestrator — best effort; log but don't fail the ingest.
	if err := s.orchClient.ReportContainers(ctx, node.ID, containers); err != nil {
		fmt.Printf("[docker] warn: failed to report containers to orchestrator: %v\n", err)
	}

	// Reconcile log tailers: start tailing newly running containers, stop for removed ones.
	var runningIDs []string
	for _, dc := range dockerContainers {
		if dc.State == "running" {
			runningIDs = append(runningIDs, dc.ID)
		}
	}
	s.logs.Reconcile(runningIDs)

	return containers, nil
}

// ListContainers returns the containers currently stored in the agent repository.
func (s *DockerService) ListContainers(ctx context.Context) ([]*types.Container, error) {
	return s.repository.Containers.ListContainers()
}

// BuildSource identifies a downloadable archive to build a container image
// from, and where within it the Docker build context lives.
type BuildSource struct {
	ArchiveURL string
	Context    string
	Dockerfile string
	Args       map[string]string
}

// ContainerCreateOptions specifies how to create a container. Exactly one of
// Image or Build should be set.
type ContainerCreateOptions struct {
	Name        string
	Image       string
	Build       *BuildSource
	Environment map[string]string
	Ports       []string // "hostPort:containerPort"
	Volumes     []string // "volumeName:/container/path"
	Labels      map[string]string
}

// DockerVolume is a named Docker volume as reported by the daemon.
type DockerVolume struct {
	Name   string
	Driver string
	Labels map[string]string
}

// CreateVolume creates a named Docker volume. Idempotent: Docker returns the
// existing volume unchanged if one with this name already exists, so a
// redeploy never wipes data.
func (s *DockerService) CreateVolume(ctx context.Context, name, driver string, labels map[string]string) error {
	if driver == "" {
		driver = "local"
	}
	if _, err := s.docker.VolumeCreate(ctx, volume.CreateOptions{
		Name:   name,
		Driver: driver,
		Labels: labels,
	}); err != nil {
		return fmt.Errorf("docker: create volume %s: %w", name, err)
	}
	return nil
}

// ListVolumes returns all Docker volumes known to this node.
func (s *DockerService) ListVolumes(ctx context.Context) ([]DockerVolume, error) {
	resp, err := s.docker.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("docker: list volumes: %w", err)
	}
	volumes := make([]DockerVolume, 0, len(resp.Volumes))
	for _, v := range resp.Volumes {
		volumes = append(volumes, DockerVolume{Name: v.Name, Driver: v.Driver, Labels: v.Labels})
	}
	return volumes, nil
}

// CreateContainer resolves the image (pulling it, or building it from
// source) and creates the container — it does not start it, see
// StartContainer. Any existing container with the same name is force-removed
// first so redeploys are idempotent; named volumes are separate Docker
// objects and are untouched by this, so their data survives.
//
// Every container gets a writable tmpfs at /tmp and an unless-stopped
// restart policy, regardless of manifest content — neither is
// manifest-configurable in track 2.
//
// Capability-dropping, a read-only root filesystem, and a flat 512MB memory
// ceiling were all tried first (matching v1's runner) and dropped after
// testing against real images: nginx:alpine crash-loops under cap-drop +
// read-only-rootfs (its entrypoint needs root capabilities to chown
// cache/run directories before dropping privileges itself), and n8n's own
// official image OOMs and crashes under a 512MB limit (Node's V8 heap
// sizing follows the container's cgroup memory limit, and n8n needs more
// than 512MB just to boot). There is no flat resource ceiling that's safe
// for arbitrary unmodified images — since "deploy any image from a
// manifest" is the actual feature, breaking ordinary images by default
// isn't an acceptable trade. This is a deliberate divergence from v1, not
// an oversight; a real per-service resource limit belongs behind a manifest
// field in a later track, not a hardcoded agent-side constant.
func (s *DockerService) CreateContainer(ctx context.Context, opts ContainerCreateOptions) (string, error) {
	var imageRef string
	var err error
	if opts.Build != nil {
		imageRef, err = s.buildImage(ctx, opts.Name, opts.Build)
	} else {
		imageRef = opts.Image
		err = s.pullImage(ctx, opts.Image)
	}
	if err != nil {
		return "", err
	}

	env := make([]string, 0, len(opts.Environment))
	for k, v := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	exposedPorts, portBindings, err := nat.ParsePortSpecs(opts.Ports)
	if err != nil {
		return "", fmt.Errorf("docker: parse ports %v: %w", opts.Ports, err)
	}

	// Idempotent redeploy: drop any prior container occupying this name.
	_ = s.docker.ContainerRemove(ctx, opts.Name, dockertypes.RemoveOptions{Force: true})

	resp, err := s.docker.ContainerCreate(ctx,
		&dockertypes.Config{
			Image:        imageRef,
			Env:          env,
			ExposedPorts: exposedPorts,
			Labels:       opts.Labels,
		},
		&dockertypes.HostConfig{
			Binds:         opts.Volumes,
			PortBindings:  portBindings,
			RestartPolicy: dockertypes.RestartPolicy{Name: dockertypes.RestartPolicyUnlessStopped},
			Tmpfs:         map[string]string{"/tmp": "rw,noexec,nosuid,size=64m"},
		},
		nil, nil, opts.Name,
	)
	if err != nil {
		return "", fmt.Errorf("docker: create container %s: %w", opts.Name, err)
	}
	return resp.ID, nil
}

func (s *DockerService) pullImage(ctx context.Context, image string) error {
	rc, err := s.docker.ImagePull(ctx, image, dockerapi.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull image %s: %w", image, err)
	}
	defer rc.Close()
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("docker: pull image %s: %w", image, err)
	}
	return nil
}

// buildImage downloads and extracts a source archive, then builds a Docker
// image from it. The returned tag is local-only (never pushed).
func (s *DockerService) buildImage(ctx context.Context, name string, build *BuildSource) (string, error) {
	if build.ArchiveURL == "" {
		return "", fmt.Errorf("docker: build source for %s has no archive url", name)
	}
	buildContext := build.Context
	if buildContext == "" {
		buildContext = "."
	}
	dockerfile := build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	tmpDir, err := os.MkdirTemp("", "underleaf-build-*")
	if err != nil {
		return "", fmt.Errorf("docker: create build temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := downloadAndExtractArchive(ctx, build.ArchiveURL, tmpDir); err != nil {
		return "", fmt.Errorf("docker: fetch build context for %s: %w", name, err)
	}

	contextDir := filepath.Join(tmpDir, buildContext)
	dockerfilePath := filepath.Join(contextDir, dockerfile)
	if _, err := os.Stat(dockerfilePath); err != nil {
		return "", fmt.Errorf("docker: %s not found in build context for %s: %w", dockerfile, name, err)
	}

	buildTar, err := tarDirectory(contextDir)
	if err != nil {
		return "", fmt.Errorf("docker: package build context for %s: %w", name, err)
	}

	buildArgs := make(map[string]*string, len(build.Args))
	for k, v := range build.Args {
		buildArgs[k] = &v
	}

	imageTag := fmt.Sprintf("underleaf/%s:latest", name)
	resp, err := s.docker.ImageBuild(ctx, buildTar, dockerapi.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: dockerfile,
		BuildArgs:  buildArgs,
		Remove:     true,
	})
	if err != nil {
		return "", fmt.Errorf("docker: build image for %s: %w", name, err)
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return "", fmt.Errorf("docker: build image for %s: %w", name, err)
	}
	return imageTag, nil
}

// downloadAndExtractArchive downloads a gzipped tarball and extracts it into
// destDir, stripping the leading path component every entry has (GitHub
// archives wrap everything under "<repo>-<sha>/").
func downloadAndExtractArchive(ctx context.Context, archiveURL, destDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Timeout: buildDownloadTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download archive: unexpected status %d from %s", resp.StatusCode, archiveURL)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	return extractTarStripped(gz, destDir)
}

// extractTarStripped extracts a tar stream into destDir. Hardened against
// path traversal (via ".." segments or absolute paths) and symlinks that
// escape destDir, and capped at maxBuildArchiveBytes of extracted content.
func extractTarStripped(r io.Reader, destDir string) error {
	cleanRoot := filepath.Clean(destDir)
	tr := tar.NewReader(r)
	var totalBytes int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Strip the leading "<repo>-<sha>/" component every entry has.
		name := hdr.Name
		idx := strings.Index(name, "/")
		if idx == -1 {
			continue // the top-level directory entry itself
		}
		name = name[idx+1:]
		if name == "" {
			continue
		}

		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, cleanRoot+string(os.PathSeparator)) {
			return fmt.Errorf("tar entry %q escapes extraction root", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeExtractedFile(tr, target, hdr.Mode, &totalBytes); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(hdr.Linkname) {
				return fmt.Errorf("tar entry %q has an absolute symlink target %q", hdr.Name, hdr.Linkname)
			}
			resolved := filepath.Join(filepath.Dir(target), hdr.Linkname)
			if !strings.HasPrefix(resolved, cleanRoot+string(os.PathSeparator)) {
				return fmt.Errorf("tar entry %q symlink target %q escapes extraction root", hdr.Name, hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			// skip other entry types (devices, fifos, etc.) — not relevant to build contexts
		}
	}
}

func writeExtractedFile(r io.Reader, target string, mode int64, totalBytes *int64) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(mode&0o777))
	if err != nil {
		return err
	}
	defer f.Close()

	remaining := maxBuildArchiveBytes - *totalBytes
	if remaining <= 0 {
		return fmt.Errorf("build archive exceeds %d byte limit", maxBuildArchiveBytes)
	}
	n, err := io.Copy(f, io.LimitReader(r, remaining+1))
	if err != nil {
		return err
	}
	*totalBytes += n
	if *totalBytes > maxBuildArchiveBytes {
		return fmt.Errorf("build archive exceeds %d byte limit", maxBuildArchiveBytes)
	}
	return nil
}

// tarDirectory packages a directory into an in-memory tar stream suitable
// for ImageBuild. Symlinks are skipped rather than followed or copied.
func tarDirectory(dir string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil // skip symlinks in the build context
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// StartContainer starts a previously created container, waits briefly to
// confirm it doesn't immediately crash, and re-ingests so the new state
// reaches the orchestrator via the existing ReportContainers push.
func (s *DockerService) StartContainer(ctx context.Context, name string) error {
	if err := s.docker.ContainerStart(ctx, name, dockertypes.StartOptions{}); err != nil {
		return fmt.Errorf("docker: start container %s: %w", name, err)
	}

	time.Sleep(containerStabilityWait)
	if inspect, err := s.docker.ContainerInspect(ctx, name); err == nil && inspect.State != nil {
		// A crash-looping container under RestartPolicyUnlessStopped can read
		// Running=true if inspected between restart attempts — Restarting is
		// the reliable signal that it isn't actually staying up.
		if !inspect.State.Running || inspect.State.Restarting {
			logs := s.tailLogs(ctx, name)
			s.reingestBestEffort(ctx, name)
			return fmt.Errorf("docker: container %s did not stay running after start (exit code %d): %s", name, inspect.State.ExitCode, logs)
		}
	}

	s.reingestBestEffort(ctx, name)
	return nil
}

// StopContainer stops a running container (no-op if already stopped).
func (s *DockerService) StopContainer(ctx context.Context, name string) error {
	timeout := 10
	if err := s.docker.ContainerStop(ctx, name, dockertypes.StopOptions{Timeout: &timeout}); err != nil {
		return fmt.Errorf("docker: stop container %s: %w", name, err)
	}
	s.reingestBestEffort(ctx, name)
	return nil
}

// RemoveContainer force-removes a container (running or not). Volumes are
// untouched — only explicit volume deletion (not implemented in track 2)
// removes data.
func (s *DockerService) RemoveContainer(ctx context.Context, name string) error {
	if err := s.docker.ContainerRemove(ctx, name, dockertypes.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("docker: remove container %s: %w", name, err)
	}
	s.reingestBestEffort(ctx, name)
	return nil
}

func (s *DockerService) reingestBestEffort(ctx context.Context, name string) {
	if _, err := s.IngestContainers(ctx); err != nil {
		fmt.Printf("[docker] warn: failed to ingest after lifecycle change on %s: %v\n", name, err)
	}
}

func (s *DockerService) tailLogs(ctx context.Context, name string) string {
	rc, err := s.docker.ContainerLogs(ctx, name, dockertypes.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: containerStabilityTailN})
	if err != nil {
		return ""
	}
	defer rc.Close()
	var buf bytes.Buffer
	_, _ = stdcopy.StdCopy(&buf, &buf, rc)
	return buf.String()
}

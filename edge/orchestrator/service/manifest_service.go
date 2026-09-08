package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"gopkg.in/yaml.v3"
)

const (
	manifestPath  = ".underleaf/deploy.yaml"
	githubAPIBase = "https://api.github.com"
	codeloadBase  = "https://codeload.github.com"
)

// ManifestService resolves a "gh:<owner>/<repo>[@ref]" source into a parsed
// DeploymentSpec by reading .underleaf/deploy.yaml directly via GitHub's
// Contents API — no clone/tarball download needed just to read the manifest.
type ManifestService struct {
	httpClient *http.Client
}

func NewManifestService() *ManifestService {
	return &ManifestService{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// ResolvedManifest is the result of resolving a gh: source: the parsed spec
// plus the concrete owner/repo/ref it was resolved against (ref is always
// concrete on return, even if the source omitted it).
type ResolvedManifest struct {
	Owner string
	Repo  string
	Ref   string
	Spec  types.DeploymentSpec
}

// ParseSource splits a "gh:<owner>/<repo>[@ref]" string into its parts.
// ref is empty if the source didn't specify one.
func ParseSource(source string) (owner, repo, ref string, err error) {
	const prefix = "gh:"
	if !strings.HasPrefix(source, prefix) {
		return "", "", "", fmt.Errorf("manifest: unsupported source %q (expected gh:<owner>/<repo>[@ref])", source)
	}
	rest := strings.TrimPrefix(source, prefix)
	if at := strings.LastIndex(rest, "@"); at != -1 {
		ref = rest[at+1:]
		rest = rest[:at]
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", fmt.Errorf("manifest: invalid source %q (expected gh:<owner>/<repo>[@ref])", source)
	}
	return parts[0], parts[1], ref, nil
}

// Resolve fetches and parses .underleaf/deploy.yaml for the given source.
// refOverride, if non-empty, takes precedence over any "@ref" embedded in
// source (this is what a CLI/REST "--ref" flag should pass through). token,
// if non-empty, is sent as a GitHub bearer token (needed for private repos,
// and to avoid unauthenticated rate limits).
func (s *ManifestService) Resolve(ctx context.Context, source, refOverride, token string) (*ResolvedManifest, error) {
	owner, repo, ref, err := ParseSource(source)
	if err != nil {
		return nil, err
	}
	if refOverride != "" {
		ref = refOverride
	}

	if ref == "" {
		ref, err = s.defaultBranch(ctx, owner, repo, token)
		if err != nil {
			return nil, fmt.Errorf("manifest: resolve default branch for %s/%s: %w", owner, repo, err)
		}
	}

	spec, err := s.fetchManifest(ctx, owner, repo, ref, token)
	if err != nil {
		return nil, err
	}

	// Services built from source need a downloadable archive of the repo at
	// this ref — attach it now so Phase 3's agent-side build step doesn't
	// need to know anything about GitHub.
	for i := range spec.Services {
		if spec.Services[i].Build != nil {
			spec.Services[i].Source = &types.SourceRef{
				Owner:      owner,
				Repo:       repo,
				Ref:        ref,
				ArchiveURL: fmt.Sprintf("%s/%s/%s/tar.gz/%s", codeloadBase, owner, repo, ref),
			}
		}
	}

	return &ResolvedManifest{Owner: owner, Repo: repo, Ref: ref, Spec: *spec}, nil
}

func (s *ManifestService) defaultBranch(ctx context.Context, owner, repo, token string) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, owner, repo)
	body, err := s.githubGET(ctx, url, token)
	if err != nil {
		return "", err
	}
	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &repoInfo); err != nil {
		return "", fmt.Errorf("manifest: decode repo info: %w", err)
	}
	if repoInfo.DefaultBranch == "" {
		return "", fmt.Errorf("manifest: repo %s/%s has no default branch", owner, repo)
	}
	return repoInfo.DefaultBranch, nil
}

func (s *ManifestService) fetchManifest(ctx context.Context, owner, repo, ref, token string) (*types.DeploymentSpec, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s", githubAPIBase, owner, repo, manifestPath, ref)
	body, err := s.githubGET(ctx, url, token)
	if err != nil {
		return nil, fmt.Errorf("manifest: fetch %s from %s/%s@%s: %w", manifestPath, owner, repo, ref, err)
	}

	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := json.Unmarshal(body, &content); err != nil {
		return nil, fmt.Errorf("manifest: decode github contents response: %w", err)
	}
	if content.Encoding != "base64" {
		return nil, fmt.Errorf("manifest: unexpected content encoding %q", content.Encoding)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return nil, fmt.Errorf("manifest: decode base64 content: %w", err)
	}

	var spec types.DeploymentSpec
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return nil, fmt.Errorf("manifest: %s is not valid YAML: %w", manifestPath, err)
	}
	normalizeSpec(&spec)
	if err := validateSpec(&spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

func (s *ManifestService) githubGET(ctx context.Context, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "underleaf-orchestrator")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (404): %s", url)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github api error (%d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// normalizeSpec fills in the build-block defaults the manifest format allows
// authors to omit (context "." and Dockerfile "Dockerfile" at the repo root).
func normalizeSpec(spec *types.DeploymentSpec) {
	for i := range spec.Services {
		b := spec.Services[i].Build
		if b == nil {
			continue
		}
		if b.Context == "" {
			b.Context = "."
		}
		if b.Dockerfile == "" {
			b.Dockerfile = "Dockerfile"
		}
	}
}

// validateSpec checks the manifest is well-formed. It deliberately does not
// check cross-references (e.g. a service's networks/volumes existing in the
// top-level declarations) — that's the reconciler's compile step (Phase 4),
// which needs the same information to build its dependency graph anyway.
func validateSpec(spec *types.DeploymentSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("manifest: missing required field 'name'")
	}
	if len(spec.Services) == 0 {
		return fmt.Errorf("manifest: at least one service is required")
	}
	for _, svc := range spec.Services {
		if svc.Name == "" {
			return fmt.Errorf("manifest: a service is missing required field 'name'")
		}
		if svc.Image == "" && svc.Build == nil {
			return fmt.Errorf("manifest: service %q must declare exactly one of image/build", svc.Name)
		}
		if svc.Image != "" && svc.Build != nil {
			return fmt.Errorf("manifest: service %q must declare exactly one of image/build, not both", svc.Name)
		}
		for _, mount := range svc.Volumes {
			name, path, ok := strings.Cut(mount, ":")
			if !ok || name == "" || path == "" {
				return fmt.Errorf("manifest: service %q has invalid volume mount %q (expected \"name:/path\")", svc.Name, mount)
			}
		}
		if svc.Expose != nil && (svc.Expose.Port < 1 || svc.Expose.Port > 65535) {
			return fmt.Errorf("manifest: service %q has invalid expose.port %d", svc.Name, svc.Expose.Port)
		}
	}
	return nil
}

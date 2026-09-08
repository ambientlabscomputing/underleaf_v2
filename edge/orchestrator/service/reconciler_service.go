package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

// ReconcilerService turns a deployment's DeploymentSpec into running
// containers: it compiles the spec into a dependency-ordered graph, diffs it
// against what's actually observed on the agent plus what was last applied,
// and executes the minimal set of agent RPCs to converge. All
// compile/observe/diff/plan decisions live here in the Orchestrator; the
// Agent (see Phase 3) is a thin, stateless-per-call Docker executor.
type ReconcilerService struct {
	Repository  *repository.Repository
	AgentClient *clients.AgentClient
}

// resourceKind distinguishes the two resource types track 2 manages.
// Networks are parsed and stored (shared/types) but deliberately not
// reconciled here — see RFDs/RFD-3.md.
type resourceKind string

const (
	resourceVolume    resourceKind = "volume"
	resourceContainer resourceKind = "container"
)

// resourceID identifies one resource in the graph by its already-namespaced
// Docker name (e.g. "n8n_n8n_data" or "n8n-n8n").
type resourceID struct {
	Kind resourceKind
	Name string
}

type opType string

const (
	opCreate opType = "create"
	opUpdate opType = "update"
	opDelete opType = "delete"
	opAdopt  opType = "adopt" // observed but not ours to manage; left alone
	opNoop   opType = "noop"
)

type operation struct {
	ID   resourceID
	Type opType
}

// compiledGraph is the output of compile(): the deployment's desired
// resources, namespaced and dependency-ordered.
type compiledGraph struct {
	Slug          string
	Volumes       map[string]types.VolumeSpec  // key: namespaced volume name
	Services      map[string]types.ServiceSpec // key: namespaced container name
	CreationOrder []resourceID
	DeletionOrder []resourceID // exact reverse of CreationOrder
}

// lastAppliedSnapshot is what was actually reconciled last time, used to
// detect drift and to know which resources this reconciler is allowed to
// prune. Persisted in the Orchestrator's own database (edge/orchestrator/
// repository/deployment_repository.go) rather than a per-agent file, unlike
// v1.
type lastAppliedSnapshot struct {
	Slug     string                       `json:"slug"`
	Services map[string]types.ServiceSpec `json:"services"`
	Volumes  map[string]types.VolumeSpec  `json:"volumes"`
}

// Reconcile converges the agent's actual state onto the deployment's desired
// spec. On failure it stops at the first failed operation and returns —
// there is no partial rollback, matching v1's fail-fast behavior.
func (s *ReconcilerService) Reconcile(ctx context.Context, deployment *types.Deployment) error {
	graph, err := compile(deployment.Spec)
	if err != nil {
		return err
	}

	node, err := s.singleNode()
	if err != nil {
		return err
	}

	observedContainers, err := s.Repository.Containers.ListContainersByNode(node.ID)
	if err != nil {
		return fmt.Errorf("reconciler: list observed containers: %w", err)
	}
	observedByName := make(map[string]*types.Container, len(observedContainers))
	for _, c := range observedContainers {
		if c.Name != "" {
			observedByName[c.Name] = c
		}
	}

	observedVolumes, err := s.AgentClient.ListVolumes(ctx)
	if err != nil {
		return fmt.Errorf("reconciler: list observed volumes: %w", err)
	}
	observedVolNames := make(map[string]bool, len(observedVolumes))
	for _, v := range observedVolumes {
		observedVolNames[v.Name] = true
	}

	prev, err := s.loadLastApplied(deployment.ID)
	if err != nil {
		return err
	}

	ops := diff(graph, observedByName, observedVolNames, prev)

	if err := s.execute(ctx, graph, ops); err != nil {
		return err
	}

	return s.saveLastApplied(deployment.ID, graph)
}

func (s *ReconcilerService) singleNode() (*types.Node, error) {
	nodes, _, err := s.Repository.Nodes.ListNodes(types.QueryNodesRequest{})
	if err != nil {
		return nil, fmt.Errorf("reconciler: list nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("reconciler: no agent node registered")
	}
	return nodes[0], nil
}

func (s *ReconcilerService) loadLastApplied(deploymentID string) (*lastAppliedSnapshot, error) {
	empty := &lastAppliedSnapshot{Services: map[string]types.ServiceSpec{}, Volumes: map[string]types.VolumeSpec{}}
	raw, err := s.Repository.Deployments.GetLastApplied(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("reconciler: load last-applied snapshot: %w", err)
	}
	if len(raw) == 0 {
		return empty, nil
	}
	var snap lastAppliedSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("reconciler: decode last-applied snapshot: %w", err)
	}
	if snap.Services == nil {
		snap.Services = map[string]types.ServiceSpec{}
	}
	if snap.Volumes == nil {
		snap.Volumes = map[string]types.VolumeSpec{}
	}
	return &snap, nil
}

func (s *ReconcilerService) saveLastApplied(deploymentID string, graph *compiledGraph) error {
	snap := lastAppliedSnapshot{Slug: graph.Slug, Services: graph.Services, Volumes: graph.Volumes}
	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("reconciler: encode last-applied snapshot: %w", err)
	}
	return s.Repository.Deployments.SaveLastApplied(deploymentID, raw)
}

// compile builds one graph node per volume and service, computing each
// service's dependencies from the volumes it mounts (the sole ordering rule
// for track 2 — see RFDs/RFD-3.md on deferred network support), then
// topologically sorts them. It also validates the cross-references Phase 2's
// manifest_service deliberately left to this step: that a service's
// declared volumes/networks actually exist in the manifest's top-level
// declarations.
func compile(spec types.DeploymentSpec) (*compiledGraph, error) {
	slug := slugify(spec)

	declaredVolumes := make(map[string]bool, len(spec.Volumes))
	volumesByName := make(map[string]types.VolumeSpec, len(spec.Volumes))
	for _, v := range spec.Volumes {
		declaredVolumes[v.Name] = true
		volumesByName[namespacedVolume(slug, v.Name)] = v
	}
	declaredNetworks := make(map[string]bool, len(spec.Networks))
	for _, n := range spec.Networks {
		declaredNetworks[n.Name] = true
	}

	nodes := map[resourceID]bool{}
	deps := map[resourceID][]resourceID{}

	for _, v := range spec.Volumes {
		id := resourceID{resourceVolume, namespacedVolume(slug, v.Name)}
		nodes[id] = true
	}

	servicesByName := make(map[string]types.ServiceSpec, len(spec.Services))
	for _, svc := range spec.Services {
		id := resourceID{resourceContainer, namespacedContainer(slug, svc.Name)}
		nodes[id] = true
		servicesByName[id.Name] = svc

		var svcDeps []resourceID
		for _, mount := range svc.Volumes {
			volName, _, ok := strings.Cut(mount, ":")
			if !ok {
				return nil, fmt.Errorf("reconciler: service %q has invalid volume mount %q", svc.Name, mount)
			}
			if !declaredVolumes[volName] {
				return nil, fmt.Errorf("reconciler: service %q mounts undeclared volume %q", svc.Name, volName)
			}
			svcDeps = append(svcDeps, resourceID{resourceVolume, namespacedVolume(slug, volName)})
		}
		for _, netName := range svc.Networks {
			if !declaredNetworks[netName] {
				return nil, fmt.Errorf("reconciler: service %q references undeclared network %q", svc.Name, netName)
			}
		}
		if svc.Build != nil && svc.Source == nil {
			return nil, fmt.Errorf("reconciler: service %q has a build source that was never resolved", svc.Name)
		}
		deps[id] = svcDeps
	}

	order, err := topoSort(nodes, deps)
	if err != nil {
		return nil, err
	}
	deletionOrder := make([]resourceID, len(order))
	for i, id := range order {
		deletionOrder[len(order)-1-i] = id
	}

	return &compiledGraph{
		Slug:          slug,
		Volumes:       volumesByName,
		Services:      servicesByName,
		CreationOrder: order,
		DeletionOrder: deletionOrder,
	}, nil
}

// topoSort orders nodes via Kahn's algorithm so every resource comes after
// everything it depends on. Ties are broken deterministically (kind, then
// name) so repeated compiles of the same spec produce the same order.
func topoSort(nodes map[resourceID]bool, deps map[resourceID][]resourceID) ([]resourceID, error) {
	inDegree := make(map[resourceID]int, len(nodes))
	dependents := make(map[resourceID][]resourceID, len(nodes))
	for id := range nodes {
		inDegree[id] = 0
	}
	for id, ds := range deps {
		for _, d := range ds {
			inDegree[id]++
			dependents[d] = append(dependents[d], id)
		}
	}

	var queue []resourceID
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}
	sortResourceIDs(queue)

	order := make([]resourceID, 0, len(nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)

		var freed []resourceID
		for _, dependent := range dependents[id] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				freed = append(freed, dependent)
			}
		}
		sortResourceIDs(freed)
		queue = append(queue, freed...)
	}

	if len(order) != len(nodes) {
		return nil, fmt.Errorf("reconciler: dependency cycle detected in deployment graph")
	}
	return order, nil
}

func sortResourceIDs(ids []resourceID) {
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Kind != ids[j].Kind {
			return ids[i].Kind < ids[j].Kind
		}
		return ids[i].Name < ids[j].Name
	})
}

func slugify(spec types.DeploymentSpec) string {
	s := spec.Slug
	if s == "" {
		s = spec.Name
	}
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func namespacedVolume(slug, name string) string    { return fmt.Sprintf("%s_%s", slug, name) }
func namespacedContainer(slug, name string) string { return fmt.Sprintf("%s-%s", slug, name) }

// diff computes what needs to happen to converge, per resource, then orders
// the resulting operations for execution. Volumes are create-only and are
// never updated or pruned — Docker volumes have no other diffable
// attributes yet, and refusing to ever auto-delete a volume avoids
// destroying user data on redeploy, matching v1's design.
func diff(graph *compiledGraph, observedContainers map[string]*types.Container, observedVolumes map[string]bool, prev *lastAppliedSnapshot) []operation {
	var ops []operation

	for _, id := range graph.CreationOrder {
		if id.Kind != resourceVolume {
			continue
		}
		if observedVolumes[id.Name] {
			ops = append(ops, operation{ID: id, Type: opNoop})
		} else {
			ops = append(ops, operation{ID: id, Type: opCreate})
		}
	}

	for _, id := range graph.CreationOrder {
		if id.Kind != resourceContainer {
			continue
		}
		desired := graph.Services[id.Name]
		_, wasObserved := observedContainers[id.Name]
		prevSpec, wasApplied := prev.Services[id.Name]

		switch {
		case !wasObserved:
			ops = append(ops, operation{ID: id, Type: opCreate})
		case !wasApplied:
			// Observed but not something we created last time: an unmanaged
			// container happens to share this namespaced name. Leave it alone
			// rather than risk clobbering something we don't own.
			ops = append(ops, operation{ID: id, Type: opAdopt})
		case !reflect.DeepEqual(desired, prevSpec):
			ops = append(ops, operation{ID: id, Type: opUpdate})
		default:
			ops = append(ops, operation{ID: id, Type: opNoop})
		}
	}

	// Prune: containers we previously created that are no longer desired.
	for name := range prev.Services {
		if _, stillDesired := graph.Services[name]; stillDesired {
			continue
		}
		if _, isObserved := observedContainers[name]; !isObserved {
			continue // already gone
		}
		ops = append(ops, operation{ID: resourceID{resourceContainer, name}, Type: opDelete})
	}

	return orderOps(ops, graph)
}

// orderOps arranges operations as deletes -> updates -> creates (adopts and
// noops trail, since they do nothing), preserving the graph's dependency
// order within each bucket.
func orderOps(ops []operation, graph *compiledGraph) []operation {
	byID := make(map[resourceID]operation, len(ops))
	for _, op := range ops {
		byID[op.ID] = op
	}

	var deletes, updates, creates, rest []operation
	seenDelete := map[resourceID]bool{}

	for _, id := range graph.DeletionOrder {
		if op, ok := byID[id]; ok && op.Type == opDelete {
			deletes = append(deletes, op)
			seenDelete[id] = true
		}
	}
	// Pruned resources are, by definition, no longer in the graph at all.
	for _, op := range ops {
		if op.Type == opDelete && !seenDelete[op.ID] {
			deletes = append(deletes, op)
			seenDelete[op.ID] = true
		}
	}

	for _, id := range graph.CreationOrder {
		op, ok := byID[id]
		if !ok {
			continue
		}
		switch op.Type {
		case opUpdate:
			updates = append(updates, op)
		case opCreate:
			creates = append(creates, op)
		case opAdopt, opNoop:
			rest = append(rest, op)
		}
	}

	ordered := make([]operation, 0, len(ops))
	ordered = append(ordered, deletes...)
	ordered = append(ordered, updates...)
	ordered = append(ordered, creates...)
	ordered = append(ordered, rest...)
	return ordered
}

// execute runs the plan in order, stopping at the first failure.
func (s *ReconcilerService) execute(ctx context.Context, graph *compiledGraph, ops []operation) error {
	for _, op := range ops {
		switch op.Type {
		case opCreate:
			if op.ID.Kind == resourceVolume {
				v := graph.Volumes[op.ID.Name]
				if err := s.AgentClient.CreateVolume(ctx, op.ID.Name, "", map[string]string{
					"underleaf.slug":   graph.Slug,
					"underleaf.volume": v.Name,
				}); err != nil {
					return fmt.Errorf("reconciler: create volume %s: %w", op.ID.Name, err)
				}
				continue
			}
			if err := s.createAndStart(ctx, graph, op.ID.Name, graph.Services[op.ID.Name]); err != nil {
				return err
			}
		case opUpdate:
			if err := s.stopAndRemove(ctx, op.ID.Name); err != nil {
				return err
			}
			if err := s.createAndStart(ctx, graph, op.ID.Name, graph.Services[op.ID.Name]); err != nil {
				return err
			}
		case opDelete:
			if err := s.stopAndRemove(ctx, op.ID.Name); err != nil {
				return err
			}
		case opAdopt, opNoop:
			// nothing to do
		}
	}
	return nil
}

func (s *ReconcilerService) stopAndRemove(ctx context.Context, name string) error {
	if err := s.AgentClient.StopContainer(ctx, name); err != nil {
		return fmt.Errorf("reconciler: stop container %s: %w", name, err)
	}
	if err := s.AgentClient.RemoveContainer(ctx, name); err != nil {
		return fmt.Errorf("reconciler: remove container %s: %w", name, err)
	}
	return nil
}

func (s *ReconcilerService) createAndStart(ctx context.Context, graph *compiledGraph, name string, svc types.ServiceSpec) error {
	req := clients.CreateContainerRequest{
		Name:        name,
		Image:       svc.Image,
		Environment: svc.Environment,
		Ports:       svc.Ports,
		Labels: map[string]string{
			"underleaf.slug":    graph.Slug,
			"underleaf.service": svc.Name,
		},
	}
	for _, mount := range svc.Volumes {
		volName, path, ok := strings.Cut(mount, ":")
		if !ok {
			return fmt.Errorf("reconciler: service %q has invalid volume mount %q", svc.Name, mount)
		}
		req.Volumes = append(req.Volumes, fmt.Sprintf("%s:%s", namespacedVolume(graph.Slug, volName), path))
	}
	if svc.Build != nil {
		if svc.Source == nil {
			return fmt.Errorf("reconciler: service %q has a build source that was never resolved", svc.Name)
		}
		req.Build = &clients.BuildSourceRequest{
			ArchiveURL: svc.Source.ArchiveURL,
			Context:    svc.Build.Context,
			Dockerfile: svc.Build.Dockerfile,
			Args:       svc.Build.Args,
		}
	}

	if _, err := s.AgentClient.CreateContainer(ctx, req); err != nil {
		return fmt.Errorf("reconciler: create container %s: %w", name, err)
	}
	if err := s.AgentClient.StartContainer(ctx, name); err != nil {
		return fmt.Errorf("reconciler: start container %s: %w", name, err)
	}
	return nil
}

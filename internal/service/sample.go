package service

import (
	"context"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/sampling"
	"rockbolt-pullout-zonal-closure/internal/store"
)

// SampleResult is the outcome of a deterministic sampling run.
type SampleResult struct {
	Task   *domain.InspectionTask
	Nodes  []store.SampleNode
	Digest string
}

// Sample runs the documented deterministic stratified sampling over the locked
// candidate holes and commits the resulting tree atomically. It returns the
// stable tree digest so two identical runs can be compared byte-for-byte.
func (s *Service) Sample(ctx context.Context, opID, taskID string) (*SampleResult, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingSample {
		return nil, ErrInvalidTransition
	}
	holes, err := s.store.ListCandidateHoles(ctx, taskID)
	if err != nil {
		return nil, err
	}
	tree, err := sampling.BuildTree(t.SamplingSeed, holes, t.LayerQuotas)
	if err != nil {
		return nil, err
	}
	nodes := treeToNodes(taskID, t.Generation, tree)
	digest := domain.Digest(nodes)
	updated, err := s.store.CommitSampleTree(ctx, opID, taskID, digest, nodes, t.Version)
	if err != nil {
		return nil, err
	}
	return &SampleResult{Task: updated, Nodes: nodes, Digest: tree.Digest()}, nil
}

// treeToNodes converts a sampling.Tree into persisted sample nodes, one layer
// node per spatial layer and one leaf node per selected hole.
func treeToNodes(taskID string, generation int, tree sampling.Tree) []store.SampleNode {
	nodes := make([]store.SampleNode, 0, len(tree.Layers)+len(tree.Leaves))
	for _, l := range tree.Layers {
		nodes = append(nodes, store.SampleNode{
			ID:         l.ID,
			TaskID:     taskID,
			LayerKey:   string(l.Azimuth),
			Category:   domain.SampleOriginal,
			Generation: generation,
		})
	}
	for _, leaf := range tree.Leaves {
		hk := leaf.HoleKey
		nodes = append(nodes, store.SampleNode{
			ID:         leaf.ID,
			TaskID:     taskID,
			ParentID:   leaf.ParentID,
			LayerKey:   string(leaf.Azimuth),
			HoleKey:    &hk,
			Category:   domain.SampleOriginal,
			PickOrder:  leaf.PickOrder,
			Generation: generation,
		})
	}
	return nodes
}

// GetSampleTree loads the persisted sample tree of a task.
func (s *Service) GetSampleTree(ctx context.Context, taskID string) ([]store.SampleNode, error) {
	return s.store.ListSampleNodes(ctx, taskID)
}

// VerifyHoles atomically verifies a batch of sample leaf nodes. Any invalid or
// duplicate verification aborts the whole batch, and once all leaves are
// verified the task advances to loading.
func (s *Service) VerifyHoles(ctx context.Context, opID, taskID string, sampleIDs []string) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusHoleVerification {
		return nil, ErrInvalidTransition
	}
	digest := domain.Digest(sampleIDs)
	return s.store.VerifyHoles(ctx, opID, taskID, digest, sampleIDs, t.Version)
}

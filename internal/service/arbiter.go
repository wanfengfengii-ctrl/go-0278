package service

import (
	"context"
	"fmt"

	"rockbolt-pullout-zonal-closure/internal/domain"
	"rockbolt-pullout-zonal-closure/internal/store"
)

// Expand computes the unique influence domain around a failing bolt and commits
// the expanded samples. When boundary data is missing it routes the task to
// pending_review without guessing any expansion. It returns the influence digest
// (empty when the task went to pending_review).
func (s *Service) Expand(ctx context.Context, opID, taskID, failureSampleID string) (*domain.InspectionTask, string, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, "", err
	}
	switch t.Status {
	case domain.StatusLoading, domain.StatusExpanding, domain.StatusRetesting:
	default:
		return nil, "", ErrInvalidTransition
	}
	nodes, err := s.store.ListSampleNodes(ctx, taskID)
	if err != nil {
		return nil, "", err
	}
	var failure *store.SampleNode
	for i := range nodes {
		if nodes[i].ID == failureSampleID && nodes[i].HoleKey != nil && nodes[i].Generation == t.Generation {
			failure = &nodes[i]
			break
		}
	}
	if failure == nil {
		return nil, "", ErrTaskNotFound
	}
	holes, err := s.store.ListCandidateHoles(ctx, taskID)
	if err != nil {
		return nil, "", err
	}
	var failureHole domain.CandidateHole
	found := false
	for _, h := range holes {
		if h.HoleKey.Equal(*failure.HoleKey) {
			failureHole = h
			found = true
			break
		}
	}
	if !found {
		return nil, "", ErrBoundaryMissing
	}

	result := domain.ComputeInfluenceDomain(failureHole, holes, t.InfluenceBound)
	if !result.Complete {
		digest := domain.Digest(failureSampleID)
		updated, err := s.store.MarkExpansionIncomplete(ctx, opID, taskID, digest, t.Status, t.Version)
		if err != nil {
			return nil, "", err
		}
		return updated, "", nil
	}

	existing := make(map[domain.HoleKey]struct{})
	for _, n := range nodes {
		if n.HoleKey != nil {
			existing[*n.HoleKey] = struct{}{}
		}
	}
	influenceDigest := result.InfluenceDigest()
	var expanded []store.SampleNode
	for _, h := range result.Holes {
		if _, dup := existing[h.HoleKey]; dup {
			continue
		}
		hk := h.HoleKey
		failKey := failureHole.HoleKey
		expanded = append(expanded, store.SampleNode{
			ID:              expandedID(t.ID, h.HoleKey),
			TaskID:          taskID,
			LayerKey:        string(h.HoleKey.Azimuth),
			HoleKey:         &hk,
			Category:        domain.SampleExpanded,
			SourceFailure:   &failKey,
			InfluenceDigest: influenceDigest,
			Generation:      t.Generation,
		})
	}
	digest := domain.Digest(expanded)
	updated, err := s.store.CommitExpansion(ctx, opID, taskID, digest, expanded, influenceDigest, t.Status, t.Version)
	if err != nil {
		return nil, "", err
	}
	return updated, influenceDigest, nil
}

func expandedID(taskID string, k domain.HoleKey) string {
	return fmt.Sprintf("exp-%s-%d-%d-%s-%d", taskID, k.MileageMM, k.RingNo, k.Azimuth, k.HoleNo)
}

// SealReinforcement records a reinforcement against an influence domain and
// advances the task from expanding to pending_reinforce.
func (s *Service) SealReinforcement(ctx context.Context, opID, taskID, influenceDigest, reinforceDigest, reason string) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusExpanding {
		return nil, ErrInvalidTransition
	}
	digest := domain.Digest([]string{influenceDigest, reinforceDigest, reason})
	return s.store.SealReinforcement(ctx, opID, taskID, digest, influenceDigest, reinforceDigest, reason, t.Generation, s.Now(), t.Version)
}

// CreateRetest creates the next strictly monotonic inspection generation and
// clones the current leaf samples as retest samples in that generation, reusing
// the locked rules but starting an independent evidence chain.
func (s *Service) CreateRetest(ctx context.Context, opID, taskID string) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingReinforce {
		return nil, ErrInvalidTransition
	}
	nodes, err := s.store.ListSampleNodes(ctx, taskID)
	if err != nil {
		return nil, err
	}
	newGen := t.Generation + 1
	var retest []store.SampleNode
	for _, n := range nodes {
		if n.HoleKey == nil || n.Generation != t.Generation {
			continue
		}
		hk := n.HoleKey
		retest = append(retest, store.SampleNode{
			ID:         fmt.Sprintf("ret-%d-%s", newGen, n.ID),
			TaskID:     taskID,
			ParentID:   n.ParentID,
			LayerKey:   n.LayerKey,
			HoleKey:    hk,
			Category:   domain.SampleRetest,
			Generation: newGen,
		})
	}
	digest := domain.Digest(retest)
	return s.store.CreateRetestGeneration(ctx, opID, taskID, digest, retest, newGen, t.Version)
}

// Review records a qualified review signature.
func (s *Service) Review(ctx context.Context, opID, taskID, reviewer, qualSnapshot, signature string) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingReview {
		return nil, ErrInvalidTransition
	}
	row := store.ReviewRow{TaskID: taskID, Reviewer: reviewer, QualSnapshot: qualSnapshot, SignatureDigest: signature}
	digest := domain.Digest(row)
	return s.store.SubmitReview(ctx, opID, taskID, digest, row, t.Version)
}

// Finalize competes for the single final slot after validating that the current
// generation's samples are all closed and that two distinct, qualified reviewers
// have signed. It returns the winning task or an error for a lost competition.
func (s *Service) Finalize(ctx context.Context, opID, taskID string, verdict domain.VerdictType) (*domain.InspectionTask, error) {
	t, err := s.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if t.Status != domain.StatusPendingReview {
		return nil, ErrInvalidTransition
	}
	if open, err := s.store.CountUnclosedSamples(ctx, taskID, t.Generation); err != nil {
		return nil, err
	} else if open > 0 {
		return nil, ErrSamplesOpen
	}
	reviews, err := s.store.ListReviews(ctx, taskID)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	qualified := make(map[string]struct{})
	for _, r := range reviews {
		q, ok := domain.ParseQualification(r.QualSnapshot)
		if !ok || !q.ValidAt(now) {
			continue
		}
		qualified[r.Reviewer] = struct{}{}
	}
	if len(qualified) < 2 {
		return nil, ErrNotQualified
	}
	credential := fmt.Sprintf("verdict-%s-%s", taskID, verdict)
	sealDigest := domain.Digest([]string{taskID, string(verdict), credential})
	digest := domain.Digest([]string{taskID, string(verdict)})
	return s.store.Finalize(ctx, opID, taskID, digest, verdict, credential, sealDigest, t.Version)
}

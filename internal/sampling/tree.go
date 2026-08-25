package sampling

import (
	"fmt"
	"sort"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

// LayerNode is one spatial layer of the sample tree, keyed by arch azimuth.
type LayerNode struct {
	ID       string
	Azimuth  domain.Azimuth
	Quota    int
	Selected int
	ParentID string
}

// LeafNode is one sampled hole in the tree, carrying its deterministic pick
// order and canonical hole reference.
type LeafNode struct {
	ID        string
	ParentID  string
	Azimuth   domain.Azimuth
	HoleKey   domain.HoleKey
	PickOrder int
}

// Tree is the deterministic spatial stratified sample tree produced by one
// sampling run.
type Tree struct {
	Layers []LayerNode
	Leaves []LeafNode
}

// orderedAzimuths returns the documented azimuths in ordinal order, restricted
// to those that carry a non-zero quota.
func orderedAzimuths(quotas map[string]int) []domain.Azimuth {
	var out []domain.Azimuth
	for raw := range quotas {
		a, ok := domain.NormalizeAzimuth(raw)
		if ok && quotas[raw] > 0 {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		io, _ := domain.AzimuthOrdinal(out[i])
		jo, _ := domain.AzimuthOrdinal(out[j])
		return io < jo
	})
	return out
}

// BuildTree constructs the deterministic spatial stratified sample tree from
// locked candidate holes and layer quotas. Holes are grouped by azimuth, sorted
// canonically, shuffled with SplitMix64 and Fisher-Yates, and the quota prefix
// is taken per layer. Any quota that exceeds the available holes in its layer
// fails the whole sampling run. The result order is stable for a fixed seed and
// input, never depending on map iteration order.
func BuildTree(seed uint64, holes []domain.CandidateHole, quotas map[string]int) (Tree, error) {
	byLayer := make(map[domain.Azimuth][]domain.CandidateHole)
	for _, h := range holes {
		a := h.HoleKey.Azimuth
		byLayer[a] = append(byLayer[a], h)
	}
	for a := range byLayer {
		layer := byLayer[a]
		sort.Slice(layer, func(i, j int) bool { return layer[i].HoleKey.Less(layer[j].HoleKey) })
		byLayer[a] = layer
	}

	rng := NewSplitMix64(seed)
	var tree Tree
	leafSeq := 0
	layerSeq := 0

	for _, a := range orderedAzimuths(quotas) {
		quota := quotas[string(a)]
		layerHoles := byLayer[a]
		if quota > len(layerHoles) {
			return Tree{}, fmt.Errorf("%w: azimuth %q quota %d exceeds %d candidates", ErrQuotaInvalid, a, quota, len(layerHoles))
		}
		selected, err := SelectPrefix(rng, layerHoles, quota)
		if err != nil {
			return Tree{}, err
		}
		layerSeq++
		layerID := fmt.Sprintf("layer-%d-%s", layerSeq, a)
		parent := layerID
		tree.Layers = append(tree.Layers, LayerNode{
			ID:       layerID,
			Azimuth:  a,
			Quota:    quota,
			Selected: len(selected),
		})
		for i, h := range selected {
			leafSeq++
			tree.Leaves = append(tree.Leaves, LeafNode{
				ID:        fmt.Sprintf("sample-%d", leafSeq),
				ParentID:  parent,
				Azimuth:   a,
				HoleKey:   h.HoleKey,
				PickOrder: i + 1,
			})
		}
	}
	return tree, nil
}

// Digest returns the stable content digest of the tree: the ordered leaf hole
// keys plus their pick orders. It is independent of generated identifiers so
// that two identical sampling runs on different tasks produce identical digests.
func (t Tree) Digest() string {
	type row struct {
		Azimuth   domain.Azimuth `json:"azimuth"`
		PickOrder int            `json:"pick_order"`
		MileageMM int64          `json:"mileage_mm"`
		RingNo    int64          `json:"ring_no"`
		HoleNo    int64          `json:"hole_no"`
	}
	rows := make([]row, 0, len(t.Leaves))
	for _, l := range t.Leaves {
		rows = append(rows, row{
			Azimuth:   l.Azimuth,
			PickOrder: l.PickOrder,
			MileageMM: l.HoleKey.MileageMM,
			RingNo:    l.HoleKey.RingNo,
			HoleNo:    l.HoleKey.HoleNo,
		})
	}
	return domain.Digest(rows)
}

package vectorindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/qdrant/go-client/qdrant"
)

// SearchPolicy controls dense search mechanics without changing embedding identity.
// The zero value preserves server defaults and does not reconcile collection settings.
type SearchPolicy struct {
	// FullScanThresholdKB is a per-segment vector-byte budget in Qdrant KB units; zero preserves it.
	FullScanThresholdKB uint64
	// HNSWEf is the graph search budget; zero leaves the server default.
	HNSWEf uint64
	// Acorn enables conditional graph repair at query time.
	Acorn bool
	// MaxSelectivity is the estimated matching fraction below which ACORN is eligible.
	MaxSelectivity float64
}

// NewWithSearchPolicy constructs an index with an explicit operator policy; it performs no I/O.
func NewWithSearchPolicy(backend Backend, collection string, dimensions int, policy SearchPolicy) (*Index, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	index, err := New(backend, collection, dimensions)
	if err != nil {
		return nil, err
	}
	index.searchPolicy = policy
	return index, nil
}

// Validate rejects invalid or excessive operator search budgets.
func (p SearchPolicy) Validate() error {
	if p.FullScanThresholdKB > math.MaxInt32 || p.HNSWEf > 65536 {
		return errors.New("invalid Qdrant search policy threshold or ef exceeds supported bounds")
	}
	if math.IsNaN(p.MaxSelectivity) || math.IsInf(p.MaxSelectivity, 0) || p.MaxSelectivity < 0 || p.MaxSelectivity > 1 || (p.Acorn && p.MaxSelectivity == 0) {
		return errors.New("invalid Qdrant ACORN max selectivity: must be in (0,1] when enabled")
	}
	return nil
}

func (p SearchPolicy) collectionConfig() *qdrant.HnswConfigDiff {
	if p.FullScanThresholdKB == 0 {
		return nil
	}
	return &qdrant.HnswConfigDiff{FullScanThreshold: &p.FullScanThresholdKB}
}

func (i *Index) reconcileSearchPolicy(ctx context.Context, info *qdrant.CollectionInfo) error {
	if i.searchPolicy.FullScanThresholdKB == 0 || info == nil {
		return nil
	}
	if info.Config.HnswConfig == nil {
		return fmt.Errorf("collection %q has no readable HNSW configuration", i.collection)
	}
	if err := i.checkDenseThresholdOverride(info); err != nil {
		return err
	}
	if info.Config.HnswConfig.GetFullScanThreshold() == i.searchPolicy.FullScanThresholdKB {
		return nil
	}
	if len(info.Config.Params.GetVectorsConfig().GetParamsMap().GetMap()) != 1 {
		return fmt.Errorf("collection %q contains other dense vectors; reconcile their shared HNSW configuration explicitly", i.collection)
	}
	if err := i.backend.UpdateCollection(ctx, &qdrant.UpdateCollection{CollectionName: i.collection, HnswConfig: i.searchPolicy.collectionConfig()}); err != nil {
		return fmt.Errorf("reconcile collection %q full_scan_threshold: %w", i.collection, err)
	}
	return i.verifySearchPolicy(ctx)
}

func (i *Index) verifySearchPolicy(ctx context.Context) error {
	updated, err := i.backend.GetCollectionInfo(ctx, i.collection)
	if err != nil {
		return fmt.Errorf("verify collection %q search policy: %w", i.collection, err)
	}
	if updated == nil || updated.Config == nil || updated.Config.HnswConfig.GetFullScanThreshold() != i.searchPolicy.FullScanThresholdKB {
		return fmt.Errorf("collection %q did not apply full_scan_threshold", i.collection)
	}
	return nil
}

func (i *Index) checkDenseThresholdOverride(info *qdrant.CollectionInfo) error {
	dense := info.Config.Params.GetVectorsConfig().GetParamsMap().GetMap()[DenseVectorName]
	override := dense.GetHnswConfig()
	if override != nil && override.FullScanThreshold != nil && override.GetFullScanThreshold() != i.searchPolicy.FullScanThresholdKB {
		return fmt.Errorf("collection %q dense vector overrides full_scan_threshold; reconcile the named-vector override explicitly", i.collection)
	}
	return nil
}

func (i *Index) checkSearchSupport(ctx context.Context) error {
	if !i.searchPolicy.Acorn {
		return nil
	}
	i.policyMu.Lock()
	defer i.policyMu.Unlock()
	if i.policyServerChecked {
		return nil
	}
	health, err := i.backend.HealthCheck(ctx)
	if err != nil {
		return fmt.Errorf("check Qdrant search policy support: %w", err)
	}
	version := health.GetVersion()
	major, minor := 0, 0
	_, err = fmt.Sscanf(strings.TrimPrefix(version, "v"), "%d.%d", &major, &minor)
	if err != nil || major != 1 || minor < 16 {
		return fmt.Errorf("ACORN requires Qdrant 1.16 or newer compatible 1.x server; got %q", version)
	}
	i.policyServerChecked = true
	return nil
}

func (p SearchPolicy) params() *qdrant.SearchParams {
	if p.HNSWEf == 0 && !p.Acorn {
		return nil
	}
	params := &qdrant.SearchParams{}
	if p.HNSWEf > 0 {
		params.HnswEf = &p.HNSWEf
	}
	if p.Acorn {
		params.Acorn = &qdrant.AcornSearchParams{Enable: qdrant.PtrOf(true), MaxSelectivity: &p.MaxSelectivity}
	}
	return params
}

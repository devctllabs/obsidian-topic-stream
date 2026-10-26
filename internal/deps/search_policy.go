package deps

import (
	"fmt"

	generated "github.com/ethernity/obsidian-topic-stream/gen/config"
	"github.com/ethernity/obsidian-topic-stream/internal/platform/vectorindex"
)

func qdrantSearchPolicy(cfg generated.RuntimeConfig) (vectorindex.SearchPolicy, error) {
	if cfg.QdrantFullScanThresholdKb < 0 || cfg.QdrantHnswEf < 0 || cfg.QdrantAcornMaxSelectivityPercent < 0 || cfg.QdrantAcornMaxSelectivityPercent > 100 {
		return vectorindex.SearchPolicy{}, fmt.Errorf("invalid Qdrant search policy: requires nonnegative threshold/ef and selectivity percent in 0..100")
	}
	policy := vectorindex.SearchPolicy{FullScanThresholdKB: uint64(cfg.QdrantFullScanThresholdKb), HNSWEf: uint64(cfg.QdrantHnswEf), Acorn: cfg.QdrantAcornEnabled, MaxSelectivity: float64(cfg.QdrantAcornMaxSelectivityPercent) / 100}
	if err := policy.Validate(); err != nil {
		return vectorindex.SearchPolicy{}, fmt.Errorf("invalid Qdrant search policy: %w", err)
	}
	return policy, nil
}

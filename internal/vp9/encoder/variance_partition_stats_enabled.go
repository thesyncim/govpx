//go:build govpx_phase_stats

package encoder

const choosePartitioningStatsEnabled = true

type choosePartitioningStatsArg struct {
	Stats *ChoosePartitioningStats
}

func choosePartitioningStats(a *ChoosePartitioningArgs) *ChoosePartitioningStats {
	if a == nil {
		return nil
	}
	return a.Stats
}

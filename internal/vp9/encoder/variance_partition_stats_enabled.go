//go:build govpx_phase_stats

package encoder

const choosePartitioningStatsEnabled = true

type choosePartitioningStatsArg struct {
	Stats *ChoosePartitioningStats
}

func choosePartitioningStats(a choosePartitioningStatsArg) *ChoosePartitioningStats {
	return a.Stats
}

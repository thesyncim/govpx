//go:build !govpx_phase_stats

package encoder

const choosePartitioningStatsEnabled = false

type choosePartitioningStatsArg struct{}

func choosePartitioningStats(*ChoosePartitioningArgs) *ChoosePartitioningStats {
	return nil
}

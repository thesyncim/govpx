//go:build !govpx_phase_stats

package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func setVTPartitioningWithStats(miGrid []vp9dec.NeighborMi, miRows, miCols, miRow, miCol int,
	bsize, bsizeMin common.BlockSize, threshold int64, forceSplit bool, isKeyFrame bool,
	args setVTPartitioningArgs,
	chromaPlaneBlockOK func(subsize common.BlockSize) bool,
	_ *ChoosePartitioningStats,
) bool {
	return setVTPartitioning(miGrid, miRows, miCols, miRow, miCol,
		bsize, bsizeMin, threshold, forceSplit, isKeyFrame, args,
		chromaPlaneBlockOK)
}

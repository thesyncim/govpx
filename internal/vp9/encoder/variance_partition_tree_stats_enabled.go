//go:build govpx_phase_stats

package encoder

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func setVTPartitioningWithStats(miGrid []vp9dec.NeighborMi, miRows, miCols, miRow, miCol int,
	bsize, bsizeMin common.BlockSize, threshold int64, forceSplit bool, isKeyFrame bool,
	args setVTPartitioningArgs,
	chromaPlaneBlockOK func(subsize common.BlockSize) bool,
	stats *ChoosePartitioningStats,
) bool {
	blockWidth := int(common.Num8x8BlocksWideLookup[bsize])
	blockHeight := int(common.Num8x8BlocksHighLookup[bsize])
	if blockWidth != blockHeight {
		panic("setVTPartitioning: non-square bsize")
	}
	part := partitionVarianceFor(bsize, args)
	stats.countSetVTCall(bsize)

	if forceSplit {
		stats.countSetVTForceSplit(bsize)
		stats.countSetVTSplit()
		return false
	}

	if bsize == bsizeMin {
		if isKeyFrame {
			getVariance(&part.None)
		}
		if miCol+blockWidth/2 < miCols &&
			miRow+blockHeight/2 < miRows &&
			int64(part.None.Variance) < threshold {
			setBlockSize(miGrid, miRows, miCols, miRow, miCol, bsize)
			stats.countSetVTSelect()
			return true
		}
		stats.countSetVTSplit()
		return false
	} else if bsize > bsizeMin {
		if isKeyFrame {
			getVariance(&part.None)
		}
		if isKeyFrame &&
			(bsize > common.Block32x32 ||
				int64(part.None.Variance) > (threshold<<4)) {
			stats.countSetVTSplit()
			return false
		}
		if miCol+blockWidth/2 < miCols &&
			miRow+blockHeight/2 < miRows &&
			int64(part.None.Variance) < threshold {
			setBlockSize(miGrid, miRows, miCols, miRow, miCol, bsize)
			stats.countSetVTSelect()
			return true
		}

		if miRow+blockHeight/2 < miRows {
			subsize := common.SubsizeLookup[common.PartitionVert][bsize]
			getVariance(&part.Vert[0])
			getVariance(&part.Vert[1])
			chromaOK := true
			if chromaPlaneBlockOK != nil {
				chromaOK = chromaPlaneBlockOK(subsize)
			}
			if int64(part.Vert[0].Variance) < threshold &&
				int64(part.Vert[1].Variance) < threshold &&
				chromaOK {
				setBlockSize(miGrid, miRows, miCols, miRow, miCol, subsize)
				setBlockSize(miGrid, miRows, miCols, miRow, miCol+blockWidth/2, subsize)
				stats.countSetVTSelect()
				return true
			}
		}
		if miCol+blockWidth/2 < miCols {
			subsize := common.SubsizeLookup[common.PartitionHorz][bsize]
			getVariance(&part.Horz[0])
			getVariance(&part.Horz[1])
			chromaOK := true
			if chromaPlaneBlockOK != nil {
				chromaOK = chromaPlaneBlockOK(subsize)
			}
			if int64(part.Horz[0].Variance) < threshold &&
				int64(part.Horz[1].Variance) < threshold &&
				chromaOK {
				setBlockSize(miGrid, miRows, miCols, miRow, miCol, subsize)
				setBlockSize(miGrid, miRows, miCols, miRow+blockHeight/2, miCol, subsize)
				stats.countSetVTSelect()
				return true
			}
		}

		stats.countSetVTSplit()
		return false
	}
	stats.countSetVTSplit()
	return false
}

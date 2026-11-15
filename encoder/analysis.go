package encoder

import "github.com/cry5tallize/goavc/internal/picture"

// frameAnalysis只在一次RC调用内存在；源图和已提交参考在全部QP尝试间不可变。
// 三个独立平面各保留整帧/顶场/底场槽，不按QP创建缓存，不跨AU借用图像。
// 场通道必须沿用整帧拟合权重，AQ则按真实FieldView采样，不能混用槽。
type frameAnalysis struct {
	slots [9]analysisSlot
}

type analysisSlot struct {
	activity      []int8
	weights       PredictionWeights
	fitted        bool
	search        *picture.Picture
	searchWeights PredictionWeights
}

func (e *Encoder) predictionWeights(src, ref *picture.Picture, planes int) PredictionWeights {
	if e.analysis == nil {
		return estimatePredictionWeights(src, ref, planes)
	}
	slot := &e.analysis.slots[e.analysisIndex]
	if !slot.fitted {
		slot.weights = estimatePredictionWeights(src, ref, planes)
		slot.fitted = true
	}
	// 按值返回，候选不能持有整个调用缓存而延长其他平面和源图的生命周期。
	return slot.weights
}

func (e *Encoder) searchReference(ref *picture.Picture, weights *PredictionWeights, planes int) *picture.Picture {
	if e.analysis == nil {
		return weightedSearchReference(ref, weights, planes)
	}
	slot := &e.analysis.slots[e.analysisIndex]
	if slot.search == nil || slot.searchWeights != *weights {
		slot.search = weightedSearchReference(ref, weights, planes)
		slot.searchWeights = *weights
	}
	return slot.search
}

func (e *Encoder) activities(src *picture.Picture) []int8 {
	if e.analysis == nil || e.adaptiveQP == 0 {
		return nil
	}
	slot := &e.analysis.slots[e.analysisIndex]
	if slot.activity == nil {
		slot.activity = make([]int8, src.MBWidth*src.MBHeight)
		for addr := range slot.activity {
			slot.activity[addr] = activityClass(&src.Planes[0], addr%src.MBWidth*16, addr/src.MBWidth*16)
		}
	}
	return slot.activity
}

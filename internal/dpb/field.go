package dpb

import (
	"fmt"

	h264 "github.com/goavc/h264"
	"github.com/goavc/h264/internal/picture"
)

// FieldReference 返回MBAFF场列表的借用参考；帧仍是唯一DPB/图像池所有者。
// 同一帧的两场使用独立轻量身份，身份只引用原帧身份，不形成图像参考链。
// 视图只在首次使用时构造，后续更新长期标记而不重复分配像素或描述符。
func (r *Reference) FieldReference(bottom bool) (*Reference, error) {
	if r == nil || r.Identity == nil || r.Picture == nil || r.FrameReference != nil {
		return nil, fmt.Errorf("dpb: invalid frame reference for field view: %w", h264.ErrInvalidData)
	}
	parity := 0
	if bottom {
		parity = 1
	}
	field := r.fields[parity]
	if field == nil {
		view, err := picture.FieldView(r.Picture, bottom)
		if err != nil {
			return nil, err
		}
		field = &Reference{Identity: &Identity{ID: r.ID, FrameNum: r.FrameNum, POC: r.FieldPOC[parity], Frame: r.Identity, BottomField: bottom}, Picture: &view, FrameReference: r}
		r.fields[parity] = field
	}
	field.Identity.LongTerm = r.LongTerm
	field.Identity.LongTermIdx = 2*r.LongTermIdx + uint32(parity)
	return field, nil
}

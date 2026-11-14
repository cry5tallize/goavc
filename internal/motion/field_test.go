package motion

import "testing"

// 8.4.1.3.2 的 / 是向零截断，不是带符号右移；以下预期值均为规范手算常量。
func TestNormalizeFieldNeighbor(t *testing.T) {
	tests := []struct {
		name    string
		current FieldStructure
		input   FieldNeighbor
		want    FieldNeighbor
	}{
		{"frame_to_top_negative_odd", TopFieldMotion, FieldNeighbor{MV{11, -7}, 3, FrameMotion, true}, FieldNeighbor{MV{11, -3}, 6, TopFieldMotion, true}},
		{"frame_to_bottom_negative_one", BottomFieldMotion, FieldNeighbor{MV{-5, -1}, 2, FrameMotion, true}, FieldNeighbor{MV{-5, 0}, 4, BottomFieldMotion, true}},
		{"frame_to_bottom_positive_odd", BottomFieldMotion, FieldNeighbor{MV{9, 7}, 0, FrameMotion, true}, FieldNeighbor{MV{9, 3}, 0, BottomFieldMotion, true}},
		{"top_to_frame_even_ref", FrameMotion, FieldNeighbor{MV{-9, -3}, 6, TopFieldMotion, true}, FieldNeighbor{MV{-9, -6}, 3, FrameMotion, true}},
		{"bottom_to_frame_odd_ref", FrameMotion, FieldNeighbor{MV{5, 5}, 7, BottomFieldMotion, true}, FieldNeighbor{MV{5, 10}, 3, FrameMotion, true}},
		{"top_to_bottom_keeps_relative_ref", BottomFieldMotion, FieldNeighbor{MV{3, -5}, 1, TopFieldMotion, true}, FieldNeighbor{MV{3, -5}, 1, BottomFieldMotion, true}},
		{"bottom_to_top_keeps_relative_ref", TopFieldMotion, FieldNeighbor{MV{-2, 9}, 2, BottomFieldMotion, true}, FieldNeighbor{MV{-2, 9}, 2, TopFieldMotion, true}},
		{"frame_to_frame", FrameMotion, FieldNeighbor{MV{13, -11}, 4, FrameMotion, true}, FieldNeighbor{MV{13, -11}, 4, FrameMotion, true}},
		{"maximum_frame_reference", BottomFieldMotion, FieldNeighbor{MV{1, -9}, 31, FrameMotion, true}, FieldNeighbor{MV{1, -4}, 62, BottomFieldMotion, true}},
		{"maximum_field_reference", FrameMotion, FieldNeighbor{MV{1, -9}, 63, BottomFieldMotion, true}, FieldNeighbor{MV{1, -18}, 31, FrameMotion, true}},
		{"unavailable_discards_stale_motion", TopFieldMotion, FieldNeighbor{MV{99, -99}, 3, FrameMotion, false}, FieldNeighbor{MV{}, -1, TopFieldMotion, false}},
		{"intra_stays_available", FrameMotion, FieldNeighbor{MV{99, -99}, -1, BottomFieldMotion, true}, FieldNeighbor{MV{}, -1, FrameMotion, true}},
		{"unused_list_not_scaled", BottomFieldMotion, FieldNeighbor{MV{99, -99}, -1, FrameMotion, true}, FieldNeighbor{MV{}, -1, BottomFieldMotion, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeFieldNeighbor(tt.current, tt.input); got != tt.want {
				t.Fatalf("NormalizeFieldNeighbor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMBAFFFieldReference(t *testing.T) {
	tests := []struct {
		name    string
		current FieldStructure
		ref     int8
		frame   int8
		field   FieldStructure
		ok      bool
	}{
		{"top_same", TopFieldMotion, 0, 0, TopFieldMotion, true},
		{"top_cross", TopFieldMotion, 1, 0, BottomFieldMotion, true},
		{"bottom_same", BottomFieldMotion, 0, 0, BottomFieldMotion, true},
		{"bottom_cross", BottomFieldMotion, 1, 0, TopFieldMotion, true},
		{"top_second_frame_same", TopFieldMotion, 2, 1, TopFieldMotion, true},
		{"bottom_second_frame_cross", BottomFieldMotion, 3, 1, TopFieldMotion, true},
		{"largest_field_ref", TopFieldMotion, 63, 31, BottomFieldMotion, true},
		{"unused_list", BottomFieldMotion, -1, -1, FrameMotion, false},
		{"frame_not_field_list", FrameMotion, 2, -1, FrameMotion, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame, field, ok := MBAFFFieldReference(tt.current, tt.ref)
			if frame != tt.frame || field != tt.field || ok != tt.ok {
				t.Fatalf("MBAFFFieldReference() = (%d, %d, %v), want (%d, %d, %v)", frame, field, ok, tt.frame, tt.field, tt.ok)
			}
		})
	}
}

func TestPredictField(t *testing.T) {
	tests := []struct {
		name      string
		current   FieldStructure
		ref       int8
		partition FieldPartition
		neighbors FieldNeighbors
		want      MV
	}{
		{
			name: "no_neighbors", current: TopFieldMotion, ref: 0, partition: FieldMedian,
			want: MV{},
		},
		{
			name: "frame_neighbor_unique_match_after_normalization", current: BottomFieldMotion, ref: 4, partition: FieldMedian,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{17, -7}, 2, FrameMotion, true},
				B: FieldNeighbor{MV{31, 10}, 1, BottomFieldMotion, true},
				C: FieldNeighbor{MV{-9, -10}, 3, TopFieldMotion, true},
			},
			want: MV{17, -3},
		},
		{
			name: "median_of_mixed_frame_and_field_units", current: TopFieldMotion, ref: 0, partition: FieldMedian,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{9, -7}, 0, FrameMotion, true},
				B: FieldNeighbor{MV{-5, -1}, 0, TopFieldMotion, true},
				C: FieldNeighbor{MV{3, 5}, 0, BottomFieldMotion, true},
			},
			want: MV{3, -1},
		},
		{
			name: "field_references_collapse_to_same_frame", current: FrameMotion, ref: 2, partition: FieldMedian,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{9, -3}, 4, TopFieldMotion, true},
				B: FieldNeighbor{MV{1, 5}, 5, BottomFieldMotion, true},
				C: FieldNeighbor{MV{4, 2}, 2, FrameMotion, true},
			},
			want: MV{4, 2},
		},
		{
			name: "unavailable_right_top_uses_normalized_left_top", current: TopFieldMotion, ref: 2, partition: FieldMedian,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{3, 4}, 0, TopFieldMotion, true},
				B: FieldNeighbor{MV{9, 10}, 1, TopFieldMotion, true},
				C: FieldNeighbor{MV{99, 99}, 2, TopFieldMotion, false},
				D: FieldNeighbor{MV{-7, -9}, 1, FrameMotion, true},
			},
			want: MV{-7, -4},
		},
		{
			name: "available_intra_right_top_does_not_fall_back", current: BottomFieldMotion, ref: 2, partition: FieldMedian,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{3, 4}, 0, BottomFieldMotion, true},
				B: FieldNeighbor{MV{9, 10}, 1, BottomFieldMotion, true},
				C: FieldNeighbor{MV{99, 99}, -1, FrameMotion, true},
				D: FieldNeighbor{MV{-7, -9}, 1, FrameMotion, true},
			},
			want: MV{3, 4},
		},
		{
			name: "left_only_without_reference_match", current: BottomFieldMotion, ref: 3, partition: FieldMedian,
			neighbors: FieldNeighbors{A: FieldNeighbor{MV{7, -11}, 0, FrameMotion, true}},
			want:      MV{7, -5},
		},
		{
			name: "field_16x8_top_prefers_b_after_scaling", current: TopFieldMotion, ref: 2, partition: Field16x8Top,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{1, 2}, 2, TopFieldMotion, true},
				B: FieldNeighbor{MV{9, -7}, 1, FrameMotion, true},
				C: FieldNeighbor{MV{5, 8}, 2, TopFieldMotion, true},
			},
			want: MV{9, -3},
		},
		{
			name: "frame_16x8_bottom_prefers_a_after_scaling", current: FrameMotion, ref: 1, partition: Field16x8Bottom,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{-9, -5}, 3, BottomFieldMotion, true},
				B: FieldNeighbor{MV{1, 2}, 1, FrameMotion, true},
				C: FieldNeighbor{MV{5, 8}, 1, FrameMotion, true},
			},
			want: MV{-9, -10},
		},
		{
			name: "field_8x16_left_prefers_a", current: BottomFieldMotion, ref: 0, partition: Field8x16Left,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{-9, -5}, 0, FrameMotion, true},
				B: FieldNeighbor{MV{1, 2}, 0, BottomFieldMotion, true},
				C: FieldNeighbor{MV{5, 8}, 0, BottomFieldMotion, true},
			},
			want: MV{-9, -2},
		},
		{
			name: "field_8x16_right_prefers_c", current: TopFieldMotion, ref: 0, partition: Field8x16Right,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{-9, -5}, 0, TopFieldMotion, true},
				B: FieldNeighbor{MV{1, 2}, 0, TopFieldMotion, true},
				C: FieldNeighbor{MV{13, -11}, 0, FrameMotion, true},
			},
			want: MV{13, -5},
		},
		{
			name: "field_8x16_right_falls_back_to_d", current: BottomFieldMotion, ref: 2, partition: Field8x16Right,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{-9, -5}, 2, BottomFieldMotion, true},
				B: FieldNeighbor{MV{1, 2}, 2, BottomFieldMotion, true},
				D: FieldNeighbor{MV{13, -11}, 1, FrameMotion, true},
			},
			want: MV{13, -5},
		},
		{
			name: "special_partition_reference_mismatch_uses_median", current: TopFieldMotion, ref: 2, partition: Field16x8Top,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{-9, -5}, 2, TopFieldMotion, true},
				B: FieldNeighbor{MV{13, 19}, 0, FrameMotion, true},
				C: FieldNeighbor{MV{1, 2}, 2, TopFieldMotion, true},
			},
			want: MV{1, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PredictField(tt.current, tt.ref, tt.partition, tt.neighbors); got != tt.want {
				t.Fatalf("PredictField() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestPredictFieldSkip(t *testing.T) {
	tests := []struct {
		name      string
		current   FieldStructure
		neighbors FieldNeighbors
		want      MV
	}{
		{
			name: "unavailable_left", current: TopFieldMotion,
			neighbors: FieldNeighbors{B: FieldNeighbor{MV{9, 9}, 0, TopFieldMotion, true}},
			want:      MV{},
		},
		{
			name: "scaled_negative_one_becomes_zero_shortcut", current: BottomFieldMotion,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{0, -1}, 0, FrameMotion, true},
				B: FieldNeighbor{MV{9, 9}, 0, BottomFieldMotion, true},
				C: FieldNeighbor{MV{7, 7}, 0, BottomFieldMotion, true},
			},
			want: MV{},
		},
		{
			name: "zero_with_other_reference_is_not_skip_shortcut", current: TopFieldMotion,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{}, 2, TopFieldMotion, true},
				B: FieldNeighbor{MV{9, -7}, 0, FrameMotion, true},
				C: FieldNeighbor{MV{7, 5}, 1, BottomFieldMotion, true},
			},
			want: MV{9, -3},
		},
		{
			name: "nonzero_mixed_neighbors_use_normalized_prediction", current: FrameMotion,
			neighbors: FieldNeighbors{
				A: FieldNeighbor{MV{9, -3}, 0, TopFieldMotion, true},
				B: FieldNeighbor{MV{1, 5}, 1, BottomFieldMotion, true},
				C: FieldNeighbor{MV{4, 2}, 0, FrameMotion, true},
			},
			want: MV{4, 2},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PredictFieldSkip(tt.current, tt.neighbors); got != tt.want {
				t.Fatalf("PredictFieldSkip() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

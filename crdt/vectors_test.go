package crdt

import (
	"encoding/hex"
	"testing"
)

func TestVectors(t *testing.T) {
	insertL := InsertOp{ID: ID{3, 0}, Origin: StartID, RightOrigin: EndID, Content: 'l'}
	deleteL := DeleteOp{ID: ID{3, 1}, Target: ID{3, 0}}
	insertE := InsertOp{ID: ID{3, 0}, Origin: StartID, RightOrigin: EndID, Content: 'é'}

	tests := []struct {
		name string
		got  []byte
		want string
	}{
		{"insert l", EncodeOps([]Op{insertL}), "01010300000000016c"},
		{"insert l then delete", EncodeOps([]Op{insertL, deleteL}), "02010300000000016c0203010300"},
		{"insert é", EncodeOps([]Op{insertE}), "0101030000000001e901"},
		{"state vector {3: 2}", EncodeSV(map[ClientID]uint64{3: 2}), "010302"},
	}

	for _, tc := range tests {
		if got := hex.EncodeToString(tc.got); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

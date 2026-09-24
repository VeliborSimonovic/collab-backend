package crdt

type ClientID uint64

type ID struct {
	Client ClientID
	Clock  uint64
}

var (
	StartID = ID{Client: 0, Clock: 0}
	EndID   = ID{Client: 0, Clock: 1}
)

type Item struct {
	ID          ID
	Origin      ID
	RightOrigin ID
	Content     rune
	Deleted     bool
	left, right *Item
}

type InsertOp struct {
	ID          ID
	Origin      ID
	RightOrigin ID
	Content     rune
}

type DeleteOp struct {
	ID     ID
	Target ID
}

type Op interface {
	OpID() ID
}

func (o InsertOp) OpID() ID { return o.ID }
func (o DeleteOp) OpID() ID { return o.ID }

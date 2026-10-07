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
	Parent      Parent
	Key         string
	CKind       ContentKind
	JSON        []byte
	Type        Kind
	left, right *Item
	seq         *seq
}

type InsertOp struct {
	ID          ID
	Origin      ID
	RightOrigin ID
	Content     rune
	Parent      Parent
	Key         string
	CKind       ContentKind
	JSON        []byte
	Type        Kind
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

type Kind uint8

const (
	KindText  Kind = 0
	KindArray Kind = 1
	KindMap   Kind = 2
)

type ContentKind uint8

const (
	ContentRune ContentKind = 0
	ContentJSON ContentKind = 1
	ContentType ContentKind = 2
)

type Parent struct {
	Root     string
	RootKind Kind
	Item     ID
}

func (p Parent) IsRoot() bool {
	return p.Item.Client == 0
}

func RootParent(name string, kind Kind) Parent {
	return Parent{Root: name, RootKind: kind}
}

func ItemParent(id ID) Parent {
	return Parent{Item: id}
}

func DefaultText() Parent {
	return RootParent("", KindText)
}

func (k Kind) String() string {
	switch k {
	case KindText:
		return "text"
	case KindArray:
		return "array"
	case KindMap:
		return "map"
	}
	return "unknown"
}

package crdt

import (
	"errors"
	"reflect"
	"testing"
)

func TestRefRoundTrip(t *testing.T) {
	for _, p := range []Parent{
		DefaultText(),
		RootParent("slides", KindMap),
		RootParent("a:b", KindMap),
		ItemParent(ID{1048577, 42}),
	} {
		s := FormatRef(p)
		got, err := ParseRef(s)
		if err != nil || got != p {
			t.Fatalf("%q: got %+v, %v; want %+v", s, got, err, p)
		}
	}
	if s := FormatRef(ItemParent(ID{1048577, 42})); s != "i:1048577:42" {
		t.Fatalf("got %q", s)
	}
}

func TestParseRefBad(t *testing.T) {
	for _, s := range []string{"x:1", "r:blob:x", "i:0:5", "i:abc:1", "r:map"} {
		if _, err := ParseRef(s); !errors.Is(err, ErrBadRef) {
			t.Fatalf("%q: err = %v, want ErrBadRef", s, err)
		}
	}
}

func TestChangedParents(t *testing.T) {
	d := NewDoc(1)
	ins, _ := d.LocalInsert(0, 'x')
	set1, _ := d.MapSetJSON(mRoot, "a", js("1"))
	del, _ := d.LocalDelete(0)
	set2, _ := d.MapSetJSON(mRoot, "b", js("2"))
	_ = ins
	got := d.ChangedParents([]Op{set1, del, set2})
	want := []Parent{mRoot, DefaultText()}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

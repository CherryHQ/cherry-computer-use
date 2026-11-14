package desktop

import (
	"github.com/godbus/dbus/v5"
	"slices"
	"testing"
)

func TestTreeBudgetKeepsVisibleDialogControlsBeforeLongList(t *testing.T) {
	ref := func(name string) Ref { return Ref{Bus: "fixture", Path: dbus.ObjectPath("/" + name)} }
	root, list, button := ref("window"), ref("list"), ref("button")
	rows := []Ref{ref("row1"), ref("row2"), ref("row3")}
	children := map[Ref][]Ref{root: {list, button}, list: rows}
	inspect := func(r Ref) (treeTraits, error) { return treeTraits{control: r == button, collection: r == list}, nil }
	result, err := planTree(root, 0, TreeOptions{MaxNodes: 3, MaxDepth: 64}, inspect, func(r Ref) ([]Ref, error) { return children[r], nil })
	if err != nil {
		t.Fatal(err)
	}
	got := []Ref{}
	for _, node := range result.nodes {
		got = append(got, node.ref)
	}
	if !slices.Equal(got, []Ref{root, list, button}) || !result.nodesTruncated {
		t.Fatalf("visible button lost to early collection: %+v", result)
	}
	if result.nodes[2].parent != root || result.nodes[2].path[1] != 1 {
		t.Fatal("document identity or parent changed while prioritizing")
	}
	shallow, err := planTree(root, 0, TreeOptions{MaxNodes: 50, MaxDepth: 0}, inspect, func(r Ref) ([]Ref, error) { return children[r], nil })
	if err != nil || len(shallow.nodes) != 1 || !shallow.depthTruncated || shallow.nodesTruncated {
		t.Fatalf("depth budget misreported: %+v %v", shallow, err)
	}
}

func TestClickPrefersClickOverGTKEntryActivation(t *testing.T) {
	action, ok := ClickAction([]Action{{Index: 0, Name: "activate"}, {Index: 1, Name: "click"}})
	if !ok || action.Index != 1 {
		t.Fatal("entry activation could submit its form instead of focusing it")
	}
}

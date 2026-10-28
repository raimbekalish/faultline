package lab

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func syntheticPlan(t *testing.T) Plan {
	t.Helper()
	p, e := buildPlan(context.Background(), descriptor(t), testSnapshot(t), fixedEntropy())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestClosedGraph(t *testing.T) {
	p := syntheticPlan(t)
	g, e := GenerateGraph(p)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(g)
	g2, _ := GenerateGraph(p)
	b, _ := json.Marshal(g2)
	if string(a) != string(b) {
		t.Fatal("not deterministic")
	}
	expected := [][]ResourceKey{{Edge, Data}, {Edge, Control}, {Backend}, {Control, Data, Backend}}
	for i, c := range g.Containers {
		var actual []ResourceKey
		for _, m := range c.Memberships {
			actual = append(actual, m.Network)
		}
		if !reflect.DeepEqual(actual, expected[i]) {
			t.Fatal(c.Key, actual)
		}
		if c.ImageID != p.Components[i].ImageID {
			t.Fatal("image mismatch")
		}
	}
	for _, n := range g.Networks {
		if !n.Profile.Internal || n.Profile.IPv6 || n.Profile.GatewayMode != "isolated" {
			t.Fatal("network profile")
		}
	}
	for _, change := range []func(*Graph){func(g *Graph) { g.Profile.Privileged = true }, func(g *Graph) { g.Profile.HostMounts = true }, func(g *Graph) { g.Profile.PublishedPorts = true }, func(g *Graph) { g.Networks[0].Profile.Internal = false }, func(g *Graph) {
		g.Containers[0].Memberships = append(g.Containers[0].Memberships, Membership{Backend, "bypass"})
	}, func(g *Graph) { g.Containers[0].Role.Command = []string{"override"} }, func(g *Graph) { g.Containers[0].ImageID = "tag:latest" }, func(g *Graph) { g.Qualification = "QUALIFIED" }} {
		x, _ := GenerateGraph(p)
		change(&x)
		if x.Validate(p) == nil {
			t.Fatal("altered graph accepted")
		}
	}
	for _, k := range resourceKeys {
		if _, _, e := g.resource(k); e != nil {
			t.Fatal(e)
		}
	}
	if _, _, e := g.resource("foreign"); e == nil {
		t.Fatal("foreign key")
	}
}

package lab

import (
	"encoding/json"
	"errors"
	"reflect"
)

var ErrGraph = errors.New("invalid closed lab resource graph")

type ResourceKey string

const (
	Edge        ResourceKey = "network.edge"
	Data        ResourceKey = "network.data"
	Control     ResourceKey = "network.control"
	Backend     ResourceKey = "network.backend"
	Application ResourceKey = "container.application"
	Runner      ResourceKey = "container.runner"
	Fixture     ResourceKey = "container.fixture"
	Proxy       ResourceKey = "container.toxiproxy"
)

var resourceKeys = [8]ResourceKey{Edge, Data, Control, Backend, Application, Runner, Fixture, Proxy}

type PlannedNetwork struct {
	Key     ResourceKey `json:"key"`
	Name    string      `json:"name"`
	Profile Network     `json:"profile"`
}
type Membership struct {
	Network ResourceKey `json:"network"`
	Alias   string      `json:"alias"`
}
type PlannedContainer struct {
	Key         ResourceKey  `json:"key"`
	Name        string       `json:"name"`
	ImageID     string       `json:"image_id"`
	Role        Role         `json:"role"`
	Memberships []Membership `json:"memberships"`
}

// Graph is private intended configuration, never a Docker/Compose file or proof
// of effective isolation. The catalog supplies every security/resource ceiling.
type Graph struct {
	SchemaVersion int                 `json:"schema_version"`
	Kind          string              `json:"kind"`
	Qualification string              `json:"qualification"`
	PlanSHA256    string              `json:"plan_sha256"`
	LaunchID      string              `json:"launch_id"`
	OwnerNonce    string              `json:"owner_nonce"`
	Profile       Definition          `json:"profile"`
	Networks      [4]PlannedNetwork   `json:"networks"`
	Containers    [4]PlannedContainer `json:"containers"`
}

func GenerateGraph(p Plan) (Graph, error) {
	if p.Validate() != nil {
		return Graph{}, ErrGraph
	}
	g := graphFor(p)
	return g, g.Validate(p)
}
func graphFor(p Plan) Graph {
	g := Graph{SchemaVersion: 1, Kind: "faultline-lab-resource-graph", Qualification: "INTENT_ONLY", PlanSHA256: p.AuthorizationSHA256, LaunchID: p.Identity.LaunchID, OwnerNonce: p.Identity.AuthorizationNonce, Profile: Catalog()}
	for i, n := range g.Profile.Networks {
		g.Networks[i] = PlannedNetwork{resourceKeys[i], p.Identity.LaunchID + "-" + n.Name, n}
	}
	for i, r := range g.Profile.Roles {
		c := PlannedContainer{Key: resourceKeys[i+4], Name: p.Identity.LaunchID + "-" + r.Name, ImageID: p.Components[i].ImageID, Role: r, Memberships: []Membership{}}
		for _, n := range r.Networks {
			alias := r.Name
			switch c.Key {
			case Application:
				alias = g.Profile.FrontendAlias
			case Fixture:
				alias = g.Profile.DependencyAlias
			case Proxy:
				alias = "toxiproxy"
				if n == "control" {
					alias = g.Profile.ControlAlias
				}
			}
			c.Memberships = append(c.Memberships, Membership{ResourceKey("network." + n), alias})
		}
		g.Containers[i] = c
	}
	return g
}
func (g Graph) Validate(p Plan) error {
	if p.Validate() != nil || !reflect.DeepEqual(g, graphFor(p)) {
		return ErrGraph
	}
	return nil
}
func (g Graph) Hash(p Plan) (string, error) {
	if err := g.Validate(p); err != nil {
		return "", err
	}
	b, err := json.Marshal(g)
	if err != nil {
		return "", ErrGraph
	}
	return hash(b), nil
}
func resourceIndex(k ResourceKey) int {
	for i, v := range resourceKeys {
		if v == k {
			return i
		}
	}
	return -1
}
func (g Graph) resource(k ResourceKey) (string, string, error) {
	i := resourceIndex(k)
	if i < 0 {
		return "", "", ErrGraph
	}
	var value any
	var name string
	if i < 4 {
		value = g.Networks[i]
		name = g.Networks[i].Name
	} else {
		value = g.Containers[i-4]
		name = g.Containers[i-4].Name
	}
	b, err := json.Marshal(struct {
		Plan     string `json:"plan"`
		Nonce    string `json:"owner_nonce"`
		Resource any    `json:"resource"`
	}{g.PlanSHA256, g.OwnerNonce, value})
	if err != nil {
		return "", "", ErrGraph
	}
	return name, hash(b), nil
}

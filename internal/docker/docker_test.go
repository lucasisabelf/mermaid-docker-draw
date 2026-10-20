package docker

import (
	"encoding/json"
	"testing"
)

func TestBodyNetworks(t *testing.T) {
	s := ContainerSpec{
		Image: "postgres:18-alpine",
		Networks: []NetworkAttach{
			{Name: "mdd-net-loja", Aliases: []string{"pg1"}},
			{Name: "mermaid-docker-draw"},
		},
	}
	b, _ := json.Marshal(s.body())
	var body struct {
		HostConfig       struct{ NetworkMode string }
		NetworkingConfig struct {
			EndpointsConfig map[string]struct{ Aliases []string }
		}
	}
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	// A primeira rede vai no create, com o alias; as outras entram com connect antes do start.
	if body.HostConfig.NetworkMode != "mdd-net-loja" {
		t.Errorf("NetworkMode = %q", body.HostConfig.NetworkMode)
	}
	ep := body.NetworkingConfig.EndpointsConfig
	if len(ep) != 1 || len(ep["mdd-net-loja"].Aliases) != 1 || ep["mdd-net-loja"].Aliases[0] != "pg1" {
		t.Errorf("EndpointsConfig = %+v", ep)
	}
	if extra := s.extraNetworks(); len(extra) != 1 || extra[0].Name != "mermaid-docker-draw" {
		t.Errorf("extras = %+v", extra)
	}

	// network_mode do yaml (ex.: host) continua valendo sozinho.
	s = ContainerSpec{Image: "a", NetworkMode: "host"}
	b, _ = json.Marshal(s.body())
	body.NetworkingConfig.EndpointsConfig = nil
	_ = json.Unmarshal(b, &body)
	if body.HostConfig.NetworkMode != "host" || body.NetworkingConfig.EndpointsConfig != nil {
		t.Errorf("network_mode: %s", b)
	}
}

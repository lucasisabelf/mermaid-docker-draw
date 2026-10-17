package nodespec

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
)

// Paths diz como traduzir os caminhos de bind: quando o servidor roda dentro de um container,
// o daemon do Docker precisa do caminho no host, não do caminho visto pelo servidor.
type Paths struct {
	ProjectDir string
	HostDir    string
}

func (p Paths) host(path string) string {
	if p.HostDir == "" || p.ProjectDir == "" {
		return path
	}
	rel, err := filepath.Rel(p.ProjectDir, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(filepath.Join(p.HostDir, rel))
}

// Container converte o serviço compose no container a criar.
func (s *Spec) Container(paths Paths) docker.ContainerSpec {
	svc := s.Service
	c := docker.ContainerSpec{
		Image:       svc.Image,
		Cmd:         svc.Command,
		Entrypoint:  svc.Entrypoint,
		WorkingDir:  svc.WorkingDir,
		User:        svc.User,
		Labels:      map[string]string{},
		NetworkMode: svc.NetworkMode,
		Privileged:  svc.Privileged,
		CapAdd:      svc.CapAdd,
		Tmpfs:       svc.Tmpfs,
		Init:        svc.Init,
		ExtraHosts:  svc.ExtraHosts.AsList(":"),
	}
	for k, v := range svc.Environment {
		if v != nil {
			c.Env = append(c.Env, k+"="+*v)
		}
	}
	sort.Strings(c.Env)
	for k, v := range svc.Labels {
		c.Labels[k] = v
	}
	for _, v := range svc.Volumes {
		m := docker.Mount{Type: v.Type, Source: v.Source, Target: v.Target, ReadOnly: v.ReadOnly}
		if v.Type == types.VolumeTypeBind {
			m.Source = paths.host(v.Source)
		}
		c.Mounts = append(c.Mounts, m)
	}
	for _, p := range svc.Ports {
		c.Ports = append(c.Ports, docker.Port{Target: p.Target, Protocol: p.Protocol, HostIP: p.HostIP, Published: p.Published})
	}
	if svc.StopGracePeriod != nil {
		secs := int(time.Duration(*svc.StopGracePeriod).Seconds())
		c.StopTimeout = &secs
	}
	return c
}

// Package docker fala com o Docker Engine pela API HTTP (socket unix ou tcp, de DOCKER_HOST),
// só com o necessário para rodar um container por retângulo e acompanhar a saída dele.
package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Client struct {
	http *http.Client
	base string
}

// New usa DOCKER_HOST (unix:///... ou tcp://host:porta); sem ele, /var/run/docker.sock.
func New() (*Client, error) {
	host := os.Getenv("DOCKER_HOST")
	if host == "" {
		host = "unix:///var/run/docker.sock"
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("DOCKER_HOST inválido: %w", err)
	}
	switch u.Scheme {
	case "unix":
		sock := u.Path
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}
		return &Client{http: &http.Client{Transport: tr}, base: "http://docker"}, nil
	case "tcp", "http":
		return &Client{http: &http.Client{}, base: "http://" + u.Host}, nil
	}
	return nil, fmt.Errorf("DOCKER_HOST %q: só unix:// e tcp:// são suportados", host)
}

type apiError struct {
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		var e apiError
		b, _ := io.ReadAll(res.Body)
		if json.Unmarshal(b, &e) != nil || e.Message == "" {
			e.Message = strings.TrimSpace(string(b))
		}
		return nil, &StatusError{Code: res.StatusCode, Msg: e.Message}
	}
	return res, nil
}

type StatusError struct {
	Code int
	Msg  string
}

func (e *StatusError) Error() string { return fmt.Sprintf("docker (%d): %s", e.Code, e.Msg) }

func IsNotFound(err error) bool {
	var s *StatusError
	return errors.As(err, &s) && s.Code == http.StatusNotFound
}

func (c *Client) call(ctx context.Context, method, path string, body, out any) error {
	res, err := c.do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, res.Body)
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *Client) Ping(ctx context.Context) error {
	return c.call(ctx, http.MethodGet, "/_ping", nil, nil)
}

// ---- imagens e volumes -----------------------------------------------------

// EnsureImage baixa a imagem se ela ainda não existe localmente; `progress` recebe o andamento.
func (c *Client) EnsureImage(ctx context.Context, image string, progress func(string)) error {
	if image == "" {
		return errors.New(`o serviço não tem "image"`)
	}
	err := c.call(ctx, http.MethodGet, "/images/"+url.PathEscape(image)+"/json", nil, nil)
	if err == nil || !IsNotFound(err) {
		return err
	}
	progress("baixando a imagem " + image + "…")
	q := url.Values{"fromImage": {image}}
	if !strings.Contains(image, "@") && !strings.Contains(image[strings.LastIndex(image, "/")+1:], ":") {
		q.Set("tag", "latest")
	}
	res, err := c.do(ctx, http.MethodPost, "/images/create?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	dec := json.NewDecoder(res.Body)
	last := ""
	for {
		var msg struct {
			Status, Error, ID string
		}
		if err := dec.Decode(&msg); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if msg.Error != "" {
			return errors.New(msg.Error)
		}
		// Só as mudanças de etapa; as barras de progresso repetiriam a mesma linha centenas de vezes.
		if line := strings.TrimSpace(msg.ID + " " + msg.Status); msg.Status != "Downloading" && msg.Status != "Extracting" && line != last {
			last = line
			progress(line)
		}
	}
}

func (c *Client) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	return c.call(ctx, http.MethodPost, "/volumes/create", map[string]any{"Name": name, "Labels": labels}, nil)
}

func (c *Client) RemoveVolume(ctx context.Context, name string) error {
	err := c.call(ctx, http.MethodDelete, "/volumes/"+url.PathEscape(name)+"?force=1", nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

// ---- containers ------------------------------------------------------------

type Mount struct {
	Type     string `json:"Type"`
	Source   string `json:"Source,omitempty"`
	Target   string `json:"Target"`
	ReadOnly bool   `json:"ReadOnly,omitempty"`
}

type Port struct {
	Target    uint32
	Protocol  string
	HostIP    string
	Published string
}

// ContainerSpec é o que um retângulo vira na hora de rodar.
type ContainerSpec struct {
	Name        string
	Image       string
	Cmd         []string
	Entrypoint  []string
	Env         []string
	WorkingDir  string
	User        string
	Labels      map[string]string
	Mounts      []Mount
	Ports       []Port
	ExtraHosts  []string
	NetworkMode string
	Privileged  bool
	CapAdd      []string
	Tmpfs       []string
	Init        *bool
	StopTimeout *int
}

func (s ContainerSpec) body() map[string]any {
	exposed := map[string]struct{}{}
	bindings := map[string][]map[string]string{}
	for _, p := range s.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		key := fmt.Sprintf("%d/%s", p.Target, proto)
		exposed[key] = struct{}{}
		if p.Published != "" || p.HostIP != "" {
			bindings[key] = append(bindings[key], map[string]string{"HostIp": p.HostIP, "HostPort": p.Published})
		}
	}
	tmpfs := map[string]string{}
	for _, t := range s.Tmpfs {
		path, opts, _ := strings.Cut(t, ":")
		tmpfs[path] = opts
	}
	host := map[string]any{
		"Mounts":       s.Mounts,
		"PortBindings": bindings,
		"ExtraHosts":   s.ExtraHosts,
		"Privileged":   s.Privileged,
		"CapAdd":       s.CapAdd,
		"Tmpfs":        tmpfs,
		"Init":         s.Init,
	}
	if s.NetworkMode != "" {
		host["NetworkMode"] = s.NetworkMode
	}
	b := map[string]any{
		"Image":        s.Image,
		"Env":          s.Env,
		"WorkingDir":   s.WorkingDir,
		"User":         s.User,
		"Labels":       s.Labels,
		"ExposedPorts": exposed,
		"HostConfig":   host,
	}
	// Sem command/entrypoint, vale o da imagem.
	if len(s.Cmd) > 0 {
		b["Cmd"] = s.Cmd
	}
	if len(s.Entrypoint) > 0 {
		b["Entrypoint"] = s.Entrypoint
	}
	if s.StopTimeout != nil {
		b["StopTimeout"] = *s.StopTimeout
	}
	return b
}

// Line é uma linha da saída de um container.
type Line struct {
	Stream string    `json:"s"`
	Text   string    `json:"l"`
	Time   time.Time `json:"t"`
}

// Container é um container já iniciado.
type Container struct {
	c     *Client
	ID    string
	lines chan Line
	done  chan struct{}
	once  sync.Once
}

// Run cria e inicia o container e começa a ler a saída dele.
func (c *Client) Run(ctx context.Context, spec ContainerSpec) (*Container, error) {
	var created struct{ Id string }
	q := ""
	if spec.Name != "" {
		q = "?name=" + url.QueryEscape(spec.Name)
	}
	if err := c.call(ctx, http.MethodPost, "/containers/create"+q, spec.body(), &created); err != nil {
		return nil, err
	}
	ct := &Container{c: c, ID: created.Id, lines: make(chan Line, 256), done: make(chan struct{})}
	if err := c.call(ctx, http.MethodPost, "/containers/"+ct.ID+"/start", nil, nil); err != nil {
		_ = ct.Remove()
		return nil, err
	}
	// Os logs são lidos desde o início: nada se perde entre o start e o attach.
	res, err := c.do(context.Background(), http.MethodGet, "/containers/"+ct.ID+"/logs?follow=1&stdout=1&stderr=1", nil)
	if err != nil {
		_ = ct.Stop(0)
		_ = ct.Remove()
		return nil, err
	}
	go ct.read(res.Body)
	return ct, nil
}

// Lines entrega a saída, linha a linha; fecha quando o container termina.
func (ct *Container) Lines() <-chan Line { return ct.lines }

const maxLine = 64 * 1024

// read separa stdout e stderr do fluxo multiplexado do Docker (cabeçalho de 8 bytes por bloco).
func (ct *Container) read(body io.ReadCloser) {
	defer body.Close()
	defer close(ct.lines)
	partial := map[string]*bytes.Buffer{"stdout": {}, "stderr": {}}
	emit := func(stream string, b []byte) {
		ct.lines <- Line{Stream: stream, Text: strings.TrimRight(string(b), "\r"), Time: time.Now()}
	}
	r := bufio.NewReader(body)
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			break
		}
		stream := "stdout"
		if header[0] == 2 {
			stream = "stderr"
		}
		chunk := make([]byte, binary.BigEndian.Uint32(header[4:]))
		if _, err := io.ReadFull(r, chunk); err != nil {
			break
		}
		buf := partial[stream]
		buf.Write(chunk)
		for {
			i := bytes.IndexByte(buf.Bytes(), '\n')
			if i < 0 {
				if buf.Len() > maxLine {
					emit(stream, buf.Next(maxLine))
					continue
				}
				break
			}
			emit(stream, buf.Next(i + 1)[:i])
		}
	}
	for _, stream := range []string{"stdout", "stderr"} {
		if partial[stream].Len() > 0 {
			emit(stream, partial[stream].Bytes())
		}
	}
}

// Wait espera o container terminar e devolve o código de saída.
func (ct *Container) Wait() (int, error) {
	var out struct {
		StatusCode int
		Error      *struct{ Message string }
	}
	if err := ct.c.call(context.Background(), http.MethodPost, "/containers/"+ct.ID+"/wait", nil, &out); err != nil {
		return -1, err
	}
	if out.Error != nil && out.Error.Message != "" {
		return out.StatusCode, errors.New(out.Error.Message)
	}
	return out.StatusCode, nil
}

// Stop pede para o container parar (SIGTERM) e mata depois de `timeout`.
func (ct *Container) Stop(timeout time.Duration) error {
	err := ct.c.call(context.Background(), http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", ct.ID, int(timeout.Seconds())), nil, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (ct *Container) Remove() error {
	var err error
	ct.once.Do(func() {
		err = ct.c.call(context.Background(), http.MethodDelete, "/containers/"+ct.ID+"?force=1&v=1", nil, nil)
		if IsNotFound(err) {
			err = nil
		}
	})
	return err
}

// RemoveLabeled remove containers e volumes que sobraram de execuções anteriores (ex.: o servidor caiu).
func (c *Client) RemoveLabeled(ctx context.Context, label string) error {
	filters, _ := json.Marshal(map[string][]string{"label": {label}})
	var list []struct{ Id string }
	if err := c.call(ctx, http.MethodGet, "/containers/json?all=1&filters="+url.QueryEscape(string(filters)), nil, &list); err != nil {
		return err
	}
	for _, ct := range list {
		_ = c.call(ctx, http.MethodDelete, "/containers/"+ct.Id+"?force=1&v=1", nil, nil)
	}
	var vols struct{ Volumes []struct{ Name string } }
	if err := c.call(ctx, http.MethodGet, "/volumes?filters="+url.QueryEscape(string(filters)), nil, &vols); err != nil {
		return err
	}
	for _, v := range vols.Volumes {
		_ = c.RemoveVolume(ctx, v.Name)
	}
	return nil
}

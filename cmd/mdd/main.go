// Comando mdd: servidor do mermaid-docker-draw. Serve o editor (web/dist), a API em /api e roda
// cada retângulo do fluxo como um container Docker.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lucasisabelf/mermaid-docker-draw/internal/docker"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/engine"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/templates"
	"github.com/lucasisabelf/mermaid-docker-draw/internal/workspace"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	addr := flag.String("addr", env("MDD_ADDR", ":8080"), "endereço HTTP")
	wsDir := flag.String("workspace", env("MDD_WORKSPACE", "workspace"), "pasta do workspace: um mermaid (projeto) por subpasta")
	static := flag.String("static", env("MDD_STATIC", "web/dist"), "editor compilado (npm --prefix web run build)")
	callback := flag.String("callback-url", env("MDD_CALLBACK_URL", ""), "endereço do servidor visto de dentro dos containers (padrão: http://host.docker.internal:<porta>)")
	network := flag.String("network", env("MDD_NETWORK", ""), "rede Docker dos containers (vazio: a padrão)")
	hostDir := flag.String("host-dir", env("MDD_HOST_DIR", ""), "caminho de --workspace no host, quando o servidor roda dentro de um container")
	flag.Parse()

	root, err := filepath.Abs(*wsDir)
	if err != nil {
		log.Fatal(err)
	}
	staticDir, err := filepath.Abs(*static)
	if err != nil {
		log.Fatal(err)
	}

	dc, err := docker.New()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	dockerOK := dc.Ping(ctx) == nil
	cancel()
	if !dockerOK {
		log.Print("aviso: Docker indisponível; o editor funciona, mas rodar os fluxos vai falhar")
	}

	if *callback == "" {
		_, port, _ := net.SplitHostPort(*addr)
		*callback = "http://" + net.JoinHostPort(callbackHost(), port)
	}
	log.Printf("containers chamam o servidor em %s (mude com --callback-url)", *callback)

	cfg := workspace.Config{
		Dir:         root,
		HostDir:     *hostDir,
		Runtime:     engine.DockerRuntime{Client: dc},
		CallbackURL: *callback,
		Network:     *network,
		PortFree:    templates.Listenable,
	}
	if dockerOK {
		cfg.Docker = dc
	}
	if _, port, err := net.SplitHostPort(*addr); err == nil {
		cfg.ServerPort, _ = strconv.Atoi(port)
	}
	ws, err := workspace.New(cfg)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", ws)
	mux.Handle("/", spa(staticDir))
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		log.Print("parando: encerrando as execuções ativas…")
		ws.Shutdown(20 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("mermaid-docker-draw em http://localhost%s (workspace: %s)", strings.TrimPrefix(*addr, "0.0.0.0"), root)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

// callbackHost é como os containers alcançam este servidor. No WSL com Docker Desktop,
// host.docker.internal aponta para o Windows, não para a distro: usa o IP da própria distro.
func callbackHost() string {
	version, _ := os.ReadFile("/proc/version")
	if os.Getenv("WSL_DISTRO_NAME") == "" && !strings.Contains(strings.ToLower(string(version)), "microsoft") {
		return "host.docker.internal"
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "br-") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ip, ok := a.(*net.IPNet); ok && ip.IP.To4() != nil {
				return ip.IP.String()
			}
		}
	}
	return "host.docker.internal"
}

// spa serve o editor compilado; caminhos desconhecidos caem no index.html.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			http.Error(w, "Editor não compilado: rode `npm --prefix web install && npm --prefix web run build`,\n"+
				"ou use `npm --prefix web run dev` (Vite com proxy para esta API).", http.StatusNotFound)
			return
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.Clean("/"+r.URL.Path))); err != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

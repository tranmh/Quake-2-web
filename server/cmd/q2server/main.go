// Command q2server runs the Go Quake 2 server: a host of game instances
// reachable over WebSocket (/ws/v1/games/{id}, one binary message == one
// netchan datagram, ADR-0001) and optionally raw UDP for the original C client
// and tools.
//
//	q2server -listen :8080 -basedir assets/demo -map demo1 -udp :27910
//
// With -map a default instance (id -id, default "default") is started for
// development; UDP datagrams go to that instance.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/game"
	"quake2web/server/internal/host"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
)

type cvarFlags [][2]string

func (c *cvarFlags) String() string { return fmt.Sprint(*c) }
func (c *cvarFlags) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("want name=value, got %q", v)
	}
	*c = append(*c, [2]string{k, val})
	return nil
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address for the WebSocket endpoint")
	udp := flag.String("udp", "", "optional UDP listen address (e.g. :27910) routed to the default instance")
	basedir := flag.String("basedir", "assets/demo", "base directory containing baseq2/pak*.pak")
	gamedir := flag.String("game", "baseq2", "game directory under basedir")
	mapName := flag.String("map", "", "start a default instance on this map (e.g. demo1)")
	id := flag.String("id", "default", "id of the default instance")
	dedicated := flag.Bool("dedicated", true, "dedicated server semantics (forces deathmatch unless coop)")
	tickets := flag.Bool("tickets", false, "require join tickets on the WebSocket endpoint")
	module := flag.String("gamemodule", "stub", "game module: \"stub\" (internal/sv/stubgame) or \"game\" (the internal/game port)")
	demoDir := flag.String("demodir", "", "directory for serverrecord .dm2 files (default <basedir>/<game>/demos)")
	origins := flag.String("origins", "", "comma separated extra WebSocket Origin host patterns (e.g. localhost:3000)")
	var cvars cvarFlags
	flag.Var(&cvars, "set", "cvar name=value applied at start (repeatable)")
	flag.Parse()

	fs := &pak.FS{}
	if err := fs.AddGameDirectory(filepath.Join(*basedir, "baseq2")); err != nil {
		log.Fatalf("basedir: %v", err)
	}
	if *gamedir != "baseq2" {
		if err := fs.AddGameDirectory(filepath.Join(*basedir, *gamedir)); err != nil {
			log.Fatalf("game dir: %v", err)
		}
	}
	defer fs.Close()

	dd := *demoDir
	if dd == "" {
		dd = filepath.Join(*basedir, *gamedir, "demos")
	}

	h := host.New()
	h.RequireTickets = *tickets
	h.Logf = log.Printf
	if *origins != "" {
		h.OriginPatterns = strings.Split(*origins, ",")
	}
	maps := sv.NewMapCache()
	saves := sv.NewMemSaveStore()

	rng := crand.New(1)
	var factory sv.GameFactory
	switch *module {
	case "stub":
		factory = stubgame.New()
	case "game":
		factory = func(gi game.Import) game.Export { return game.New(gi, rng) }
	default:
		log.Fatalf("unknown -gamemodule %q", *module)
	}

	if *mapName != "" {
		_, err := h.Create(host.InstanceConfig{
			ID: *id,
			Server: sv.Config{
				FS:        fs,
				Maps:      maps,
				Saves:     saves,
				Rand:      rng,
				Game:      factory,
				Dedicated: *dedicated,
				Cvars:     cvars,
				Printf: func(f string, a ...any) {
					fmt.Fprintf(os.Stdout, "[%s] "+f, append([]any{*id}, a...)...)
				},
				DemoCreate: func(name string) (io.WriteCloser, error) {
					if err := os.MkdirAll(dd, 0o755); err != nil {
						return nil, err
					}
					return os.Create(filepath.Join(dd, filepath.Base(name)+".dm2"))
				},
			},
			Commands: []string{"map " + *mapName},
		})
		if err != nil {
			log.Fatalf("start instance: %v", err)
		}
		log.Printf("instance %q running %s", *id, *mapName)
	}

	if *udp != "" {
		l, err := qnet.ListenUDP(*udp)
		if err != nil {
			log.Fatalf("udp: %v", err)
		}
		defer l.Close()
		log.Printf("UDP on %s -> instance %q", l.LocalAddr(), *id)
		go func() { _ = h.ServeUDP(l, *id) }()
	}

	srv := &http.Server{Addr: *listen, Handler: h.Handler()}
	go func() {
		log.Printf("WebSocket endpoint on %s/ws/v1/games/{id}", *listen)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	log.Printf("shutting down")
	_ = srv.Shutdown(context.Background())
	h.Shutdown()
}

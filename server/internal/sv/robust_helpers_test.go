package sv

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/game"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/sv/stubgame"
)

// Synchronous helpers for robustness tests and fuzz targets: a server on the
// synthetic floor map driven directly through HandlePacket/Frame (no
// goroutines, no wall clock) and a hand-rolled client.

var synthBSP = bsp.Encode(bsp.SyntheticFloorMap())

type synthFS struct{}

func (synthFS) ReadFile(name string) ([]byte, error) {
	if name == "maps/synth.bsp" {
		return synthBSP, nil
	}
	return nil, errors.New("not found")
}

// recorder is a Sender that keeps every datagram.
type recorder struct{ pkts [][]byte }

func (r *recorder) SendPacket(_ qnet.Addr, d []byte) error {
	r.pkts = append(r.pkts, append([]byte(nil), d...))
	return nil
}

func (r *recorder) take() [][]byte {
	p := r.pkts
	r.pkts = nil
	return p
}

// newSynthServer starts a deathmatch server on maps/synth.bsp. realGame
// selects the internal/game port instead of the stub game.
func newSynthServer(t testing.TB, realGame bool, extra ...[2]string) *Server {
	t.Helper()
	rng := crand.New(1)
	var gf GameFactory = stubgame.New()
	if realGame {
		gf = func(gi game.Import) game.Export { return game.New(gi, rng) }
	}
	return newSynthServerWith(t, gf, rng, extra...)
}

func newSynthServerWith(t testing.TB, gf GameFactory, rng *crand.Rand, extra ...[2]string) *Server {
	t.Helper()
	clock := 0
	s := New(Config{
		FS: synthFS{}, Game: gf, Rand: rng, Dedicated: true,
		Clock: func() int { return clock },
		Cvars: append([][2]string{{"deathmatch", "1"}, {"maxclients", "4"}}, extra...),
	})
	if err := s.ExecuteText("map synth\n"); err != nil {
		t.Fatalf("map synth: %v", err)
	}
	if !s.InGame() {
		t.Fatalf("server not in game")
	}
	return s
}

// tclient is a minimal protocol-34 client talking to a synchronous server.
type tclient struct {
	t    testing.TB
	s    *Server
	addr qnet.Addr
	in   *recorder // server -> client datagrams
	ch   qnet.Netchan
	out  *recorder // client -> server datagrams (netchan output)
	// allowInternal lets HandlePacket report an InternalError (a recovered
	// runtime panic) without failing the test.
	allowInternal bool
}

// failOnInternal fails the test when err is a recovered runtime panic (so
// fuzz targets still see them as crashes).
func failOnInternal(t testing.TB, err error) {
	var ie *InternalError
	if errors.As(err, &ie) {
		t.Fatalf("runtime panic: %v\n%s", ie.Value, ie.Stack)
	}
}

func newTClient(t testing.TB, s *Server, base string, port int) *tclient {
	return &tclient{t: t, s: s, addr: qnet.Addr{Base: base, Port: port}, in: &recorder{}, out: &recorder{}}
}

// raw delivers one datagram to the server as coming from this client.
func (c *tclient) raw(d []byte) error {
	err := c.s.HandlePacket(0, qnet.Packet{From: c.addr, Via: c.in, Data: d})
	if !c.allowInternal {
		failOnInternal(c.t, err)
	}
	return err
}

func (c *tclient) oob(text string) error {
	return c.raw(append([]byte{0xff, 0xff, 0xff, 0xff}, text...))
}

// oobReplies returns the text of every OOB reply received so far.
func (c *tclient) oobReplies() []string {
	var r []string
	for _, p := range c.in.take() {
		if len(p) >= 4 && binary.LittleEndian.Uint32(p) == 0xffffffff {
			r = append(r, string(p[4:]))
		} else {
			c.process(p)
		}
	}
	return r
}

func (c *tclient) process(p []byte) {
	m := msg.NewReader(p)
	c.ch.Process(m, 0)
}

// connect performs getchallenge + connect; the client ends up cs_connected.
func (c *tclient) connect(qport int, userinfo string) {
	c.t.Helper()
	if err := c.oob("getchallenge\n"); err != nil {
		c.t.Fatal(err)
	}
	var ch int
	for _, r := range c.oobReplies() {
		if strings.HasPrefix(r, "challenge ") {
			ch, _ = strconv.Atoi(strings.TrimPrefix(r, "challenge "))
		}
	}
	if err := c.oob(fmt.Sprintf("connect %d %d %d \"%s\"\n", q2const.PROTOCOL_VERSION, qport, ch, userinfo)); err != nil {
		c.t.Fatal(err)
	}
	ok := false
	for _, r := range c.oobReplies() {
		if r == "client_connect" {
			ok = true
		}
	}
	if !ok {
		c.t.Fatalf("connect refused")
	}
	c.ch.Setup(q2const.NS_CLIENT, c.addr, c.out, qport, 0)
}

// send transmits payload (unreliable part) in a sequenced packet.
func (c *tclient) send(payload []byte) error {
	c.ch.Transmit(payload, 0)
	var err error
	for _, p := range c.out.take() {
		if e := c.raw(p); e != nil && err == nil {
			err = e
		}
	}
	c.oobReplies() // process the server's answers (acks reliable data)
	return err
}

func stringCmd(s string) []byte {
	b := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	b.MSG_WriteByte(q2const.Clc_stringcmd)
	b.MSG_WriteString(s)
	return b.Bytes()
}

// spawn walks the client through new/begin so it is cs_spawned.
func (c *tclient) spawn() {
	c.t.Helper()
	sc := c.s.SVS.SpawnCount
	for _, cmd := range []string{"new", fmt.Sprintf("configstrings %d 0", sc),
		fmt.Sprintf("baselines %d 0", sc), fmt.Sprintf("begin %d", sc)} {
		if err := c.send(stringCmd(cmd)); err != nil {
			c.t.Fatalf("%s: %v", cmd, err)
		}
	}
	cl := c.slot()
	if cl == nil || cl.State != cs_spawned {
		c.t.Fatalf("client not spawned")
	}
}

func (c *tclient) slot() *Client {
	for i := range c.s.SVS.Clients {
		cl := &c.s.SVS.Clients[i]
		if cl.State != cs_free && cl.Netchan.RemoteAddress == c.addr {
			return cl
		}
	}
	return nil
}

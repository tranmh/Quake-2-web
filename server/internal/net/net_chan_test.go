package net

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
)

// captureSender records the last datagram sent.
type captureSender struct{ last []byte }

func (c *captureSender) SendPacket(_ Addr, data []byte) error {
	c.last = append([]byte(nil), data...)
	return nil
}

// TestNetchanMatchesC replays testdata/netchan_script.txt and compares every
// sent datagram, Process result and channel state with the output of the C
// driver built from qcommon/net_chan.c (testdata/netchan_expected.txt).
func TestNetchanMatchesC(t *testing.T) {
	script, err := os.ReadFile("testdata/netchan_script.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/netchan_expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	got := runNetchanScript(t, string(script))

	wl := filterPrints(strings.Split(strings.TrimRight(string(want), "\n"), "\n"))
	gl := filterPrints(strings.Split(strings.TrimRight(got, "\n"), "\n"))
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			ctx := ""
			for j := i; j >= 0 && j < len(wl); j-- {
				if strings.HasPrefix(wl[j], "> ") {
					ctx = wl[j]
					break
				}
			}
			t.Fatalf("line %d (after %q):\n C: %s\nGo: %s", i+1, ctx, w, g)
		}
	}
}

// filterPrints drops the "SZ_GetSpace: overflow" print, which the Go msg
// package does not emit (it has no console).
func filterPrints(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l == "print SZ_GetSpace: overflow" {
			continue
		}
		out = append(out, l)
	}
	return out
}

func unhexArg(t *testing.T, s string) []byte {
	if s == "-" || s == "" {
		return nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func runNetchanScript(t *testing.T, script string) string {
	var out bytes.Buffer
	var ch [2]Netchan
	var snd [2]captureSender
	curtime := 0
	adr := Addr{Base: "adr", Port: 0}
	for i := range ch {
		ch[i].Printf = func(format string, args ...any) {
			s := fmt.Sprintf(format, args...)
			s = strings.ReplaceAll(s, adr.String(), "adr")
			out.WriteString("print " + s)
		}
		ch[i].Message.SZ_Init(make([]byte, 0)) // mimic zeroed static state
	}
	idx := func(n string) int {
		if n == "s" {
			return 1
		}
		return 0
	}
	sc := bufio.NewScanner(strings.NewReader(script))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		op := f[0]
		a, b := "", ""
		if len(f) > 1 {
			a = f[1]
		}
		if len(f) > 2 {
			b = f[2]
		}
		fmt.Fprintf(&out, "> %s\n", line)
		i := idx(a)
		switch op {
		case "time":
			curtime, _ = strconv.Atoi(a)
			continue
		case "setup":
			q, _ := strconv.Atoi(b)
			sock := q2const.NS_CLIENT
			if i == 1 {
				sock = q2const.NS_SERVER
			}
			ch[i].Setup(sock, adr, &snd[i], q, curtime)
		case "rel":
			ch[i].Message.SZ_Write(unhexArg(t, b))
		case "relfill":
			n, _ := strconv.Atoi(b)
			ch[i].Message.SZ_Write(bytes.Repeat([]byte{0xab}, n))
		case "tx", "txfill":
			var data []byte
			if op == "tx" {
				data = unhexArg(t, b)
			} else {
				n, _ := strconv.Atoi(b)
				data = bytes.Repeat([]byte{0xcd}, n)
			}
			if sent := ch[i].Transmit(data, curtime); sent != nil {
				fmt.Fprintf(&out, "sent %x\n", sent)
			}
		case "rx", "rxlast":
			var data []byte
			if op == "rxlast" {
				data = append([]byte(nil), snd[idx(b)].last...)
			} else {
				data = unhexArg(t, b)
			}
			buf := make([]byte, q2const.MAX_MSGLEN)
			copy(buf, data)
			m := msg.NewReader(buf)
			m.CurSize = len(data)
			ok := ch[i].Process(m, curtime)
			fmt.Fprintf(&out, "rx %d %d\n", b2i(ok), m.ReadCount)
		default:
			t.Fatalf("bad op %q", op)
		}
		for j := range ch {
			c := &ch[j]
			name := 'c'
			if j == 1 {
				name = 's'
			}
			fmt.Fprintf(&out, "state %c in=%d inack=%d inrack=%d inrel=%d out=%d rel=%d lastrel=%d rellen=%d dropped=%d fatal=%d msg=%d ovf=%d lr=%d ls=%d\n",
				name, c.IncomingSequence, c.IncomingAcknowledged, c.IncomingReliableAcknowledged, c.IncomingReliableSequence,
				c.OutgoingSequence, c.ReliableSequence, c.LastReliableSequence, c.ReliableLength, c.Dropped,
				b2i(c.FatalError), c.Message.CurSize, b2i(c.Message.Overflowed), c.LastReceived, c.LastSent)
		}
	}
	return out.String()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestNetchanHeader checks the 10-byte client header layout directly.
func TestNetchanHeader(t *testing.T) {
	var s captureSender
	var c Netchan
	c.Setup(q2const.NS_CLIENT, Addr{Base: "x"}, &s, 0xbeef, 0)
	c.IncomingSequence = 0x12345678
	c.IncomingReliableSequence = 1
	c.Message.SZ_Write([]byte{9})
	c.Transmit([]byte{7}, 0)
	want := []byte{1, 0, 0, 0x80, 0x78, 0x56, 0x34, 0x92, 0xef, 0xbe, 9, 7}
	if !bytes.Equal(s.last, want) {
		t.Fatalf("got %x want %x", s.last, want)
	}
	if len(want)-2 != q2const.PACKET_HEADER {
		t.Fatal("header size")
	}
}

// TestNetchanReliableResend drives two channels through a dropped reliable.
func TestNetchanReliableResend(t *testing.T) {
	var cs, ss captureSender
	var c, s Netchan
	c.Setup(q2const.NS_CLIENT, Addr{Base: "a"}, &cs, 1, 0)
	s.Setup(q2const.NS_SERVER, Addr{Base: "a"}, &ss, 1, 0)
	deliver := func(dst *Netchan, pkt []byte) bool {
		return dst.Process(msg.NewReader(append([]byte(nil), pkt...)), 0)
	}
	s.Message.SZ_Write([]byte("hello"))
	s.Transmit(nil, 0) // dropped
	if s.ReliableLength != 5 {
		t.Fatal("reliable not staged")
	}
	// client keeps talking; server must resend once it sees an ack past it
	for i := 0; i < 3; i++ {
		c.Transmit(nil, 0)
		deliver(&s, cs.last)
		pkt := s.Transmit(nil, 0)
		if deliver(&c, pkt) && bytes.Contains(pkt, []byte("hello")) {
			c.Transmit(nil, 0)
			deliver(&s, cs.last)
			if s.ReliableLength != 0 {
				t.Fatal("reliable not acknowledged")
			}
			return
		}
	}
	t.Fatal("reliable never resent")
}

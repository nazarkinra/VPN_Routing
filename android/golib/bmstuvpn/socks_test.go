package bmstuvpn

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func socksConnect(t *testing.T, p *Proxy, user, pass string, target *net.TCPAddr) (net.Conn, byte) {
	t.Helper()
	c, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(p.Port())))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = c.Write([]byte{5, 1, 2})
	reply := make([]byte, 2)
	if _, err := io.ReadFull(c, reply); err != nil || reply[1] != 2 {
		t.Fatalf("method: %v %v", reply, err)
	}
	msg := []byte{1, byte(len(user))}
	msg = append(msg, user...)
	msg = append(msg, byte(len(pass)))
	msg = append(msg, pass...)
	_, _ = c.Write(msg)
	if _, err := io.ReadFull(c, reply); err != nil {
		t.Fatal(err)
	}
	if reply[1] != 0 {
		return c, 0xff
	}
	req := []byte{5, 1, 0, 1}
	req = append(req, target.IP.To4()...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, uint16(target.Port))
	req = append(req, port...)
	_, _ = c.Write(req)
	resp := make([]byte, 10)
	if _, err := io.ReadFull(c, resp); err != nil {
		t.Fatal(err)
	}
	return c, resp[1]
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestProxyDirectWhenTunnelDown(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }()
		}
	}()

	p, err := NewProxy()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.ViaTunnel() {
		t.Fatal("tunnel must be down")
	}

	c, code := socksConnect(t, p, p.User(), p.Password(), echo.Addr().(*net.TCPAddr))
	defer c.Close()
	if code != 0 {
		t.Fatalf("connect reply %d", code)
	}
	_, _ = c.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo: %q %v", buf, err)
	}

	bad, code := socksConnect(t, p, p.User(), "wrong", echo.Addr().(*net.TCPAddr))
	defer bad.Close()
	if code != 0xff {
		t.Fatal("wrong password accepted")
	}
}

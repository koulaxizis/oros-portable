package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// selfSigned makes a certificate like Proton Mail Bridge's: self-signed,
// for 127.0.0.1.
func selfSigned(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hex.EncodeToString(h[:])
}

// fakeMail is a tiny IMAP-like server: a greeting, "a STARTTLS" upgrades
// (starttls) or TLS from the start (tls); afterwards it echoes each line.
func fakeMail(t *testing.T, cert tls.Certificate, implicit bool) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				if implicit {
					c = tls.Server(c, cfg)
				}
				io.WriteString(c, "* OK ready\r\n")
				br := bufio.NewReader(c)
				for {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					if line == "a STARTTLS\r\n" && !implicit {
						io.WriteString(c, "a OK begin TLS\r\n")
						tc := tls.Server(c, cfg)
						if tc.Handshake() != nil {
							return
						}
						c, br = tc, bufio.NewReader(tc)
						continue
					}
					io.WriteString(c, "echo "+line)
				}
			}(c)
		}
	}()
	_, p, _ := net.SplitHostPort(ln.Addr().String())
	return p
}

type wsClient struct {
	c  net.Conn
	br *bufio.Reader
}

// dialSock opens /__oros/mail/sock on the launcher's handler as the orOS
// page would; it returns the HTTP status when the upgrade is refused.
func dialSock(t *testing.T, srv *httptest.Server, query string, cookie, origin string) (*wsClient, int) {
	t.Helper()
	c, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	req := "GET " + mailSockPath + "?" + query + " HTTP/1.1\r\nHost: localhost:47800\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: " + key + "\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	if cookie != "" {
		req += "Cookie: " + mailCookie + "=" + cookie + "\r\n"
	}
	req += "Sec-Fetch-Site: same-origin\r\n\r\n"
	io.WriteString(c, req)
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, resp.StatusCode
	}
	return &wsClient{c: c, br: br}, 101
}

func (w *wsClient) send(op byte, p []byte) {
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n <= 0xffff:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = append(h, 0x80|127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	mask := []byte{1, 2, 3, 4}
	h = append(h, mask...)
	m := make([]byte, len(p))
	for i := range p {
		m[i] = p[i] ^ mask[i%4]
	}
	w.c.Write(append(h, m...))
}

// next reads one frame (the server never fragments).
func (w *wsClient) next(t *testing.T) (byte, []byte) {
	t.Helper()
	w.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	var h [2]byte
	if _, err := io.ReadFull(w.br, h[:]); err != nil {
		t.Fatalf("read: %v", err)
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		io.ReadFull(w.br, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	} else if n == 127 {
		var b [8]byte
		io.ReadFull(w.br, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	p := make([]byte, n)
	io.ReadFull(w.br, p)
	return h[0] & 0x0f, p
}

func (w *wsClient) control(t *testing.T) map[string]string {
	t.Helper()
	op, p := w.next(t)
	if op != wsText {
		t.Fatalf("want a control message, got op %d %q", op, p)
	}
	var m map[string]string
	json.Unmarshal(p, &m)
	return m
}

// readText collects binary frames until want has arrived.
func (w *wsClient) readText(t *testing.T, want string) {
	t.Helper()
	got := ""
	for !strings.Contains(got, want) {
		op, p := w.next(t)
		if op != wsBinary {
			t.Fatalf("want %q, got op %d %q (so far %q)", want, op, p, got)
		}
		got += string(p)
	}
}

func mailServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(newHandler(t.TempDir()))
	t.Cleanup(srv.Close)
	return srv
}

const localOrigin = "http://localhost:47800"

func TestMailSockRefusesOthers(t *testing.T) {
	srv := mailServer(t)
	q := "host=127.0.0.1&port=1143&mode=starttls"
	if _, st := dialSock(t, srv, q, "", localOrigin); st != 403 {
		t.Fatalf("no cookie: %d", st)
	}
	if _, st := dialSock(t, srv, q, "wrong", localOrigin); st != 403 {
		t.Fatalf("wrong cookie: %d", st)
	}
	if _, st := dialSock(t, srv, q, mailAPI.token, "https://evil.example"); st != 403 {
		t.Fatalf("other origin: %d", st)
	}
	if _, st := dialSock(t, srv, q, mailAPI.token, ""); st != 403 {
		t.Fatalf("no origin: %d", st)
	}
	for _, bad := range []string{
		"host=imap.example.com&port=1143&mode=starttls", // a server by name: mail ports only
		"host=imap.example.com&port=993&mode=starttls",  // ... with their own mode
		"host=imap.example.com&port=80&mode=tls",
		"host=imap.example.com&port=993&mode=tls&pin=" + strings.Repeat("a", 64),
		"host=mail.lan&port=993&mode=tls",
		"host=nas.local&port=993&mode=tls",
		"host=10.0.0.1&port=993&mode=tls", // no IP literals but this computer's
		"host=8.8.8.8&port=993&mode=tls",
		"host=10.0.0.1&port=1143&mode=starttls",
		"host=127.0.0.2&port=1143&mode=starttls",
		"host=127.0.0.1&port=47800&mode=starttls", // never the launcher itself
		"host=127.0.0.1&port=01143&mode=starttls",
		"host=127.0.0.1&port=0&mode=starttls",
		"host=127.0.0.1&port=1143&mode=plain", // TLS always
		"host=127.0.0.1&port=1143&mode=starttls&pin=xyz",
	} {
		if _, st := dialSock(t, srv, bad, mailAPI.token, localOrigin); st != 400 {
			t.Fatalf("%s: %d, want 400", bad, st)
		}
	}
}

func TestMailSockSetsCookieOnPages(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "http://localhost:47800/", nil)
	newHandler(t.TempDir()).ServeHTTP(rec, req)
	if !strings.Contains(strings.Join(rec.Header().Values("Set-Cookie"), "\n"),
		mailCookie+"="+mailAPI.token+"; Path=/__oros/mail/; HttpOnly; SameSite=Strict") {
		t.Fatalf("cookie missing: %v", rec.Header().Values("Set-Cookie"))
	}
}

func TestMailSockStartTLSWithPin(t *testing.T) {
	cert, fp := selfSigned(t)
	port := fakeMail(t, cert, false)
	srv := mailServer(t)
	q := "host=127.0.0.1&port=" + port + "&mode=starttls"

	// First time: no pin, so the certificate is shown, never trusted.
	ws, st := dialSock(t, srv, q, mailAPI.token, localOrigin)
	if st != 101 {
		t.Fatalf("upgrade: %d", st)
	}
	if m := ws.control(t); m["t"] != "open" {
		t.Fatalf("want open, got %v", m)
	}
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("a STARTTLS\r\n"))
	ws.readText(t, "a OK begin TLS\r\n")
	ws.send(wsText, []byte(`{"t":"starttls"}`))
	if m := ws.control(t); m["t"] != "err" || m["code"] != "cert-new" || m["fp"] != fp {
		t.Fatalf("want cert-new with %s, got %v", fp, m)
	}

	// With the pin the person confirmed: TLS, and bytes go both ways.
	ws, _ = dialSock(t, srv, q+"&pin="+fp, mailAPI.token, localOrigin)
	ws.control(t)
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("a STARTTLS\r\n"))
	ws.readText(t, "a OK begin TLS\r\n")
	ws.send(wsText, []byte(`{"t":"starttls"}`))
	if m := ws.control(t); m["t"] != "tls" || m["fp"] != fp {
		t.Fatalf("want tls, got %v", m)
	}
	big := strings.Repeat("x", 70000) // over a 16-bit frame, past the plain caps
	ws.send(wsBinary, []byte("b LOGIN "+big+"\r\n"))
	ws.readText(t, "echo b LOGIN "+big+"\r\n")

	// Another certificate than the pinned one is refused.
	other := strings.Repeat("0", 64)
	ws, _ = dialSock(t, srv, q+"&pin="+other, mailAPI.token, localOrigin)
	ws.control(t)
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("a STARTTLS\r\n"))
	ws.readText(t, "a OK begin TLS\r\n")
	ws.send(wsText, []byte(`{"t":"starttls"}`))
	if m := ws.control(t); m["code"] != "cert" || m["fp"] != fp {
		t.Fatalf("want cert, got %v", m)
	}
}

func TestMailSockImplicitTLS(t *testing.T) {
	cert, fp := selfSigned(t)
	port := fakeMail(t, cert, true)
	srv := mailServer(t)
	ws, st := dialSock(t, srv, "host=localhost&port="+port+"&mode=tls&pin="+fp, mailAPI.token, localOrigin)
	if st != 101 {
		t.Fatalf("upgrade: %d", st)
	}
	if m := ws.control(t); m["t"] != "tls" || m["fp"] != fp {
		t.Fatalf("want tls, got %v", m)
	}
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("c NOOP\r\n"))
	ws.readText(t, "echo c NOOP\r\n")
}

// Without TLS the socket carries only a few hundred bytes, so it cannot
// be used for another protocol (an HTTP API on this computer).
func TestMailSockPlainCap(t *testing.T) {
	cert, _ := selfSigned(t)
	port := fakeMail(t, cert, false)
	srv := mailServer(t)
	ws, _ := dialSock(t, srv, "host=127.0.0.1&port="+port+"&mode=starttls", mailAPI.token, localOrigin)
	ws.control(t)
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte(strings.Repeat("y", mailPlainUp+1)))
	if m := ws.control(t); m["t"] != "err" || m["code"] != "plain" {
		t.Fatalf("want plain, got %v", m)
	}
}

func TestMailSockConnectError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	ln.Close() // nothing listens: like Bridge not running
	srv := mailServer(t)
	ws, st := dialSock(t, srv, "host=127.0.0.1&port="+port+"&mode=starttls", mailAPI.token, localOrigin)
	if st != 101 {
		t.Fatalf("upgrade: %d", st)
	}
	if m := ws.control(t); m["t"] != "err" || m["code"] != "connect" {
		t.Fatalf("want connect, got %v", m)
	}
}

// testCA makes a CA and a leaf for name, like a provider's real certificate.
func testCA(t *testing.T, name string) (*x509.CertPool, tls.Certificate) {
	t.Helper()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ca := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDer, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDer)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(8), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	return pool, tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// A provider's server by name: resolved to a public address (faked here),
// dialled by that address, the certificate checked against the roots.
func useRemote(t *testing.T, ip string, port string, roots *x509.CertPool) *[]string {
	t.Helper()
	dialled := &[]string{}
	oldDial, oldLookup, oldRoots := mailAPI.dial, mailAPI.lookup, mailAPI.roots
	t.Cleanup(func() { mailAPI.dial, mailAPI.lookup, mailAPI.roots = oldDial, oldLookup, oldRoots })
	mailAPI.lookup = func(ctx context.Context, host string) ([]net.IP, error) { return []net.IP{net.ParseIP(ip)}, nil }
	mailAPI.dial = func(addr string) (net.Conn, error) {
		*dialled = append(*dialled, addr)
		return net.Dial("tcp", "127.0.0.1:"+port)
	}
	mailAPI.roots = roots
	return dialled
}

func TestMailSockServerByName(t *testing.T) {
	roots, cert := testCA(t, "imap.example.com")
	port := fakeMail(t, cert, true)
	dialled := useRemote(t, "93.184.216.34", port, roots)
	srv := mailServer(t)
	ws, st := dialSock(t, srv, "host=imap.example.com&port=993&mode=tls", mailAPI.token, localOrigin)
	if st != 101 {
		t.Fatalf("upgrade: %d", st)
	}
	if m := ws.control(t); m["t"] != "tls" || m["fp"] != "" {
		t.Fatalf("want tls, got %v", m)
	}
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("d NOOP\r\n"))
	ws.readText(t, "echo d NOOP\r\n")
	if len(*dialled) != 1 || (*dialled)[0] != "93.184.216.34:993" {
		t.Fatalf("dialled %v", *dialled)
	}
}

func TestMailSockServerByNameSTARTTLS(t *testing.T) {
	roots, cert := testCA(t, "smtp.example.com")
	port := fakeMail(t, cert, false)
	useRemote(t, "93.184.216.34", port, roots)
	srv := mailServer(t)
	ws, _ := dialSock(t, srv, "host=smtp.example.com&port=587&mode=starttls", mailAPI.token, localOrigin)
	ws.control(t)
	ws.readText(t, "* OK ready\r\n")
	ws.send(wsBinary, []byte("a STARTTLS\r\n"))
	ws.readText(t, "a OK begin TLS\r\n")
	ws.send(wsText, []byte(`{"t":"starttls"}`))
	if m := ws.control(t); m["t"] != "tls" {
		t.Fatalf("want tls, got %v", m)
	}
}

func TestMailSockServerByNameRefusals(t *testing.T) {
	// a certificate for another name, or one the roots do not know
	roots, cert := testCA(t, "other.example.com")
	port := fakeMail(t, cert, true)
	useRemote(t, "93.184.216.34", port, roots)
	srv := mailServer(t)
	ws, _ := dialSock(t, srv, "host=imap.example.com&port=993&mode=tls", mailAPI.token, localOrigin)
	if m := ws.control(t); m["t"] != "err" || m["code"] != "tls" || m["fp"] != "" {
		t.Fatalf("wrong name: %v", m)
	}
	self, _ := selfSigned(t)
	port2 := fakeMail(t, self, true)
	useRemote(t, "93.184.216.34", port2, nil)
	ws, _ = dialSock(t, srv, "host=imap.example.com&port=993&mode=tls", mailAPI.token, localOrigin)
	if m := ws.control(t); m["code"] != "tls" {
		t.Fatalf("self-signed: %v", m)
	}
	// a name that resolves to this computer or the local network
	for _, ip := range []string{"127.0.0.1", "192.168.1.10", "10.1.2.3", "100.64.0.1", "169.254.1.1", "::1", "fd00::1", "0.0.0.0"} {
		dialled := useRemote(t, ip, port, roots)
		ws, _ = dialSock(t, srv, "host=imap.example.com&port=993&mode=tls", mailAPI.token, localOrigin)
		if m := ws.control(t); m["code"] != "connect" || len(*dialled) != 0 {
			t.Fatalf("%s: %v, dialled %v", ip, m, *dialled)
		}
	}
}

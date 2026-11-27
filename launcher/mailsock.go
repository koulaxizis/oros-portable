package main

// Mail socket (orOS MAIL-LOCAL, Chris 2026-11-27 «Προχώρα (επιλογή 1)»).
//
// orOS Mail reaches mail servers through the orOS relay on Cloudflare,
// which cannot reach a server that listens only on this computer, such as
// Proton Mail Bridge (IMAP 127.0.0.1:1143, SMTP 127.0.0.1:1025, STARTTLS
// with its own self-signed certificate). This WebSocket lets the orOS page
// open such a connection itself: the page runs the relay's own IMAP, SMTP
// and ManageSieve code (orOS mail/wire/) over it, and the launcher only
// carries bytes and does the TLS.
//
//   GET /__oros/mail/sock?host=127.0.0.1&port=1143&mode=starttls[&pin=<hex>]
//
// Who can call it: only the orOS page itself. Every request needs the Host
// check the whole server does (no DNS rebinding), the cookie oros_mail (a
// random key made at each start and set, HttpOnly and SameSite=Strict with
// Path=/__oros/mail/, on the orOS pages this server sends), an Origin equal
// to this server's own, and Sec-Fetch-Site same-origin when the browser
// sends it.
//
// What it can reach (Chris 2026-11-27 «Ναι, μέσω launcher»: Mail for every
// provider in the orOS on the computer, whose origin the relay does not
// serve for Mail):
//   - this computer (127.0.0.1, ::1, "localhost"), any port but the
//     launcher's own, trusted by pin (a local server such as Bridge);
//   - a mail server by name on the mail ports only (993 and 465 "tls",
//     143, 587 and 4190 "starttls"), the name checked like the relay's
//     validHost (no IP literal, no .local/.lan/... names), every address
//     it resolves to public (never this computer or the local network),
//     the certificate checked against the system's roots for that name.
// So it is no open proxy. Every connection must become TLS: mode "tls" handshakes at once,
// mode "starttls" lets at most mailPlainUp bytes go to the server and
// mailPlainDown bytes come back before the page asks for TLS (enough for a
// greeting, CAPABILITY/EHLO and STARTTLS, not for any other protocol).
//
// Certificates: a server by name must show a certificate the system
// trusts for that name ({"t":"err","code":"tls"} otherwise, no pin). A
// local mail server has its own self-signed certificate,
// so trust is by pin. The page passes the SHA-256 of the certificate it
// trusts (pin); without one, or when the server shows another
// certificate, the connection ends with {"t":"err","code":"cert-new"} or
// {"code":"cert"} and the certificate's SHA-256 (fp), which the page shows
// the person to confirm. Nothing is stored here.
//
// Messages: binary frames are the connection's bytes both ways. Text
// frames are JSON control messages: from the launcher {"t":"open"},
// {"t":"tls","fp":…}, {"t":"err","code":…[,"fp":…]}; from the page
// {"t":"starttls"}.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mailPrefix     = "/__oros/mail/"
	mailSockPath   = "/__oros/mail/sock"
	mailCookie     = "oros_mail"
	mailPlainUp    = 512              // bytes the page may send before TLS
	mailPlainDown  = 16 * 1024        // bytes the server may send before TLS
	mailMaxMessage = 32 * 1024 * 1024 // one WebSocket message (a 14 MB mail in base64 is far less)
	mailMaxOpen    = 8                // connections at once
	mailIdle       = 3 * time.Minute  // no bytes either way closes the connection
	mailDialWait   = 5 * time.Second
)

var mailAPI = newMailSock()

type mailSock struct {
	token  string
	open   atomic.Int32
	dial   func(addr string) (net.Conn, error)                      // tests may replace it
	lookup func(ctx context.Context, host string) ([]net.IP, error) // tests may replace it
	roots  *x509.CertPool                                           // nil: the system's
}

func newMailSock() *mailSock {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return &mailSock{
		token: base64.RawURLEncoding.EncodeToString(b),
		dial: func(addr string) (net.Conn, error) {
			return net.DialTimeout("tcp", addr, mailDialWait)
		},
		lookup: func(ctx context.Context, host string) ([]net.IP, error) {
			return net.DefaultResolver.LookupIP(ctx, "ip", host)
		},
	}
}

// setCookie gives the orOS page its key; called for every HTML page.
func (m *mailSock) setCookie(w http.ResponseWriter) {
	w.Header().Add("Set-Cookie", mailCookie+"="+m.token+"; Path="+mailPrefix+"; HttpOnly; SameSite=Strict")
}

func (m *mailSock) allowed(r *http.Request) bool {
	ck, err := r.Cookie(mailCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(m.token)) != 1 {
		return false
	}
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" && s != "same-origin" {
		return false
	}
	return r.Header.Get("Origin") == "http://"+r.Host
}

var mailPinRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// The mail ports a server by name may use, with their mode.
var mailPorts = map[string]string{"993": "tls", "465": "tls", "143": "starttls", "587": "starttls", "4190": "starttls"}

var (
	mailHostRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	mailLocalRe = regexp.MustCompile(`\.(local|localhost|internal|lan|home|arpa|test|invalid)$`)
)

type mailDest struct {
	host  string // the name for TLS
	port  string
	local bool   // this computer: trust by pin
	addr  string // local: the address to dial
}

// mailTarget checks the address the page asks for.
func mailTarget(host, portS, mode, pin string) (mailDest, error) {
	if mode != "tls" && mode != "starttls" {
		return mailDest{}, errors.New("mode")
	}
	if pin != "" && !mailPinRe.MatchString(pin) {
		return mailDest{}, errors.New("pin")
	}
	p, err := strconv.Atoi(portS)
	if err != nil || p < 1 || p > 65535 || strconv.Itoa(p) != portS {
		return mailDest{}, errors.New("port")
	}
	h := strings.ToLower(host)
	switch h {
	case "127.0.0.1", "localhost":
		if p == port {
			return mailDest{}, errors.New("port")
		}
		return mailDest{host: "127.0.0.1", port: portS, local: true, addr: net.JoinHostPort("127.0.0.1", portS)}, nil
	case "::1", "[::1]":
		if p == port {
			return mailDest{}, errors.New("port")
		}
		return mailDest{host: "::1", port: portS, local: true, addr: net.JoinHostPort("::1", portS)}, nil
	}
	h = strings.TrimSuffix(h, ".")
	if len(h) < 4 || len(h) > 253 || !mailHostRe.MatchString(h) || mailLocalRe.MatchString(h) || net.ParseIP(h) != nil ||
		strings.Trim(h, "0123456789.") == "" {
		return mailDest{}, errors.New("host")
	}
	if mailPorts[portS] != mode {
		return mailDest{}, errors.New("port")
	}
	if pin != "" {
		return mailDest{}, errors.New("pin")
	}
	return mailDest{host: h, port: portS}, nil
}

// publicIP: an address on the Internet, never this computer, the local
// network or a special range.
func publicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0, v4[0] == 100 && v4[1]&0xc0 == 64, v4[0] == 192 && v4[1] == 0 && v4[2] == 0,
			v4[0] == 198 && v4[1]&0xfe == 18, v4[0] >= 240:
			return false // this network, shared (CGNAT), IETF, benchmarking, reserved
		}
		return true
	}
	return !(ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8) // documentation
}

// connect dials the destination; a name is resolved here and dialled by
// address, so what was checked is what is reached.
func (m *mailSock) connect(d mailDest) (net.Conn, error) {
	if d.local {
		return m.dial(d.addr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), mailDialWait)
	defer cancel()
	ips, err := m.lookup(ctx, d.host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("lookup")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errors.New("private")
		}
	}
	var last error
	for _, ip := range ips {
		c, err := m.dial(net.JoinHostPort(ip.String(), d.port))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}

func (m *mailSock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != mailSockPath {
		http.NotFound(w, r)
		return
	}
	if !m.allowed(r) {
		http.Error(w, "only orOS may use this", http.StatusForbidden)
		return
	}
	q := r.URL.Query()
	pin := q.Get("pin")
	dest, err := mailTarget(q.Get("host"), q.Get("port"), q.Get("mode"), pin)
	if err != nil {
		http.Error(w, "invalid "+err.Error(), http.StatusBadRequest)
		return
	}
	if m.open.Add(1) > mailMaxOpen {
		m.open.Add(-1)
		http.Error(w, "busy", http.StatusTooManyRequests)
		return
	}
	defer m.open.Add(-1)
	ws, err := wsAccept(w, r)
	if err != nil {
		return // wsAccept answered
	}
	defer ws.close()
	conn, err := m.connect(dest)
	if err != nil {
		ws.control(map[string]string{"t": "err", "code": "connect"})
		return
	}
	s := &mailSession{ws: ws, raw: conn, conn: conn, pin: pin, host: dest.host, local: dest.local, roots: m.roots}
	defer func() { s.conn.Close() }()
	if q.Get("mode") == "tls" {
		if !s.handshake() {
			return
		}
	} else {
		ws.control(map[string]string{"t": "open"})
	}
	s.run()
}

// ---------- one connection ----------

type mailSession struct {
	ws        *wsConn
	raw       net.Conn // the TCP connection
	conn      net.Conn // raw, or the TLS connection over it
	pin, host string
	local     bool           // trust by pin; else the system's roots
	roots     *x509.CertPool // nil: the system's
	tls       atomic.Bool
	up, down  int // plain bytes so far (before TLS)
	upgrading atomic.Bool
	readerOut chan struct{}
	lastIO    atomic.Int64
}

func (s *mailSession) touch() { s.lastIO.Store(time.Now().UnixNano()) }

// handshake makes s.conn TLS with the pinned certificate; false after an
// error, which it has reported.
func (s *mailSession) handshake() bool {
	var fp string
	if !s.local {
		cfg := &tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12, RootCAs: s.roots}
		tc := tls.Client(s.raw, cfg)
		_ = s.raw.SetDeadline(time.Now().Add(15 * time.Second))
		err := tc.Handshake()
		_ = s.raw.SetDeadline(time.Time{})
		if err != nil {
			s.ws.control(map[string]string{"t": "err", "code": "tls"})
			return false
		}
		s.conn = tc
		s.tls.Store(true)
		s.ws.control(map[string]string{"t": "tls"})
		return true
	}
	cfg := &tls.Config{
		ServerName:         s.host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // trust is the pin, checked below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("no certificate")
			}
			fp = certPin(cs.PeerCertificates[0])
			if s.pin == "" || subtle.ConstantTimeCompare([]byte(fp), []byte(s.pin)) != 1 {
				return errors.New("certificate not trusted")
			}
			return nil
		},
	}
	tc := tls.Client(s.raw, cfg)
	_ = s.raw.SetDeadline(time.Now().Add(15 * time.Second))
	err := tc.Handshake()
	_ = s.raw.SetDeadline(time.Time{})
	if err != nil {
		msg := map[string]string{"t": "err", "code": "tls"}
		if fp != "" {
			msg["fp"] = fp
			if s.pin == "" {
				msg["code"] = "cert-new"
			} else {
				msg["code"] = "cert"
			}
		}
		s.ws.control(msg)
		return false
	}
	s.conn = tc
	s.tls.Store(true)
	s.ws.control(map[string]string{"t": "tls", "fp": fp})
	return true
}

func certPin(c *x509.Certificate) string {
	h := sha256.Sum256(c.Raw)
	return hex.EncodeToString(h[:])
}

func (s *mailSession) run() {
	s.touch()
	done := make(chan struct{})
	defer close(done)
	go func() { // idle watch
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if time.Since(time.Unix(0, s.lastIO.Load())) > mailIdle {
					s.ws.close()
					s.raw.Close()
					return
				}
			}
		}
	}()
	s.startReader()
	for {
		op, data, err := s.ws.read()
		if err != nil {
			return
		}
		s.touch()
		switch op {
		case wsBinary:
			if !s.tls.Load() {
				s.up += len(data)
				if s.up > mailPlainUp || s.upgrading.Load() {
					s.ws.control(map[string]string{"t": "err", "code": "plain"})
					return
				}
			}
			if _, err := s.conn.Write(data); err != nil {
				return
			}
		case wsText:
			var c struct {
				T string `json:"t"`
			}
			if json.Unmarshal(data, &c) != nil || c.T != "starttls" || s.tls.Load() || s.upgrading.Load() {
				s.ws.control(map[string]string{"t": "err", "code": "proto"})
				return
			}
			// Stop the plain reader without losing bytes: the server sends
			// nothing between its STARTTLS answer and the handshake.
			s.upgrading.Store(true)
			_ = s.raw.SetReadDeadline(time.Now())
			<-s.readerOut
			_ = s.raw.SetReadDeadline(time.Time{})
			if !s.handshake() {
				return
			}
			s.upgrading.Store(false)
			s.startReader()
		}
	}
}

// startReader carries the server's bytes to the page until the connection
// ends or an upgrade stops it.
func (s *mailSession) startReader() {
	out := make(chan struct{})
	s.readerOut = out
	conn := s.conn
	go func() {
		defer close(out)
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				s.touch()
				if !s.tls.Load() {
					s.down += n
					if s.down > mailPlainDown {
						s.ws.control(map[string]string{"t": "err", "code": "plain"})
						s.ws.close()
						return
					}
				}
				if s.ws.write(wsBinary, buf[:n]) != nil {
					return
				}
			}
			if err != nil {
				if s.upgrading.Load() {
					return // the read deadline stopped it; the handshake takes over
				}
				s.ws.closeNormal()
				return
			}
		}
	}()
}

// ---------- a small WebSocket server (RFC 6455) ----------

const (
	wsCont   = 0x0
	wsText   = 0x1
	wsBinary = 0x2
	wsClose  = 0x8
	wsPing   = 0x9
	wsPong   = 0xA
)

type wsConn struct {
	c      net.Conn
	br     *bufio.Reader
	wmu    sync.Mutex
	closed atomic.Bool
}

func wsAccept(w http.ResponseWriter, r *http.Request) (*wsConn, error) {
	if r.Method != http.MethodGet || !headerHas(r.Header, "Connection", "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "websocket only", http.StatusBadRequest)
		return nil, errors.New("not a websocket")
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if k, err := base64.StdEncoding.DecodeString(key); err != nil || len(k) != 16 {
		http.Error(w, "bad key", http.StatusBadRequest)
		return nil, errors.New("bad key")
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "unsupported", http.StatusInternalServerError)
		return nil, errors.New("no hijack")
	}
	c, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n"
	if _, err := c.Write([]byte(resp)); err != nil {
		c.Close()
		return nil, err
	}
	return &wsConn{c: c, br: brw.Reader}, nil
}

func headerHas(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, p := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(p), token) {
				return true
			}
		}
	}
	return false
}

// read returns the next text or binary message; pings are answered.
func (ws *wsConn) read() (byte, []byte, error) {
	var msg []byte
	var msgOp byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(ws.br, h[:]); err != nil {
			return 0, nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		if h[0]&0x70 != 0 || h[1]&0x80 == 0 { // no extensions; clients must mask
			return 0, nil, errors.New("bad frame")
		}
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(ws.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(ws.br, b[:]); err != nil {
				return 0, nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if op >= 0x8 && (n > 125 || !fin) {
			return 0, nil, errors.New("bad control frame")
		}
		if n > mailMaxMessage || uint64(len(msg))+n > mailMaxMessage {
			return 0, nil, errors.New("message too large")
		}
		var mask [4]byte
		if _, err := io.ReadFull(ws.br, mask[:]); err != nil {
			return 0, nil, err
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(ws.br, p); err != nil {
			return 0, nil, err
		}
		for i := range p {
			p[i] ^= mask[i%4]
		}
		switch op {
		case wsPing:
			_ = ws.write(wsPong, p)
			continue
		case wsPong:
			continue
		case wsClose:
			ws.closeNormal()
			return 0, nil, io.EOF
		case wsText, wsBinary:
			if msg != nil {
				return 0, nil, errors.New("unexpected new message")
			}
			msgOp, msg = op, p
		case wsCont:
			if msg == nil {
				return 0, nil, errors.New("unexpected continuation")
			}
			msg = append(msg, p...)
		default:
			return 0, nil, errors.New("unknown opcode")
		}
		if fin {
			return msgOp, msg, nil
		}
	}
}

func (ws *wsConn) write(op byte, p []byte) error {
	ws.wmu.Lock()
	defer ws.wmu.Unlock()
	if ws.closed.Load() && op != wsClose {
		return net.ErrClosed
	}
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, byte(n))
	case n <= 0xffff:
		h = append(h, 126, byte(n>>8), byte(n))
	default:
		h = append(h, 127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	_ = ws.c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if _, err := ws.c.Write(append(h, p...)); err != nil {
		return err
	}
	return nil
}

func (ws *wsConn) control(v map[string]string) {
	b, _ := json.Marshal(v)
	_ = ws.write(wsText, b)
}

// closeNormal sends a close frame (1000) once.
func (ws *wsConn) closeNormal() {
	if ws.closed.Swap(true) {
		return
	}
	_ = ws.write(wsClose, []byte{0x03, 0xe8})
}

func (ws *wsConn) close() {
	ws.closeNormal()
	ws.c.Close()
}

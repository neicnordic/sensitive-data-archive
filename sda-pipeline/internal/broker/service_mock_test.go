// Copyright (c) 2012-2019, Sean Treadway, SoundCloud Ltd.
// All rights reserved.

// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:

// Redistributions of source code must retain the above copyright notice, this
// list of conditions and the following disclaimer.

// Redistributions in binary form must reproduce the above copyright notice, this
// list of conditions and the following disclaimer in the documentation and/or
// other materials provided with the distribution.

// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
// WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
// SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
// CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
// OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package broker

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

//
// Stuff below this line is used for mocking the server interface
// code comes from github.com/streadway/amqp
//

//nolint:deadcode,unused
func confirm(confirms <-chan amqp.Confirmation, tag uint64) error {
	confirmed := <-confirms
	if !confirmed.Ack || confirmed.DeliveryTag != tag {
		return fmt.Errorf("failed delivery of delivery tag: %d", confirmed.DeliveryTag)
	}
	return nil
}

//nolint:deadcode,unused
func defaultConfig() amqp.Config {
	return amqp.Config{
		SASL:   []amqp.Authentication{&amqp.PlainAuth{Username: "guest", Password: "guest"}},
		Vhost:  "/",
		Locale: "en_US",
	}
}

//nolint:deadcode,unused
func newSession(t *testing.T) (io.ReadWriteCloser, *commonServer) {
	rs, wc := io.Pipe()
	rc, ws := io.Pipe()

	rws := &logIO{t, "server", pipe{rs, ws}}
	rwc := &logIO{t, "client", pipe{rc, wc}}

	return rwc, newServer(t, rws, rwc)
}

//nolint
type pipe struct {
	r *io.PipeReader
	w *io.PipeWriter
}

//nolint:deadcode,unused
func (p pipe) Read(b []byte) (int, error) {
	return p.r.Read(b)
}

//nolint:deadcode,unused
func (p pipe) Write(b []byte) (int, error) {
	return p.w.Write(b)
}

//nolint:deadcode,unused
func (p pipe) Close() error {
	p.r.Close()
	p.w.Close()
	return nil
}

//nolint:unused
type logIO struct {
	t      *testing.T
	prefix string
	proxy  io.ReadWriteCloser
}

//nolint:deadcode,unused
func (log *logIO) Read(p []byte) (n int, err error) {
	log.t.Logf("%s reading %d\n", log.prefix, len(p))
	n, err = log.proxy.Read(p)
	if err != nil {
		log.t.Logf("%s read %x: %v\n", log.prefix, p[0:n], err)
	} else {
		log.t.Logf("%s read:\n%s\n", log.prefix, hex.Dump(p[0:n]))
		//fmt.Printf("%s read:\n%s\n", log.prefix, hex.Dump(p[0:n]))
	}
	return
}

//nolint:deadcode,unused
func (log *logIO) Write(p []byte) (n int, err error) {
	log.t.Logf("%s writing %d\n", log.prefix, len(p))
	n, err = log.proxy.Write(p)
	if err != nil {
		log.t.Logf("%s write %d, %x: %v\n", log.prefix, len(p), p[0:n], err)
	} else {
		log.t.Logf("%s write %d:\n%s", log.prefix, len(p), hex.Dump(p[0:n]))
		//fmt.Printf("%s write %d:\n%s", log.prefix, len(p), hex.Dump(p[0:n]))
	}
	return
}

//nolint:deadcode,unused
func (log *logIO) Close() (err error) {
	err = log.proxy.Close()
	if err != nil {
		log.t.Logf("%s close : %v\n", log.prefix, err)
	} else {
		log.t.Logf("%s close\n", log.prefix)
	}
	return
}

//nolint:unused
func (t *commonServer) channelOpen(id int) {
	t.recv(id, &channelOpen{})
	t.send(id, &channelOpenOk{})
}

// TLS secured server
type tlsServer struct {
	net.Listener
	URL      string
	Config   *tls.Config
	Sessions chan *commonServer
}

func tlsServerConfig() *tls.Config {
	cfg := new(tls.Config)
	cfg.ClientCAs = x509.NewCertPool()
	if ca, err := os.ReadFile("../../dev_utils/certs/ca.pem"); err == nil {
		cfg.ClientCAs.AppendCertsFromPEM(ca)
	} else {
		fmt.Printf("caLoad: %v", err)
	}
	if cert, err := tls.LoadX509KeyPair("../../dev_utils/certs/mq.pem", "../../dev_utils/certs/mq-key.pem"); err == nil {
		cfg.Certificates = append(cfg.Certificates, cert)
	} else {
		fmt.Printf("certLoad: %v", err)
	}
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	return cfg
}

func (s *tlsServer) Serve(t *testing.T) {
	for {
		c, err := s.Accept()
		if err != nil {
			return
		}
		s.Sessions <- newServer(t, c, c)
	}
}

func startTLSServer(t *testing.T, port int, cfg *tls.Config) tlsServer {
	l, err := tls.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port), cfg)
	if err != nil {
		panic(fmt.Errorf("TLSServerStart: %s", err))
	}
	s := tlsServer{
		Listener: l,
		Config:   cfg,
		URL:      fmt.Sprintf("amqps://%s/", l.Addr().String()),
		Sessions: make(chan *commonServer),
	}
	go s.Serve(t)
	return s
}

// Non TLS secured server

type server struct {
	net.Listener
	URL      string
	Sessions chan *commonServer
}

func (s *server) Serve(t *testing.T) {
	for {
		c, err := s.Accept()
		if err != nil {
			return
		}
		s.Sessions <- newServer(t, c, c)
	}
}

func handleOneConnection(s chan *commonServer, failChannel, failDeclare bool) {
	session := <-s
	session.connectionOpen()

	session.recv(1, &channelOpen{})

	if failChannel == true {
		session.send(1, &channelClose{ReplyCode: 506, ReplyText: "Something", MethodID: 10, ClassID: 20})
		return
	}
	session.send(1, &channelOpenOk{})

	qD := queueDeclare{}
	session.recv(1, &qD)

	if failDeclare == true {
		session.send(1, &channelClose{ReplyCode: 506,
			ReplyText: "Something", MethodID: 10, ClassID: 50})
		return
	}
	session.send(1, &queueDeclareOk{})

	session.recv(1, &confirmSelect{})
	session.send(1, &confirmSelectOk{})

	//	session.connectionClose()
	//	session.S.Close()
}

func startServer(t *testing.T, port int) server {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		panic(fmt.Errorf("ServerStart: %s", err))
	}
	s := server{
		Listener: l,
		URL:      fmt.Sprintf("amqps://%s/", l.Addr().String()),
		Sessions: make(chan *commonServer),
	}
	go s.Serve(t)
	return s
}

type commonServer struct {
	*testing.T
	r reader
	w writer
	S io.ReadWriteCloser // Server IO
	C io.ReadWriteCloser // Client IO
	// captured client frames
	start connectionStartOk
	tune  connectionTuneOk
}

func newServer(t *testing.T, serverIO, clientIO io.ReadWriteCloser) *commonServer {
	return &commonServer{
		T: t,
		r: reader{serverIO},
		w: writer{serverIO},
		S: serverIO,
		C: clientIO,
	}
}

type connectionOpen struct {
	VirtualHost string
	reserved1   string
	reserved2   bool
}

func (t *commonServer) connectionOpen() {
	t.expectAMQP()
	t.connectionStart()
	t.connectionTune()
	t.recv(0, &connectionOpen{})
	t.send(0, &connectionOpenOk{})
}

func (msg *connectionOpen) id() (uint16, uint16) {
	return 10, 40
}

func (msg *connectionOpen) wait() bool {
	return true
}

func (msg *connectionOpen) write(w io.Writer) (err error) {
	var bits byte
	if err = writeShortstr(w, msg.VirtualHost); err != nil {
		return
	}
	if err = writeShortstr(w, msg.reserved1); err != nil {
		return
	}
	if msg.reserved2 {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *connectionOpen) read(r io.Reader) (err error) {
	var bits byte
	if msg.VirtualHost, err = readShortstr(r); err != nil {
		return
	}
	if msg.reserved1, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.reserved2 = (bits&(1<<0) > 0)
	return
}

//nolint:unused
func (t *commonServer) connectionClose() {
	t.recv(0, &connectionClose{})
	t.send(0, &connectionCloseOk{})
}

type connectionClose struct {
	ReplyCode uint16
	ReplyText string
	ClassID   uint16
	MethodID  uint16
}

type connectionCloseOk struct{}

// Extras
func readShortstr(r io.Reader) (v string, err error) {
	var length uint8
	if err = binary.Read(r, binary.BigEndian, &length); err != nil {
		return
	}
	bytes := make([]byte, length)
	if _, err = io.ReadFull(r, bytes); err != nil {
		return
	}
	return string(bytes), nil
}

func writeShortstr(w io.Writer, s string) (err error) {
	b := []byte(s)
	var length = uint8(len(b))
	if err = binary.Write(w, binary.BigEndian, length); err != nil {
		return
	}
	if _, err = w.Write(b[:length]); err != nil {
		return
	}
	return
}

type connectionOpenOk struct {
	reserved1 string
}

func (msg *connectionOpenOk) id() (uint16, uint16) {
	return 10, 41
}

func (msg *connectionOpenOk) wait() bool {
	return true
}

func (msg *connectionOpenOk) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.reserved1); err != nil {
		return
	}
	return
}

func (msg *connectionOpenOk) read(r io.Reader) (err error) {
	if msg.reserved1, err = readShortstr(r); err != nil {
		return
	}
	return
}

func (t *commonServer) connectionStart() {
	t.send(0, &connectionStart{
		VersionMajor: 0,
		VersionMinor: 9,
		Mechanisms:   "PLAIN",
		Locales:      "en_US",
	})
	t.recv(0, &t.start)
}

type connectionStart struct {
	VersionMajor     byte
	VersionMinor     byte
	ServerProperties Table
	Mechanisms       string
	Locales          string
}

func (msg *connectionStart) id() (uint16, uint16) {
	return 10, 10
}

func (msg *connectionStart) wait() bool {
	return true
}

func (msg *connectionStart) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.VersionMajor); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.VersionMinor); err != nil {
		return
	}
	if err = writeTable(w, msg.ServerProperties); err != nil {
		return
	}
	if err = writeLongstr(w, msg.Mechanisms); err != nil {
		return
	}
	if err = writeLongstr(w, msg.Locales); err != nil {
		return
	}
	return
}

func (msg *connectionStart) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.VersionMajor); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.VersionMinor); err != nil {
		return
	}
	if msg.ServerProperties, err = readTable(r); err != nil {
		return
	}
	if msg.Mechanisms, err = readLongstr(r); err != nil {
		return
	}
	if msg.Locales, err = readLongstr(r); err != nil {
		return
	}
	return
}

type message interface {
	id() (uint16, uint16)
	wait() bool
	read(io.Reader) error
	write(io.Writer) error
}

type headerFrame struct {
	ChannelID  uint16
	ClassID    uint16
	weight     uint16
	Size       uint64
	Properties properties
}

func (f *headerFrame) channel() uint16 { return f.ChannelID }

// Used by header frames to capture routing and header information
type properties struct {
	ContentType     string    // MIME content type
	ContentEncoding string    // MIME content encoding
	Headers         Table     // Application or header exchange table
	DeliveryMode    uint8     // queue implementation use - Transient (1) or Persistent (2)
	Priority        uint8     // queue implementation use - 0 to 9
	CorrelationID   string    // application use - correlation identifier
	ReplyTo         string    // application use - address to to reply to (ex: RPC)
	Expiration      string    // implementation use - message expiration spec
	MessageID       string    // application use - message identifier
	Timestamp       time.Time // application use - message timestamp
	Type            string    // application use - message type name
	UserID          string    // application use - creating user id
	AppID           string    // application use - creating application
	reserved1       string    // was cluster-id - process for buffer consumption
}

// drops all but method frames expected on the given channel
func (t *commonServer) recv(channel int, m message) message {
	defer time.AfterFunc(time.Second, func() { panic("recv deadlock") }).Stop()
	var remaining int
	var header *headerFrame
	var body []byte
	for {
		frame, err := t.r.ReadFrame()
		if err != nil {
			t.Fatalf("frame err, read: %s", err)
		}
		if frame.channel() != uint16(channel) {
			t.Fatalf("expected frame on channel %d, got channel %d", channel, frame.channel())
		}
		switch f := frame.(type) {
		case *heartbeatFrame:
			// drop
		case *headerFrame:
			// start content state
			header = f
			remaining = int(header.Size)
			if remaining == 0 {
				m.(messageWithContent).setContent(header.Properties, nil)
				return m
			}
		case *bodyFrame:
			// continue until terminated
			body = append(body, f.Body...)
			remaining -= len(f.Body)
			if remaining <= 0 {
				m.(messageWithContent).setContent(header.Properties, body)
				return m
			}
		case *methodFrame:
			if reflect.TypeOf(m) == reflect.TypeOf(f.Method) {
				wantv := reflect.ValueOf(m).Elem()
				havev := reflect.ValueOf(f.Method).Elem()
				wantv.Set(havev)
				if _, ok := m.(messageWithContent); !ok {
					return m
				}
			} else {
				t.Fatalf("expected method type: %T, got: %T", m, f.Method)
			}
		default:
			t.Fatalf("unexpected frame: %+v", f)
		}
	}
}

type messageWithContent interface {
	message
	getContent() (properties, []byte)
	setContent(properties, []byte)
}

func (t *commonServer) send(channel int, m message) {
	defer time.AfterFunc(time.Second, func() { panic("send deadlock") }).Stop()
	if msg, ok := m.(messageWithContent); ok {
		props, body := msg.getContent()
		class, _ := msg.id()
		_ = t.w.WriteFrame(&methodFrame{
			ChannelID: uint16(channel),
			Method:    msg,
		})
		_ = t.w.WriteFrame(&headerFrame{
			ChannelID:  uint16(channel),
			ClassID:    class,
			Size:       uint64(len(body)),
			Properties: props,
		})
		_ = t.w.WriteFrame(&bodyFrame{
			ChannelID: uint16(channel),
			Body:      body,
		})
	} else {
		_ = t.w.WriteFrame(&methodFrame{
			ChannelID: uint16(channel),
			Method:    m,
		})
	}
}

func (w *writer) WriteFrame(frame frame) (err error) {
	if err = frame.write(w.w); err != nil {
		return
	}
	if buf, ok := w.w.(*bufio.Writer); ok {
		err = buf.Flush()
	}
	return
}

func (t *commonServer) expectAMQP() {
	t.expectBytes([]byte{'A', 'M', 'Q', 'P', 0, 0, 9, 1})
}

func (t *commonServer) expectBytes(b []byte) {
	in := make([]byte, len(b))
	if _, err := io.ReadFull(t.S, in); err != nil {
		t.Fatalf("io error expecting bytes: %v", err)
	}
	if !bytes.Equal(b, in) {
		t.Fatalf("failed bytes: expected: %s got: %s", string(b), string(in))
	}
}

type Decimal struct {
	Scale uint8
	Value int32
}

func readArray(r io.Reader) ([]interface{}, error) {
	var (
		size uint32
		err  error
	)
	if err = binary.Read(r, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	var (
		lim   = &io.LimitedReader{R: r, N: int64(size)}
		arr   = []interface{}{}
		field interface{}
	)
	for {
		if field, err = readField(lim); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		arr = append(arr, field)
	}
	return arr, nil
}

func readTimestamp(r io.Reader) (v time.Time, err error) {
	var sec int64
	if err = binary.Read(r, binary.BigEndian, &sec); err != nil {
		return
	}
	return time.Unix(sec, 0), nil
}

func writeField(w io.Writer, value interface{}) (err error) {
	var buf [9]byte
	var enc []byte
	switch v := value.(type) {
	case bool:
		buf[0] = 't'
		if v {
			buf[1] = byte(1)
		} else {
			buf[1] = byte(0)
		}
		enc = buf[:2]
	case byte:
		buf[0] = 'b'
		buf[1] = byte(v)
		enc = buf[:2]
	case int16:
		buf[0] = 's'
		binary.BigEndian.PutUint16(buf[1:3], uint16(v))
		enc = buf[:3]
	case int:
		buf[0] = 'I'
		binary.BigEndian.PutUint32(buf[1:5], uint32(v))
		enc = buf[:5]
	case int32:
		buf[0] = 'I'
		binary.BigEndian.PutUint32(buf[1:5], uint32(v))
		enc = buf[:5]
	case int64:
		buf[0] = 'l'
		binary.BigEndian.PutUint64(buf[1:9], uint64(v))
		enc = buf[:9]
	case float32:
		buf[0] = 'f'
		binary.BigEndian.PutUint32(buf[1:5], math.Float32bits(v))
		enc = buf[:5]
	case float64:
		buf[0] = 'd'
		binary.BigEndian.PutUint64(buf[1:9], math.Float64bits(v))
		enc = buf[:9]
	case Decimal:
		buf[0] = 'D'
		buf[1] = byte(v.Scale)
		binary.BigEndian.PutUint32(buf[2:6], uint32(v.Value))
		enc = buf[:6]
	case string:
		buf[0] = 'S'
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(v)))
		enc = append(buf[:5], []byte(v)...)
	case []interface{}: // field-array
		buf[0] = 'A'
		sec := new(bytes.Buffer)
		for _, val := range v {
			if err = writeField(sec, val); err != nil {
				return
			}
		}
		binary.BigEndian.PutUint32(buf[1:5], uint32(sec.Len()))
		if _, err = w.Write(buf[:5]); err != nil {
			return
		}
		if _, err = w.Write(sec.Bytes()); err != nil {
			return
		}
		return
	case time.Time:
		buf[0] = 'T'
		binary.BigEndian.PutUint64(buf[1:9], uint64(v.Unix()))
		enc = buf[:9]
	case Table:
		if _, err = w.Write([]byte{'F'}); err != nil {
			return
		}
		return writeTable(w, v)
	case []byte:
		buf[0] = 'x'
		binary.BigEndian.PutUint32(buf[1:5], uint32(len(v)))
		if _, err = w.Write(buf[0:5]); err != nil {
			return
		}
		if _, err = w.Write(v); err != nil {
			return
		}
		return
	case nil:
		buf[0] = 'V'
		enc = buf[:1]
	default:
		return ErrFieldType
	}
	_, err = w.Write(enc)
	return
}

// type consts
const (
	frameMethod         = 1
	frameHeader         = 2
	frameBody           = 3
	frameHeartbeat      = 8
	frameMinSize        = 4096 //nolint
	frameEnd            = 206
	replySuccess        = 200 //nolint
	ContentTooLarge     = 311
	NoRoute             = 312
	NoConsumers         = 313
	ConnectionForced    = 320
	InvalidPath         = 402
	AccessRefused       = 403
	NotFound            = 404
	ResourceLocked      = 405
	PreconditionFailed  = 406
	FrameError          = 501
	SyntaxError         = 502
	CommandInvalid      = 503
	ChannelError        = 504
	UnexpectedFrame     = 505
	ResourceError       = 506
	NotAllowed          = 530
	NotImplemented      = 540
	InternalError       = 541
	flagContentType     = 0x8000
	flagContentEncoding = 0x4000
	flagHeaders         = 0x2000
	flagDeliveryMode    = 0x1000
	flagPriority        = 0x0800
	flagCorrelationID   = 0x0400
	flagReplyTo         = 0x0200
	flagExpiration      = 0x0100
	flagMessageID       = 0x0080
	flagTimestamp       = 0x0040
	flagType            = 0x0020
	flagUserID          = 0x0010
	flagAppID           = 0x0008
	flagReserved1       = 0x0004
)

// type vars
var (
	ErrSyntax           = &Error{Code: SyntaxError, Reason: "invalid field or value inside of a frame"}
	ErrFieldType        = &Error{Code: SyntaxError, Reason: "unsupported table field type"}
	ErrFrame            = &Error{Code: FrameError, Reason: "frame could not be parsed"}
	errHeartbeatPayload = errors.New("heartbeats should not have a payload")
)

type Error struct {
	Code    int    // constant code from the specification
	Reason  string // description of the error
	Server  bool   // true when initiated from the server, false when from this library
	Recover bool   // true when this error can be recovered by retrying later or with different parameters
}

func (e Error) Error() string {
	return fmt.Sprintf("Exception (%d) Reason: %q", e.Code, e.Reason)
}

func (msg *connectionClose) id() (uint16, uint16) {
	return 10, 50
}

func (msg *connectionClose) wait() bool {
	return true
}

func (msg *connectionClose) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.ReplyCode); err != nil {
		return
	}
	if err = writeShortstr(w, msg.ReplyText); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.ClassID); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.MethodID); err != nil {
		return
	}
	return
}

func (msg *connectionClose) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.ReplyCode); err != nil {
		return
	}
	if msg.ReplyText, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.ClassID); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.MethodID); err != nil {
		return
	}
	return
}

func (msg *connectionCloseOk) id() (uint16, uint16) {
	return 10, 51
}

func (msg *connectionCloseOk) wait() bool {
	return true
}

func (msg *connectionCloseOk) write(w io.Writer) (err error) {
	return
}

func (msg *connectionCloseOk) read(r io.Reader) (err error) {
	return
}

type heartbeatFrame struct {
	ChannelID uint16
}

func (f *heartbeatFrame) channel() uint16 { return f.ChannelID }

type bodyFrame struct {
	ChannelID uint16
	Body      []byte
}

func (f *bodyFrame) channel() uint16 { return f.ChannelID }

type methodFrame struct {
	ChannelID uint16
	ClassID   uint16
	MethodID  uint16
	Method    message
}

func (f *methodFrame) channel() uint16 { return f.ChannelID }

func (r *reader) ReadFrame() (frame frame, err error) {
	var scratch [7]byte
	if _, err = io.ReadFull(r.r, scratch[:7]); err != nil {
		return
	}
	typ := uint8(scratch[0])
	channel := binary.BigEndian.Uint16(scratch[1:3])
	size := binary.BigEndian.Uint32(scratch[3:7])
	switch typ {
	case frameMethod:
		if frame, err = r.parseMethodFrame(channel, size); err != nil {
			return
		}
	case frameHeader:
		if frame, err = r.parseHeaderFrame(channel, size); err != nil {
			return
		}
	case frameBody:
		if frame, err = r.parseBodyFrame(channel, size); err != nil {
			return nil, err
		}
	case frameHeartbeat:
		if frame, err = r.parseHeartbeatFrame(channel, size); err != nil {
			return
		}
	default:
		return nil, ErrFrame
	}
	if _, err = io.ReadFull(r.r, scratch[:1]); err != nil {
		return nil, err
	}
	if scratch[0] != frameEnd {
		return nil, ErrFrame
	}
	return
}

type frame interface {
	write(io.Writer) error
	channel() uint16
}

func (r *reader) parseHeaderFrame(channel uint16, size uint32) (frame frame, err error) {
	hf := &headerFrame{
		ChannelID: channel,
	}
	if err = binary.Read(r.r, binary.BigEndian, &hf.ClassID); err != nil {
		return
	}
	if err = binary.Read(r.r, binary.BigEndian, &hf.weight); err != nil {
		return
	}
	if err = binary.Read(r.r, binary.BigEndian, &hf.Size); err != nil {
		return
	}
	var flags uint16
	if err = binary.Read(r.r, binary.BigEndian, &flags); err != nil {
		return
	}
	if hasProperty(flags, flagContentType) {
		if hf.Properties.ContentType, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagContentEncoding) {
		if hf.Properties.ContentEncoding, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagHeaders) {
		if hf.Properties.Headers, err = readTable(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagDeliveryMode) {
		if err = binary.Read(r.r, binary.BigEndian, &hf.Properties.DeliveryMode); err != nil {
			return
		}
	}
	if hasProperty(flags, flagPriority) {
		if err = binary.Read(r.r, binary.BigEndian, &hf.Properties.Priority); err != nil {
			return
		}
	}
	if hasProperty(flags, flagCorrelationID) {
		if hf.Properties.CorrelationID, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagReplyTo) {
		if hf.Properties.ReplyTo, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagExpiration) {
		if hf.Properties.Expiration, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagMessageID) {
		if hf.Properties.MessageID, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagTimestamp) {
		if hf.Properties.Timestamp, err = readTimestamp(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagType) {
		if hf.Properties.Type, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagUserID) {
		if hf.Properties.UserID, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagAppID) {
		if hf.Properties.AppID, err = readShortstr(r.r); err != nil {
			return
		}
	}
	if hasProperty(flags, flagReserved1) {
		if hf.Properties.reserved1, err = readShortstr(r.r); err != nil {
			return
		}
	}
	return hf, nil
}

func (f *heartbeatFrame) write(w io.Writer) (err error) {
	return writeFrame(w, frameHeartbeat, f.ChannelID, []byte{})
}

func (f *bodyFrame) write(w io.Writer) (err error) {
	return writeFrame(w, frameBody, f.ChannelID, f.Body)
}

func (f *methodFrame) write(w io.Writer) (err error) {
	var payload bytes.Buffer
	if f.Method == nil {
		return errors.New("malformed frame: missing method")
	}
	class, method := f.Method.id()
	if err = binary.Write(&payload, binary.BigEndian, class); err != nil {
		return
	}
	if err = binary.Write(&payload, binary.BigEndian, method); err != nil {
		return
	}
	if err = f.Method.write(&payload); err != nil {
		return
	}
	return writeFrame(w, frameMethod, f.ChannelID, payload.Bytes())
}

func (f *headerFrame) write(w io.Writer) (err error) {
	var payload bytes.Buffer
	var zeroTime time.Time
	if err = binary.Write(&payload, binary.BigEndian, f.ClassID); err != nil {
		return
	}
	if err = binary.Write(&payload, binary.BigEndian, f.weight); err != nil {
		return
	}
	if err = binary.Write(&payload, binary.BigEndian, f.Size); err != nil {
		return
	}
	// First pass will build the mask to be serialized, second pass will serialize
	// each of the fields that appear in the mask.
	var mask uint16
	if len(f.Properties.ContentType) > 0 {
		mask = mask | flagContentType
	}
	if len(f.Properties.ContentEncoding) > 0 {
		mask = mask | flagContentEncoding
	}
	if f.Properties.Headers != nil && len(f.Properties.Headers) > 0 {
		mask = mask | flagHeaders
	}
	if f.Properties.DeliveryMode > 0 {
		mask = mask | flagDeliveryMode
	}
	if f.Properties.Priority > 0 {
		mask = mask | flagPriority
	}
	if len(f.Properties.CorrelationID) > 0 {
		mask = mask | flagCorrelationID
	}
	if len(f.Properties.ReplyTo) > 0 {
		mask = mask | flagReplyTo
	}
	if len(f.Properties.Expiration) > 0 {
		mask = mask | flagExpiration
	}
	if len(f.Properties.MessageID) > 0 {
		mask = mask | flagMessageID
	}
	if f.Properties.Timestamp != zeroTime {
		mask = mask | flagTimestamp
	}
	if len(f.Properties.Type) > 0 {
		mask = mask | flagType
	}
	if len(f.Properties.UserID) > 0 {
		mask = mask | flagUserID
	}
	if len(f.Properties.AppID) > 0 {
		mask = mask | flagAppID
	}
	if err = binary.Write(&payload, binary.BigEndian, mask); err != nil {
		return
	}
	if hasProperty(mask, flagContentType) {
		if err = writeShortstr(&payload, f.Properties.ContentType); err != nil {
			return
		}
	}
	if hasProperty(mask, flagContentEncoding) {
		if err = writeShortstr(&payload, f.Properties.ContentEncoding); err != nil {
			return
		}
	}
	if hasProperty(mask, flagHeaders) {
		if err = writeTable(&payload, f.Properties.Headers); err != nil {
			return
		}
	}
	if hasProperty(mask, flagDeliveryMode) {
		if err = binary.Write(&payload, binary.BigEndian, f.Properties.DeliveryMode); err != nil {
			return
		}
	}
	if hasProperty(mask, flagPriority) {
		if err = binary.Write(&payload, binary.BigEndian, f.Properties.Priority); err != nil {
			return
		}
	}
	if hasProperty(mask, flagCorrelationID) {
		if err = writeShortstr(&payload, f.Properties.CorrelationID); err != nil {
			return
		}
	}
	if hasProperty(mask, flagReplyTo) {
		if err = writeShortstr(&payload, f.Properties.ReplyTo); err != nil {
			return
		}
	}
	if hasProperty(mask, flagExpiration) {
		if err = writeShortstr(&payload, f.Properties.Expiration); err != nil {
			return
		}
	}
	if hasProperty(mask, flagMessageID) {
		if err = writeShortstr(&payload, f.Properties.MessageID); err != nil {
			return
		}
	}
	if hasProperty(mask, flagTimestamp) {
		if err = binary.Write(&payload, binary.BigEndian, uint64(f.Properties.Timestamp.Unix())); err != nil {
			return
		}
	}
	if hasProperty(mask, flagType) {
		if err = writeShortstr(&payload, f.Properties.Type); err != nil {
			return
		}
	}
	if hasProperty(mask, flagUserID) {
		if err = writeShortstr(&payload, f.Properties.UserID); err != nil {
			return
		}
	}
	if hasProperty(mask, flagAppID) {
		if err = writeShortstr(&payload, f.Properties.AppID); err != nil {
			return
		}
	}
	return writeFrame(w, frameHeader, f.ChannelID, payload.Bytes())
}

func (r *reader) parseMethodFrame(channel uint16, size uint32) (f frame, err error) {
	mf := &methodFrame{
		ChannelID: channel,
	}
	if err = binary.Read(r.r, binary.BigEndian, &mf.ClassID); err != nil {
		return
	}
	if err = binary.Read(r.r, binary.BigEndian, &mf.MethodID); err != nil {
		return
	}
	switch mf.ClassID {
	case 10: // connection
		switch mf.MethodID {
		case 10: // connection start
			//fmt.Println("NextMethod: class:10 method:10")
			method := &connectionStart{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // connection start-ok
			//fmt.Println("NextMethod: class:10 method:11")
			method := &connectionStartOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // connection secure
			//fmt.Println("NextMethod: class:10 method:20")
			method := &connectionSecure{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // connection secure-ok
			//fmt.Println("NextMethod: class:10 method:21")
			method := &connectionSecureOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 30: // connection tune
			//fmt.Println("NextMethod: class:10 method:30")
			method := &connectionTune{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 31: // connection tune-ok
			//fmt.Println("NextMethod: class:10 method:31")
			method := &connectionTuneOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 40: // connection open
			//fmt.Println("NextMethod: class:10 method:40")
			method := &connectionOpen{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 41: // connection open-ok
			//fmt.Println("NextMethod: class:10 method:41")
			method := &connectionOpenOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 50: // connection close
			//fmt.Println("NextMethod: class:10 method:50")
			method := &connectionClose{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 51: // connection close-ok
			//fmt.Println("NextMethod: class:10 method:51")
			method := &connectionCloseOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 60: // connection blocked
			//fmt.Println("NextMethod: class:10 method:60")
			method := &connectionBlocked{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 61: // connection unblocked
			//fmt.Println("NextMethod: class:10 method:61")
			method := &connectionUnblocked{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 20: // channel
		switch mf.MethodID {
		case 10: // channel open
			// fmt.Println("NextMethod: class:20 method:10")
			method := &channelOpen{}
			if err = method.read(r.r); err != nil {
				fmt.Println("NextMethod: class:20 method:10 fail")
				return
			}
			// fmt.Println("NextMethod: class:20 method:10 done")

			mf.Method = method
		case 11: // channel open-ok
			//fmt.Println("NextMethod: class:20 method:11")
			method := &channelOpenOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // channel flow
			//fmt.Println("NextMethod: class:20 method:20")
			method := &channelFlow{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // channel flow-ok
			//fmt.Println("NextMethod: class:20 method:21")
			method := &channelFlowOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 40: // channel close
			//fmt.Println("NextMethod: class:20 method:40")
			method := &channelClose{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 41: // channel close-ok
			//fmt.Println("NextMethod: class:20 method:41")
			method := &channelCloseOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 40: // exchange
		switch mf.MethodID {
		case 10: // exchange declare
			//fmt.Println("NextMethod: class:40 method:10")
			method := &exchangeDeclare{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // exchange declare-ok
			//fmt.Println("NextMethod: class:40 method:11")
			method := &exchangeDeclareOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // exchange delete
			//fmt.Println("NextMethod: class:40 method:20")
			method := &exchangeDelete{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // exchange delete-ok
			//fmt.Println("NextMethod: class:40 method:21")
			method := &exchangeDeleteOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 30: // exchange bind
			//fmt.Println("NextMethod: class:40 method:30")
			method := &exchangeBind{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 31: // exchange bind-ok
			//fmt.Println("NextMethod: class:40 method:31")
			method := &exchangeBindOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 40: // exchange unbind
			//fmt.Println("NextMethod: class:40 method:40")
			method := &exchangeUnbind{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 51: // exchange unbind-ok
			//fmt.Println("NextMethod: class:40 method:51")
			method := &exchangeUnbindOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 50: // queue
		switch mf.MethodID {
		case 10: // queue declare
			//fmt.Println("NextMethod: class:50 method:10")
			method := &queueDeclare{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // queue declare-ok
			//fmt.Println("NextMethod: class:50 method:11")
			method := &queueDeclareOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // queue bind
			//fmt.Println("NextMethod: class:50 method:20")
			method := &queueBind{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // queue bind-ok
			//fmt.Println("NextMethod: class:50 method:21")
			method := &queueBindOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 50: // queue unbind
			//fmt.Println("NextMethod: class:50 method:50")
			method := &queueUnbind{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 51: // queue unbind-ok
			//fmt.Println("NextMethod: class:50 method:51")
			method := &queueUnbindOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 30: // queue purge
			//fmt.Println("NextMethod: class:50 method:30")
			method := &queuePurge{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 31: // queue purge-ok
			//fmt.Println("NextMethod: class:50 method:31")
			method := &queuePurgeOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 40: // queue delete
			//fmt.Println("NextMethod: class:50 method:40")
			method := &queueDelete{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 41: // queue delete-ok
			//fmt.Println("NextMethod: class:50 method:41")
			method := &queueDeleteOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 60: // basic
		switch mf.MethodID {
		case 10: // basic qos
			//fmt.Println("NextMethod: class:60 method:10")
			method := &basicQos{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // basic qos-ok
			//fmt.Println("NextMethod: class:60 method:11")
			method := &basicQosOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // basic consume
			//fmt.Println("NextMethod: class:60 method:20")
			method := &basicConsume{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // basic consume-ok
			//fmt.Println("NextMethod: class:60 method:21")
			method := &basicConsumeOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 30: // basic cancel
			//fmt.Println("NextMethod: class:60 method:30")
			method := &basicCancel{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 31: // basic cancel-ok
			//fmt.Println("NextMethod: class:60 method:31")
			method := &basicCancelOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 40: // basic publish
			//fmt.Println("NextMethod: class:60 method:40")
			method := &basicPublish{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 50: // basic return
			//fmt.Println("NextMethod: class:60 method:50")
			method := &basicReturn{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 60: // basic deliver
			//fmt.Println("NextMethod: class:60 method:60")
			method := &basicDeliver{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 70: // basic get
			//fmt.Println("NextMethod: class:60 method:70")
			method := &basicGet{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 71: // basic get-ok
			//fmt.Println("NextMethod: class:60 method:71")
			method := &basicGetOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 72: // basic get-empty
			//fmt.Println("NextMethod: class:60 method:72")
			method := &basicGetEmpty{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 80: // basic ack
			//fmt.Println("NextMethod: class:60 method:80")
			method := &basicAck{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 90: // basic reject
			//fmt.Println("NextMethod: class:60 method:90")
			method := &basicReject{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 100: // basic recover-async
			//fmt.Println("NextMethod: class:60 method:100")
			method := &basicRecoverAsync{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 110: // basic recover
			//fmt.Println("NextMethod: class:60 method:110")
			method := &basicRecover{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 111: // basic recover-ok
			//fmt.Println("NextMethod: class:60 method:111")
			method := &basicRecoverOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 120: // basic nack
			//fmt.Println("NextMethod: class:60 method:120")
			method := &basicNack{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 90: // tx
		switch mf.MethodID {
		case 10: // tx select
			//fmt.Println("NextMethod: class:90 method:10")
			method := &txSelect{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // tx select-ok
			//fmt.Println("NextMethod: class:90 method:11")
			method := &txSelectOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 20: // tx commit
			//fmt.Println("NextMethod: class:90 method:20")
			method := &txCommit{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 21: // tx commit-ok
			//fmt.Println("NextMethod: class:90 method:21")
			method := &txCommitOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 30: // tx rollback
			//fmt.Println("NextMethod: class:90 method:30")
			method := &txRollback{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 31: // tx rollback-ok
			//fmt.Println("NextMethod: class:90 method:31")
			method := &txRollbackOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	case 85: // confirm
		switch mf.MethodID {
		case 10: // confirm select
			//fmt.Println("NextMethod: class:85 method:10")
			method := &confirmSelect{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		case 11: // confirm select-ok
			//fmt.Println("NextMethod: class:85 method:11")
			method := &confirmSelectOk{}
			if err = method.read(r.r); err != nil {
				return
			}
			mf.Method = method
		default:
			return nil, fmt.Errorf("Bad method frame, unknown method %d for class %d", mf.MethodID, mf.ClassID)
		}
	default:
		return nil, fmt.Errorf("Bad method frame, unknown class %d", mf.ClassID)
	}
	return mf, nil
}

func (r *reader) parseBodyFrame(channel uint16, size uint32) (frame frame, err error) {
	bf := &bodyFrame{
		ChannelID: channel,
		Body:      make([]byte, size),
	}
	if _, err = io.ReadFull(r.r, bf.Body); err != nil {
		return nil, err
	}
	return bf, nil
}

func (r *reader) parseHeartbeatFrame(channel uint16, size uint32) (frame frame, err error) {
	hf := &heartbeatFrame{
		ChannelID: channel,
	}
	if size > 0 {
		return nil, errHeartbeatPayload
	}
	return hf, nil
}

func hasProperty(mask uint16, prop int) bool {
	return int(mask)&prop > 0
}

// Connection tune components
type connectionTune struct {
	ChannelMax uint16
	FrameMax   uint32
	Heartbeat  uint16
}

func (msg *connectionTune) id() (uint16, uint16) {
	return 10, 30
}

func (msg *connectionTune) wait() bool {
	return true
}

func (t *commonServer) connectionTune() {
	t.send(0, &connectionTune{
		ChannelMax: 11,
		FrameMax:   20000,
		Heartbeat:  10,
	})
	t.recv(0, &t.tune)
}

func (msg *connectionTune) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.ChannelMax); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.FrameMax); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.Heartbeat); err != nil {
		return
	}
	return
}

func (msg *connectionTune) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.ChannelMax); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.FrameMax); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.Heartbeat); err != nil {
		return
	}
	return
}

type connectionTuneOk struct {
	ChannelMax uint16
	FrameMax   uint32
	Heartbeat  uint16
}

func (msg *connectionTuneOk) id() (uint16, uint16) {
	return 10, 31
}

func (msg *connectionTuneOk) wait() bool {
	return true
}

func (msg *connectionTuneOk) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.ChannelMax); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.FrameMax); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.Heartbeat); err != nil {
		return
	}
	return
}

func (msg *connectionTuneOk) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.ChannelMax); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.FrameMax); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.Heartbeat); err != nil {
		return
	}
	return
}

type connectionSecure struct {
	Challenge string
}

func (msg *connectionSecure) id() (uint16, uint16) {
	return 10, 20
}

func (msg *connectionSecure) wait() bool {
	return true
}

func (msg *connectionSecure) write(w io.Writer) (err error) {
	if err = writeLongstr(w, msg.Challenge); err != nil {
		return
	}
	return
}

func (msg *connectionSecure) read(r io.Reader) (err error) {
	if msg.Challenge, err = readLongstr(r); err != nil {
		return
	}
	return
}

type connectionSecureOk struct {
	Response string
}

func (msg *connectionSecureOk) id() (uint16, uint16) {
	return 10, 21
}

func (msg *connectionSecureOk) wait() bool {
	return true
}

func (msg *connectionSecureOk) write(w io.Writer) (err error) {
	if err = writeLongstr(w, msg.Response); err != nil {
		return
	}
	return
}

func (msg *connectionSecureOk) read(r io.Reader) (err error) {
	if msg.Response, err = readLongstr(r); err != nil {
		return
	}
	return
}

type connectionBlocked struct {
	Reason string
}

func (msg *connectionBlocked) id() (uint16, uint16) {
	return 10, 60
}

func (msg *connectionBlocked) wait() bool {
	return false
}

func (msg *connectionBlocked) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.Reason); err != nil {
		return
	}
	return
}

func (msg *connectionBlocked) read(r io.Reader) (err error) {
	if msg.Reason, err = readShortstr(r); err != nil {
		return
	}
	return
}

type connectionUnblocked struct{}

func (msg *connectionUnblocked) id() (uint16, uint16) {
	return 10, 61
}

func (msg *connectionUnblocked) wait() bool {
	return false
}

func (msg *connectionUnblocked) write(w io.Writer) (err error) {
	return
}

func (msg *connectionUnblocked) read(r io.Reader) (err error) {
	return
}

// Channel open components
type channelOpen struct {
	reserved1 string
}

func (msg *channelOpen) id() (uint16, uint16) {
	return 20, 10
}

func (msg *channelOpen) wait() bool {
	return true
}

func (msg *channelOpen) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.reserved1); err != nil {
		return
	}
	return
}

func (msg *channelOpen) read(r io.Reader) (err error) {
	if msg.reserved1, err = readShortstr(r); err != nil {
		return
	}
	return
}

type channelOpenOk struct {
	reserved1 string
}

func (msg *channelOpenOk) id() (uint16, uint16) {
	return 20, 11
}

func (msg *channelOpenOk) wait() bool {
	return true
}

func (msg *channelOpenOk) write(w io.Writer) (err error) {
	if err = writeLongstr(w, msg.reserved1); err != nil {
		return
	}
	return
}

func (msg *channelOpenOk) read(r io.Reader) (err error) {
	if msg.reserved1, err = readLongstr(r); err != nil {
		return
	}
	return
}

type channelFlow struct {
	Active bool
}

func (msg *channelFlow) id() (uint16, uint16) {
	return 20, 20
}

func (msg *channelFlow) wait() bool {
	return true
}

func (msg *channelFlow) write(w io.Writer) (err error) {
	var bits byte
	if msg.Active {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *channelFlow) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Active = (bits&(1<<0) > 0)
	return
}

type channelFlowOk struct {
	Active bool
}

func (msg *channelFlowOk) id() (uint16, uint16) {
	return 20, 21
}

func (msg *channelFlowOk) wait() bool {
	return false
}

func (msg *channelFlowOk) write(w io.Writer) (err error) {
	var bits byte
	if msg.Active {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *channelFlowOk) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Active = (bits&(1<<0) > 0)
	return
}

type channelClose struct {
	ReplyCode uint16
	ReplyText string
	ClassID   uint16
	MethodID  uint16
}

func (msg *channelClose) id() (uint16, uint16) {
	return 20, 40
}

func (msg *channelClose) wait() bool {
	return true
}

func (msg *channelClose) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.ReplyCode); err != nil {
		return
	}
	if err = writeShortstr(w, msg.ReplyText); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.ClassID); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.MethodID); err != nil {
		return
	}
	return
}

func (msg *channelClose) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.ReplyCode); err != nil {
		return
	}
	if msg.ReplyText, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.ClassID); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.MethodID); err != nil {
		return
	}
	return
}

type channelCloseOk struct{}

func (msg *channelCloseOk) id() (uint16, uint16) {
	return 20, 41
}

func (msg *channelCloseOk) wait() bool {
	return true
}

func (msg *channelCloseOk) write(w io.Writer) (err error) {
	return
}

func (msg *channelCloseOk) read(r io.Reader) (err error) {
	return
}

type exchangeDeclare struct {
	reserved1  uint16
	Exchange   string
	Type       string
	Passive    bool
	Durable    bool
	AutoDelete bool
	Internal   bool
	NoWait     bool
	Arguments  Table
}

func (msg *exchangeDeclare) id() (uint16, uint16) {
	return 40, 10
}

func (msg *exchangeDeclare) wait() bool {
	return true && !msg.NoWait
}

func (msg *exchangeDeclare) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Type); err != nil {
		return
	}
	if msg.Passive {
		bits |= 1 << 0
	}
	if msg.Durable {
		bits |= 1 << 1
	}
	if msg.AutoDelete {
		bits |= 1 << 2
	}
	if msg.Internal {
		bits |= 1 << 3
	}
	if msg.NoWait {
		bits |= 1 << 4
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *exchangeDeclare) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.Type, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Passive = (bits&(1<<0) > 0)
	msg.Durable = (bits&(1<<1) > 0)
	msg.AutoDelete = (bits&(1<<2) > 0)
	msg.Internal = (bits&(1<<3) > 0)
	msg.NoWait = (bits&(1<<4) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type exchangeDeclareOk struct{}

func (msg *exchangeDeclareOk) id() (uint16, uint16) {
	return 40, 11
}

func (msg *exchangeDeclareOk) wait() bool {
	return true
}

func (msg *exchangeDeclareOk) write(w io.Writer) (err error) {
	return
}

func (msg *exchangeDeclareOk) read(r io.Reader) (err error) {
	return
}

type exchangeDelete struct {
	reserved1 uint16
	Exchange  string
	IfUnused  bool
	NoWait    bool
}

func (msg *exchangeDelete) id() (uint16, uint16) {
	return 40, 20
}

func (msg *exchangeDelete) wait() bool {
	return true && !msg.NoWait
}

func (msg *exchangeDelete) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if msg.IfUnused {
		bits |= 1 << 0
	}
	if msg.NoWait {
		bits |= 1 << 1
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *exchangeDelete) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.IfUnused = (bits&(1<<0) > 0)
	msg.NoWait = (bits&(1<<1) > 0)
	return
}

type exchangeDeleteOk struct{}

func (msg *exchangeDeleteOk) id() (uint16, uint16) {
	return 40, 21
}

func (msg *exchangeDeleteOk) wait() bool {
	return true
}

func (msg *exchangeDeleteOk) write(w io.Writer) (err error) {
	return
}

func (msg *exchangeDeleteOk) read(r io.Reader) (err error) {
	return
}

type exchangeBind struct {
	reserved1   uint16
	Destination string
	Source      string
	RoutingKey  string
	NoWait      bool
	Arguments   Table
}

func (msg *exchangeBind) id() (uint16, uint16) {
	return 40, 30
}

func (msg *exchangeBind) wait() bool {
	return true && !msg.NoWait
}

func (msg *exchangeBind) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Destination); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Source); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if msg.NoWait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *exchangeBind) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Destination, err = readShortstr(r); err != nil {
		return
	}
	if msg.Source, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoWait = (bits&(1<<0) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type exchangeBindOk struct{}

func (msg *exchangeBindOk) id() (uint16, uint16) {
	return 40, 31
}

func (msg *exchangeBindOk) wait() bool {
	return true
}

func (msg *exchangeBindOk) write(w io.Writer) (err error) {
	return
}

func (msg *exchangeBindOk) read(r io.Reader) (err error) {
	return
}

type exchangeUnbind struct {
	reserved1   uint16
	Destination string
	Source      string
	RoutingKey  string
	NoWait      bool
	Arguments   Table
}

func (msg *exchangeUnbind) id() (uint16, uint16) {
	return 40, 40
}

func (msg *exchangeUnbind) wait() bool {
	return true && !msg.NoWait
}

func (msg *exchangeUnbind) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Destination); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Source); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if msg.NoWait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *exchangeUnbind) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Destination, err = readShortstr(r); err != nil {
		return
	}
	if msg.Source, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoWait = (bits&(1<<0) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type exchangeUnbindOk struct{}

func (msg *exchangeUnbindOk) id() (uint16, uint16) {
	return 40, 51
}

func (msg *exchangeUnbindOk) wait() bool {
	return true
}

func (msg *exchangeUnbindOk) write(w io.Writer) (err error) {
	return
}

func (msg *exchangeUnbindOk) read(r io.Reader) (err error) {
	return
}

type queueDeclare struct {
	reserved1  uint16
	Queue      string
	Passive    bool
	Durable    bool
	Exclusive  bool
	AutoDelete bool
	NoWait     bool
	Arguments  Table
}

func (msg *queueDeclare) id() (uint16, uint16) {
	return 50, 10
}

func (msg *queueDeclare) wait() bool {
	return true && !msg.NoWait
}

func (msg *queueDeclare) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if msg.Passive {
		bits |= 1 << 0
	}
	if msg.Durable {
		bits |= 1 << 1
	}
	if msg.Exclusive {
		bits |= 1 << 2
	}
	if msg.AutoDelete {
		bits |= 1 << 3
	}
	if msg.NoWait {
		bits |= 1 << 4
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *queueDeclare) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Passive = (bits&(1<<0) > 0)
	msg.Durable = (bits&(1<<1) > 0)
	msg.Exclusive = (bits&(1<<2) > 0)
	msg.AutoDelete = (bits&(1<<3) > 0)
	msg.NoWait = (bits&(1<<4) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type queueDeclareOk struct {
	Queue         string
	MessageCount  uint32
	ConsumerCount uint32
}

func (msg *queueDeclareOk) id() (uint16, uint16) {
	return 50, 11
}

func (msg *queueDeclareOk) wait() bool {
	return true
}

func (msg *queueDeclareOk) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.MessageCount); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.ConsumerCount); err != nil {
		return
	}
	return
}

func (msg *queueDeclareOk) read(r io.Reader) (err error) {
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.MessageCount); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.ConsumerCount); err != nil {
		return
	}
	return
}

type queueBind struct {
	reserved1  uint16
	Queue      string
	Exchange   string
	RoutingKey string
	NoWait     bool
	Arguments  Table
}

func (msg *queueBind) id() (uint16, uint16) {
	return 50, 20
}

func (msg *queueBind) wait() bool {
	return true && !msg.NoWait
}

func (msg *queueBind) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if msg.NoWait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *queueBind) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoWait = (bits&(1<<0) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type queueBindOk struct{}

func (msg *queueBindOk) id() (uint16, uint16) {
	return 50, 21
}

func (msg *queueBindOk) wait() bool {
	return true
}

func (msg *queueBindOk) write(w io.Writer) (err error) {
	return
}

func (msg *queueBindOk) read(r io.Reader) (err error) {
	return
}

type queueUnbind struct {
	reserved1  uint16
	Queue      string
	Exchange   string
	RoutingKey string
	Arguments  Table
}

func (msg *queueUnbind) id() (uint16, uint16) {
	return 50, 50
}

func (msg *queueUnbind) wait() bool {
	return true
}

func (msg *queueUnbind) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *queueUnbind) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type queueUnbindOk struct{}

func (msg *queueUnbindOk) id() (uint16, uint16) {
	return 50, 51
}

func (msg *queueUnbindOk) wait() bool {
	return true
}

func (msg *queueUnbindOk) write(w io.Writer) (err error) {
	return
}

func (msg *queueUnbindOk) read(r io.Reader) (err error) {
	return
}

type queuePurge struct {
	reserved1 uint16
	Queue     string
	NoWait    bool
}

func (msg *queuePurge) id() (uint16, uint16) {
	return 50, 30
}

func (msg *queuePurge) wait() bool {
	return true && !msg.NoWait
}

func (msg *queuePurge) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if msg.NoWait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *queuePurge) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoWait = (bits&(1<<0) > 0)
	return
}

type queuePurgeOk struct {
	MessageCount uint32
}

func (msg *queuePurgeOk) id() (uint16, uint16) {
	return 50, 31
}

func (msg *queuePurgeOk) wait() bool {
	return true
}

func (msg *queuePurgeOk) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.MessageCount); err != nil {
		return
	}
	return
}

func (msg *queuePurgeOk) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.MessageCount); err != nil {
		return
	}
	return
}

type queueDelete struct {
	reserved1 uint16
	Queue     string
	IfUnused  bool
	IfEmpty   bool
	NoWait    bool
}

func (msg *queueDelete) id() (uint16, uint16) {
	return 50, 40
}

func (msg *queueDelete) wait() bool {
	return true && !msg.NoWait
}

func (msg *queueDelete) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if msg.IfUnused {
		bits |= 1 << 0
	}
	if msg.IfEmpty {
		bits |= 1 << 1
	}
	if msg.NoWait {
		bits |= 1 << 2
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *queueDelete) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.IfUnused = (bits&(1<<0) > 0)
	msg.IfEmpty = (bits&(1<<1) > 0)
	msg.NoWait = (bits&(1<<2) > 0)
	return
}

type queueDeleteOk struct {
	MessageCount uint32
}

func (msg *queueDeleteOk) id() (uint16, uint16) {
	return 50, 41
}

func (msg *queueDeleteOk) wait() bool {
	return true
}

func (msg *queueDeleteOk) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.MessageCount); err != nil {
		return
	}
	return
}

func (msg *queueDeleteOk) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.MessageCount); err != nil {
		return
	}
	return
}

type basicQos struct {
	PrefetchSize  uint32
	PrefetchCount uint16
	Global        bool
}

func (msg *basicQos) id() (uint16, uint16) {
	return 60, 10
}

func (msg *basicQos) wait() bool {
	return true
}

func (msg *basicQos) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.PrefetchSize); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.PrefetchCount); err != nil {
		return
	}
	if msg.Global {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicQos) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.PrefetchSize); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.PrefetchCount); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Global = (bits&(1<<0) > 0)
	return
}

type basicQosOk struct{}

func (msg *basicQosOk) id() (uint16, uint16) {
	return 60, 11
}

func (msg *basicQosOk) wait() bool {
	return true
}

func (msg *basicQosOk) write(w io.Writer) (err error) {
	return
}

func (msg *basicQosOk) read(r io.Reader) (err error) {
	return
}

type basicConsume struct {
	reserved1   uint16
	Queue       string
	ConsumerTag string
	NoLocal     bool
	NoAck       bool
	Exclusive   bool
	NoWait      bool
	Arguments   Table
}

func (msg *basicConsume) id() (uint16, uint16) {
	return 60, 20
}

func (msg *basicConsume) wait() bool {
	return true && !msg.NoWait
}

func (msg *basicConsume) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if err = writeShortstr(w, msg.ConsumerTag); err != nil {
		return
	}
	if msg.NoLocal {
		bits |= 1 << 0
	}
	if msg.NoAck {
		bits |= 1 << 1
	}
	if msg.Exclusive {
		bits |= 1 << 2
	}
	if msg.NoWait {
		bits |= 1 << 3
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeTable(w, msg.Arguments); err != nil {
		return
	}
	return
}

func (msg *basicConsume) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if msg.ConsumerTag, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoLocal = (bits&(1<<0) > 0)
	msg.NoAck = (bits&(1<<1) > 0)
	msg.Exclusive = (bits&(1<<2) > 0)
	msg.NoWait = (bits&(1<<3) > 0)
	if msg.Arguments, err = readTable(r); err != nil {
		return
	}
	return
}

type basicConsumeOk struct {
	ConsumerTag string
}

func (msg *basicConsumeOk) id() (uint16, uint16) {
	return 60, 21
}

func (msg *basicConsumeOk) wait() bool {
	return true
}

func (msg *basicConsumeOk) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.ConsumerTag); err != nil {
		return
	}
	return
}

func (msg *basicConsumeOk) read(r io.Reader) (err error) {
	if msg.ConsumerTag, err = readShortstr(r); err != nil {
		return
	}
	return
}

type basicCancel struct {
	ConsumerTag string
	NoWait      bool
}

func (msg *basicCancel) id() (uint16, uint16) {
	return 60, 30
}

func (msg *basicCancel) wait() bool {
	return true && !msg.NoWait
}

func (msg *basicCancel) write(w io.Writer) (err error) {
	var bits byte
	if err = writeShortstr(w, msg.ConsumerTag); err != nil {
		return
	}
	if msg.NoWait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicCancel) read(r io.Reader) (err error) {
	var bits byte
	if msg.ConsumerTag, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoWait = (bits&(1<<0) > 0)
	return
}

type basicCancelOk struct {
	ConsumerTag string
}

func (msg *basicCancelOk) id() (uint16, uint16) {
	return 60, 31
}

func (msg *basicCancelOk) wait() bool {
	return true
}

func (msg *basicCancelOk) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.ConsumerTag); err != nil {
		return
	}
	return
}

func (msg *basicCancelOk) read(r io.Reader) (err error) {
	if msg.ConsumerTag, err = readShortstr(r); err != nil {
		return
	}
	return
}

type basicPublish struct {
	reserved1  uint16
	Exchange   string
	RoutingKey string
	Mandatory  bool
	Immediate  bool
	Properties properties
	Body       []byte
}

func (msg *basicPublish) id() (uint16, uint16) {
	return 60, 40
}

func (msg *basicPublish) wait() bool {
	return false
}

func (msg *basicPublish) getContent() (properties, []byte) {
	return msg.Properties, msg.Body
}

func (msg *basicPublish) setContent(props properties, body []byte) {
	msg.Properties, msg.Body = props, body
}

func (msg *basicPublish) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if msg.Mandatory {
		bits |= 1 << 0
	}
	if msg.Immediate {
		bits |= 1 << 1
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicPublish) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Mandatory = (bits&(1<<0) > 0)
	msg.Immediate = (bits&(1<<1) > 0)
	return
}

type basicReturn struct {
	ReplyCode  uint16
	ReplyText  string
	Exchange   string
	RoutingKey string
	Properties properties
	Body       []byte
}

func (msg *basicReturn) id() (uint16, uint16) {
	return 60, 50
}

func (msg *basicReturn) wait() bool {
	return false
}

func (msg *basicReturn) getContent() (properties, []byte) {
	return msg.Properties, msg.Body
}

func (msg *basicReturn) setContent(props properties, body []byte) {
	msg.Properties, msg.Body = props, body
}

func (msg *basicReturn) write(w io.Writer) (err error) {
	if err = binary.Write(w, binary.BigEndian, msg.ReplyCode); err != nil {
		return
	}
	if err = writeShortstr(w, msg.ReplyText); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	return
}

func (msg *basicReturn) read(r io.Reader) (err error) {
	if err = binary.Read(r, binary.BigEndian, &msg.ReplyCode); err != nil {
		return
	}
	if msg.ReplyText, err = readShortstr(r); err != nil {
		return
	}
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	return
}

type basicDeliver struct {
	ConsumerTag string
	DeliveryTag uint64
	Redelivered bool
	Exchange    string
	RoutingKey  string
	Properties  properties
	Body        []byte
}

func (msg *basicDeliver) id() (uint16, uint16) {
	return 60, 60
}

func (msg *basicDeliver) wait() bool {
	return false
}

func (msg *basicDeliver) getContent() (properties, []byte) {
	return msg.Properties, msg.Body
}

func (msg *basicDeliver) setContent(props properties, body []byte) {
	msg.Properties, msg.Body = props, body
}

func (msg *basicDeliver) write(w io.Writer) (err error) {
	var bits byte
	if err = writeShortstr(w, msg.ConsumerTag); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.DeliveryTag); err != nil {
		return
	}
	if msg.Redelivered {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	return
}

func (msg *basicDeliver) read(r io.Reader) (err error) {
	var bits byte
	if msg.ConsumerTag, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.DeliveryTag); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Redelivered = (bits&(1<<0) > 0)
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	return
}

type basicGet struct {
	reserved1 uint16
	Queue     string
	NoAck     bool
}

func (msg *basicGet) id() (uint16, uint16) {
	return 60, 70
}

func (msg *basicGet) wait() bool {
	return true
}

func (msg *basicGet) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.reserved1); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Queue); err != nil {
		return
	}
	if msg.NoAck {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicGet) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.reserved1); err != nil {
		return
	}
	if msg.Queue, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.NoAck = (bits&(1<<0) > 0)
	return
}

type basicGetOk struct {
	DeliveryTag  uint64
	Redelivered  bool
	Exchange     string
	RoutingKey   string
	MessageCount uint32
	Properties   properties
	Body         []byte
}

func (msg *basicGetOk) id() (uint16, uint16) {
	return 60, 71
}

func (msg *basicGetOk) wait() bool {
	return true
}

func (msg *basicGetOk) getContent() (properties, []byte) {
	return msg.Properties, msg.Body
}

func (msg *basicGetOk) setContent(props properties, body []byte) {
	msg.Properties, msg.Body = props, body
}

func (msg *basicGetOk) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.DeliveryTag); err != nil {
		return
	}
	if msg.Redelivered {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Exchange); err != nil {
		return
	}
	if err = writeShortstr(w, msg.RoutingKey); err != nil {
		return
	}
	if err = binary.Write(w, binary.BigEndian, msg.MessageCount); err != nil {
		return
	}
	return
}

func (msg *basicGetOk) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.DeliveryTag); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Redelivered = (bits&(1<<0) > 0)
	if msg.Exchange, err = readShortstr(r); err != nil {
		return
	}
	if msg.RoutingKey, err = readShortstr(r); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &msg.MessageCount); err != nil {
		return
	}
	return
}

type basicGetEmpty struct {
	reserved1 string
}

func (msg *basicGetEmpty) id() (uint16, uint16) {
	return 60, 72
}

func (msg *basicGetEmpty) wait() bool {
	return true
}

func (msg *basicGetEmpty) write(w io.Writer) (err error) {
	if err = writeShortstr(w, msg.reserved1); err != nil {
		return
	}
	return
}

func (msg *basicGetEmpty) read(r io.Reader) (err error) {
	if msg.reserved1, err = readShortstr(r); err != nil {
		return
	}
	return
}

type basicAck struct {
	DeliveryTag uint64
	Multiple    bool
}

func (msg *basicAck) id() (uint16, uint16) {
	return 60, 80
}

func (msg *basicAck) wait() bool {
	return false
}

func (msg *basicAck) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.DeliveryTag); err != nil {
		return
	}
	if msg.Multiple {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicAck) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.DeliveryTag); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Multiple = (bits&(1<<0) > 0)
	return
}

type basicReject struct {
	DeliveryTag uint64
	Requeue     bool
}

func (msg *basicReject) id() (uint16, uint16) {
	return 60, 90
}

func (msg *basicReject) wait() bool {
	return false
}

func (msg *basicReject) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.DeliveryTag); err != nil {
		return
	}
	if msg.Requeue {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicReject) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.DeliveryTag); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Requeue = (bits&(1<<0) > 0)
	return
}

type basicRecoverAsync struct {
	Requeue bool
}

func (msg *basicRecoverAsync) id() (uint16, uint16) {
	return 60, 100
}

func (msg *basicRecoverAsync) wait() bool {
	return false
}

func (msg *basicRecoverAsync) write(w io.Writer) (err error) {
	var bits byte
	if msg.Requeue {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicRecoverAsync) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Requeue = (bits&(1<<0) > 0)
	return
}

type basicRecover struct {
	Requeue bool
}

func (msg *basicRecover) id() (uint16, uint16) {
	return 60, 110
}

func (msg *basicRecover) wait() bool {
	return true
}

func (msg *basicRecover) write(w io.Writer) (err error) {
	var bits byte
	if msg.Requeue {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicRecover) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Requeue = (bits&(1<<0) > 0)
	return
}

type basicRecoverOk struct{}

func (msg *basicRecoverOk) id() (uint16, uint16) {
	return 60, 111
}

func (msg *basicRecoverOk) wait() bool {
	return true
}

func (msg *basicRecoverOk) write(w io.Writer) (err error) {
	return
}

func (msg *basicRecoverOk) read(r io.Reader) (err error) {
	return
}

type basicNack struct {
	DeliveryTag uint64
	Multiple    bool
	Requeue     bool
}

func (msg *basicNack) id() (uint16, uint16) {
	return 60, 120
}

func (msg *basicNack) wait() bool {
	return false
}

func (msg *basicNack) write(w io.Writer) (err error) {
	var bits byte
	if err = binary.Write(w, binary.BigEndian, msg.DeliveryTag); err != nil {
		return
	}
	if msg.Multiple {
		bits |= 1 << 0
	}
	if msg.Requeue {
		bits |= 1 << 1
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *basicNack) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &msg.DeliveryTag); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Multiple = (bits&(1<<0) > 0)
	msg.Requeue = (bits&(1<<1) > 0)
	return
}

type txSelect struct {
}

func (msg *txSelect) id() (uint16, uint16) {
	return 90, 10
}

func (msg *txSelect) wait() bool {
	return true
}

func (msg *txSelect) write(w io.Writer) (err error) {
	return
}

func (msg *txSelect) read(r io.Reader) (err error) {
	return
}

type txSelectOk struct{}

func (msg *txSelectOk) id() (uint16, uint16) {
	return 90, 11
}

func (msg *txSelectOk) wait() bool {
	return true
}

func (msg *txSelectOk) write(w io.Writer) (err error) {
	return
}

func (msg *txSelectOk) read(r io.Reader) (err error) {
	return
}

type txCommit struct{}

func (msg *txCommit) id() (uint16, uint16) {
	return 90, 20
}

func (msg *txCommit) wait() bool {
	return true
}

func (msg *txCommit) write(w io.Writer) (err error) {
	return
}

func (msg *txCommit) read(r io.Reader) (err error) {
	return
}

type txCommitOk struct{}

func (msg *txCommitOk) id() (uint16, uint16) {
	return 90, 21
}

func (msg *txCommitOk) wait() bool {
	return true
}

func (msg *txCommitOk) write(w io.Writer) (err error) {
	return
}

func (msg *txCommitOk) read(r io.Reader) (err error) {
	return
}

type txRollback struct{}

func (msg *txRollback) id() (uint16, uint16) {
	return 90, 30
}

func (msg *txRollback) wait() bool {
	return true
}

func (msg *txRollback) write(w io.Writer) (err error) {
	return
}

func (msg *txRollback) read(r io.Reader) (err error) {
	return
}

type txRollbackOk struct{}

func (msg *txRollbackOk) id() (uint16, uint16) {
	return 90, 31
}

func (msg *txRollbackOk) wait() bool {
	return true
}

func (msg *txRollbackOk) write(w io.Writer) (err error) {
	return
}

func (msg *txRollbackOk) read(r io.Reader) (err error) {
	return
}

type confirmSelect struct {
	Nowait bool
}

func (msg *confirmSelect) id() (uint16, uint16) {
	return 85, 10
}

func (msg *confirmSelect) wait() bool {
	return true
}

func (msg *confirmSelect) write(w io.Writer) (err error) {
	var bits byte
	if msg.Nowait {
		bits |= 1 << 0
	}
	if err = binary.Write(w, binary.BigEndian, bits); err != nil {
		return
	}
	return
}

func (msg *confirmSelect) read(r io.Reader) (err error) {
	var bits byte
	if err = binary.Read(r, binary.BigEndian, &bits); err != nil {
		return
	}
	msg.Nowait = (bits&(1<<0) > 0)
	return
}

type confirmSelectOk struct{}

func (msg *confirmSelectOk) id() (uint16, uint16) {
	return 85, 11
}

func (msg *confirmSelectOk) wait() bool {
	return true
}

func (msg *confirmSelectOk) write(w io.Writer) (err error) {
	return
}

func (msg *confirmSelectOk) read(r io.Reader) (err error) {
	return
}

type reader struct {
	r io.Reader
}

type writer struct {
	w io.Writer
}

type Table map[string]interface{}

type connectionStartOk struct {
	ClientProperties Table
	Mechanism        string
	Response         string
	Locale           string
}

func (msg *connectionStartOk) id() (uint16, uint16) {
	return 10, 11
}

func (msg *connectionStartOk) read(r io.Reader) (err error) {
	if msg.ClientProperties, err = readTable(r); err != nil {
		return
	}
	if msg.Mechanism, err = readShortstr(r); err != nil {
		return
	}
	if msg.Response, err = readLongstr(r); err != nil {
		return
	}
	if msg.Locale, err = readShortstr(r); err != nil {
		return
	}
	return
}

func (msg *connectionStartOk) wait() bool {
	return true
}

func (msg *connectionStartOk) write(w io.Writer) (err error) {
	if err = writeTable(w, msg.ClientProperties); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Mechanism); err != nil {
		return
	}
	if err = writeLongstr(w, msg.Response); err != nil {
		return
	}
	if err = writeShortstr(w, msg.Locale); err != nil {
		return
	}
	return
}

func readTable(r io.Reader) (table Table, err error) {
	var nested bytes.Buffer
	var str string
	if str, err = readLongstr(r); err != nil {
		return
	}
	nested.Write([]byte(str))
	table = make(Table)
	for nested.Len() > 0 {
		var key string
		var value interface{}
		if key, err = readShortstr(&nested); err != nil {
			return
		}
		if value, err = readField(&nested); err != nil {
			return
		}
		table[key] = value
	}
	return
}

func readLongstr(r io.Reader) (v string, err error) {
	var length uint32
	if err = binary.Read(r, binary.BigEndian, &length); err != nil {
		return
	}
	// slices can't be longer than max int32 value
	if length > (^uint32(0) >> 1) {
		return
	}
	bytes := make([]byte, length)
	if _, err = io.ReadFull(r, bytes); err != nil {
		return
	}
	return string(bytes), nil
}

func readField(r io.Reader) (v interface{}, err error) {
	var typ byte
	if err = binary.Read(r, binary.BigEndian, &typ); err != nil {
		return
	}
	switch typ {
	case 't':
		var value uint8
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return (value != 0), nil
	case 'b':
		var value [1]byte
		if _, err = io.ReadFull(r, value[0:1]); err != nil {
			return
		}
		return value[0], nil
	case 's':
		var value int16
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return value, nil
	case 'I':
		var value int32
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return value, nil
	case 'l':
		var value int64
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return value, nil
	case 'f':
		var value float32
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return value, nil
	case 'd':
		var value float64
		if err = binary.Read(r, binary.BigEndian, &value); err != nil {
			return
		}
		return value, nil
	case 'D':
		return readDecimal(r)
	case 'S':
		return readLongstr(r)
	case 'A':
		return readArray(r)
	case 'T':
		return readTimestamp(r)
	case 'F':
		return readTable(r)
	case 'x':
		var len int32
		if err = binary.Read(r, binary.BigEndian, &len); err != nil {
			return nil, err
		}
		value := make([]byte, len)
		if _, err = io.ReadFull(r, value); err != nil {
			return nil, err
		}
		return value, err
	case 'V':
		return nil, nil
	}
	return nil, ErrSyntax
}

func readDecimal(r io.Reader) (v Decimal, err error) {
	if err = binary.Read(r, binary.BigEndian, &v.Scale); err != nil {
		return
	}
	if err = binary.Read(r, binary.BigEndian, &v.Value); err != nil {
		return
	}
	return
}

func writeTable(w io.Writer, table Table) (err error) {
	var buf bytes.Buffer
	for key, val := range table {
		if err = writeShortstr(&buf, key); err != nil {
			return
		}
		if err = writeField(&buf, val); err != nil {
			return
		}
	}
	return writeLongstr(w, buf.String())
}

func writeLongstr(w io.Writer, s string) (err error) {
	b := []byte(s)
	var length = uint32(len(b))
	if err = binary.Write(w, binary.BigEndian, length); err != nil {
		return
	}
	if _, err = w.Write(b[:length]); err != nil {
		return
	}
	return
}

func writeFrame(w io.Writer, typ uint8, channel uint16, payload []byte) (err error) {
	end := []byte{frameEnd}
	size := uint(len(payload))
	_, err = w.Write([]byte{
		byte(typ),
		byte((channel & 0xff00) >> 8),
		byte((channel & 0x00ff) >> 0),
		byte((size & 0xff000000) >> 24),
		byte((size & 0x00ff0000) >> 16),
		byte((size & 0x0000ff00) >> 8),
		byte((size & 0x000000ff) >> 0),
	})
	if err != nil {
		return
	}
	if _, err = w.Write(payload); err != nil {
		return
	}
	if _, err = w.Write(end); err != nil {
		return
	}
	return
}

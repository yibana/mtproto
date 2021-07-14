package transport

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/xelaj/go-dry/ioutil"
	"golang.org/x/net/proxy"
	"net/url"
)

type tcpConn struct {
	cancelReader *ioutil.CancelableReader
	conn         *net.TCPConn
	timeout      time.Duration
}

type TCPConnConfig struct {
	Ctx     context.Context
	Host    string
	Timeout time.Duration
	ProxyUrl string
}

func NewTCP(cfg TCPConnConfig) (Conn, error) {

	d := net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 15 * time.Second,
		Cancel: cfg.Ctx.Done(),
	}
	var conn net.Conn
	var err error
	if len(cfg.ProxyUrl)>0{
		u,err:=url.Parse(cfg.ProxyUrl)
		if err!=nil {
			return nil, err
		}
		dialer, err :=proxy.FromURL(u,&d)
		if err!=nil {
			return nil, err
		}
		conn, err =dialer.Dial("tcp",cfg.Host)
	} else {
		tcpAddr, err := net.ResolveTCPAddr("tcp", cfg.Host)
		if err != nil {
			return nil, errors.Wrap(err, "resolving tcp")
		}
		conn, err = net.DialTCP("tcp", nil, tcpAddr)
	}

	//
	if err != nil {
		return nil, errors.Wrap(err, "dialing tcp")
	}

	if conn==nil{
		return nil, errors.Wrap(err,"conn fail")
	}

	return &tcpConn{
		cancelReader: ioutil.NewCancelableReader(cfg.Ctx, conn),
		conn:         conn.(*net.TCPConn),
		timeout:      cfg.Timeout,
	}, nil
}

func (t *tcpConn) Close() error {
	return t.conn.Close()
}

func (t *tcpConn) Write(b []byte) (int, error) {
	return t.conn.Write(b)
}

func (t *tcpConn) Read(b []byte) (int, error) {
	if t.timeout > 0 {
		err := t.conn.SetReadDeadline(time.Now().Add(t.timeout))
		check(err)
	}

	n, err := t.cancelReader.Read(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok {
			if e.Err.Error() == "i/o timeout" {
				// timeout? no worries, but we must reconnect tcp connection
				return 0, errors.Wrap(err, "required to reconnect!")
			}
		}
		switch err {
		case io.EOF, context.Canceled:
			return 0, err
		default:
			return 0, errors.Wrap(err, "unexpected error")
		}
	}
	return n, nil
}

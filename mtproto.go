// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/xelaj/mtproto/blob/master/LICENSE for details

package mtproto

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"
	//"fmt"
	"github.com/k0kubun/pp"
	"github.com/pkg/errors"
	"github.com/xelaj/errs"

	"github.com/xelaj/mtproto/internal/encoding/tl"
	"github.com/xelaj/mtproto/internal/mode"
	"github.com/xelaj/mtproto/internal/mtproto/messages"
	"github.com/xelaj/mtproto/internal/mtproto/objects"
	"github.com/xelaj/mtproto/internal/session"
	"github.com/xelaj/mtproto/internal/transport"
	"github.com/xelaj/mtproto/internal/utils"
)

type MTProto struct {
	addr         string
	ProxyUrl 	 string
	transport    transport.Transport
	stopRoutines context.CancelFunc // stopping ping, read, etc. routines
	routineswg   sync.WaitGroup     // WaitGroup for being sure that all routines are stopped
	Reconnecting	bool

	// ключ авторизации. изменять можно только через setAuthKey
	authKey []byte

	// хеш ключа авторизации. изменять можно только через setAuthKey
	authKeyHash []byte

	// соль сессии
	serverSalt int64
	encrypted  bool
	sessionId  int64

	// общий мьютекс
	mutex sync.Mutex

	// каналы, которые ожидают ответа rpc. ответ записывается в канал и удаляется
	responseChannels *utils.SyncIntObjectChan
	expectedTypes    *utils.SyncIntReflectTypes // uses for parcing bool values in rpc result for example

	// идентификаторы сообщений, нужны что бы посылать и принимать сообщения.
	seqNoMutex sync.Mutex
	seqNo      int32
	msgId	   int64

	// айдишники DC для КОНКРЕТНОГО Приложения и клиента. Может меняться, но фиксирована для
	// связки приложение+клиент
	dclist map[int]string

	// storage of session for this instance
	tokensStorage session.SessionLoader

	// один из публичных ключей telegram. нужен только для создания сессии.
	publicKey *rsa.PublicKey

	// serviceChannel нужен только на время создания ключей, т.к. это
	// не RpcResult, поэтому все данные отдаются в один поток без
	// привязки к MsgID
	serviceChannel       chan tl.Object
	serviceModeActivated bool

	//! DEPRECATED RecoverFunc используется только до того момента, когда из пакета будут убраны все паники
	RecoverFunc func(i any)
	// if set, all critical errors writing to this channel
	Warnings chan error

	serverRequestHandlers []customHandlerFunc
}

type customHandlerFunc = func(i any) bool

type Config struct {
	AuthKeyFile string //! DEPRECATED // use SessionStorage

	// if SessionStorage is nil, AuthKeyFile is required, otherwise it will be ignored
	SessionStorage session.SessionLoader

	ServerHost string
	PublicKey  *rsa.PublicKey
	ProxyUrl string
}

func NewMTProto(c Config) (*MTProto, error) {
	if c.SessionStorage == nil {
		if c.AuthKeyFile == "" {
			return nil, errors.New("AuthKeyFile is empty") //nolint:golint // its a field name, makes no sense
		}

		c.SessionStorage = session.NewFromFile(c.AuthKeyFile)
	}

	s, err := c.SessionStorage.Load()
	switch {
	case err == nil, errs.IsNotFound(err):
	default:
		return nil, errors.Wrap(err, "loading session")
	}

	m := &MTProto{
		tokensStorage:         c.SessionStorage,
		addr:                  c.ServerHost,
		encrypted:             s != nil, // if not nil, then it's already encrypted, otherwise makes no sense
		sessionId:             utils.GenerateSessionID(),
		serviceChannel:        make(chan tl.Object),
		publicKey:             c.PublicKey,
		responseChannels:      utils.NewSyncIntObjectChan(),
		expectedTypes:         utils.NewSyncIntReflectTypes(),
		serverRequestHandlers: make([]customHandlerFunc, 0),
		dclist:                defaultDCList(),
		ProxyUrl: c.ProxyUrl,
	}

	if s != nil {
		m.LoadSession(s)
	}

	return m, nil
}

func (m *MTProto) SetDCList(in map[int]string) {
	if m.dclist == nil {
		m.dclist = make(map[int]string)
	}
	for k, v := range in {
		m.dclist[k] = v
	}
}

func (m *MTProto) CreateConnection() error {
	ctx, cancelfunc := context.WithCancel(context.Background())
	m.stopRoutines = cancelfunc
	err := m.connect(ctx)
	if err != nil {
		return err
	}
	fmt.Println("链接服务器成功!")
	// start reading responses from the server
	m.startReadingResponses(ctx)

	// get new authKey if need
	if !m.encrypted {
		err = m.makeAuthKey()
		if err != nil {
			return errors.Wrap(err, "making auth key")
		}
	}

	// start keepalive pinging
	m.startPinging(ctx)

	return nil
}

const defaultTimeout = 65 * time.Second // 60 seconds is maximum timeouts without pings

func (m *MTProto) connect(ctx context.Context) error {
	var err error
	transport, err := transport.NewTransport(
		m,
		transport.TCPConnConfig{
			Ctx:     ctx,
			Host:    m.addr,
			Timeout: defaultTimeout,
			ProxyUrl: m.ProxyUrl,
		},
		mode.Intermediate,
	)
	if err != nil {
		return errors.Wrap(err, "can't connect")
	}
	m.transport = transport

	CloseOnCancel(ctx, m.transport)
	return nil
}

func (m *MTProto) makeRequest(data tl.Object, expectedTypes ...reflect.Type) (any, error) {
	resp, err := m.sendPacket(data, expectedTypes...)
	//fmt.Printf("m seqNo:%d MSgid:%d\n",m.seqNo,m.msgId)
	if err != nil {
		if strings.Contains(err.Error(),"use of closed network connection") {
			// 如果链接断开
			go func() {
				m.Reconnect()
			}()
		}
		return nil, errors.Wrap(err, "sending message")
	}

	//等待服务器返回数据,设置一个超时
	select {
	case response := <-resp: //拿到锁
		switch r := response.(type) {
		case *objects.RpcError:
			realErr := RpcErrorToNative(r)
			//fmt.Println(r.ErrorMessage)
			err = m.tryToProcessErr(realErr.(*ErrResponseCode))
			if err != nil {
				return nil, err
			}

			return m.makeRequest(data, expectedTypes...)

		case *errorSessionConfigsChanged:
			fmt.Println("errorSessionConfigsChanged")
			return m.makeRequest(data, expectedTypes...)
		case *BadMsgError:
			return nil, errors.New("Reconnect")

		}

		return tl.UnwrapNativeTypes(response), nil
	case <-time.After(60 * time.Second): //超时60s
		return nil, errors.New("makeRequest timeout")
	}

	return nil, nil
}

// Disconnect is closing current TCP connection and stopping all routines like pinging, reading etc.
func (m *MTProto) Disconnect() error {
	// stop all routines
	m.stopRoutines()

	// TODO: close ALL CHANNELS

	return nil
}

func (m *MTProto) Reconnect() error {
	if m.Reconnecting{
		return nil
	}
	m.Reconnecting = true
	defer func() {
		m.Reconnecting = false
	}()
	err := m.Disconnect()
	if err != nil {
		return errors.Wrap(err, "disconnecting")
	}
	//fmt.Println("Reconnect m.mutex.Lock()")
	//m.mutex.Lock()
	//for _, k := range m.responseChannels.Keys() {
	//	v, _ := m.responseChannels.Get(k)
	//	if cap(v)==0{
	//		v <- &BadMsgError{}
	//	}
	//	//m.responseChannels.Delete(k)
	//}
	//m.mutex.Unlock()
	//fmt.Println("Reconnect m.mutex.Unlock()")
	m.routineswg.Wait()
	err = m.CreateConnection()
	return errors.Wrap(err, "recreating connection")
}

// startPinging pings the server that everything is fine, the client is online
// you just need to run and forget about it
func (m *MTProto) startPinging(ctx context.Context) {
	m.routineswg.Add(1)

	go func() {
		ticker := time.NewTicker(time.Minute)
		fmt.Println("ping start")
		defer ticker.Stop()
		defer m.routineswg.Done()
		defer func() {
			fmt.Println("startPinging is exit!")
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				//_, err := m.ping(0xCADACADA) //nolint:gomnd not magic
				_,err:=m.ping_delay_disconnect(time.Now().UnixNano(),75)//保持链接
				if err != nil {
					fmt.Println("ping unsuccsesful")
					go func() {
						err = m.Reconnect()
						if err != nil {
							m.warnError(errors.Wrap(err, "can't reconnect"))
						}
					}()
					return
				}else {
					fmt.Println("ping succsesful")
				}
			}
		}
	}()
}

func (m *MTProto) startReadingResponses(ctx context.Context) {
	m.routineswg.Add(1)
	go func() {
		defer m.routineswg.Done()
		defer func() {
			fmt.Println("startReadingResponses is exit!")
		}()
		fmt.Println("startReadingResponses start")
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err := m.readMsg()
				switch err {
				case nil: // skip
				case context.Canceled:
					return
				case io.EOF:
					fmt.Println("ioEOF")
					go func() {
						err = m.Reconnect()
						if err != nil {
							m.warnError(errors.Wrap(err, "can't reconnect"))
						}
					}()
					return
				default:
					if strings.Contains(err.Error(),"i/o timeout"){
						fmt.Println(err.Error())
						go func() {
							err = m.Reconnect()
							if err != nil {
								m.warnError(errors.Wrap(err, "can't reconnect"))
							}
						}()
						return
						//err = m.Reconnect()
						//if err != nil {
						//	m.warnError(errors.Wrap(err, "can't reconnect"))
						//}else {
						//	fmt.Println("timeout重新链接成功")
						//	continue
						//}

					}
					check(err)
				}
			}
		}
	}()
}

func (m *MTProto) readMsg() error {
	if m.transport == nil {
		return errors.New("must setup connection before reading messages")
	}

	//fmt.Println("m.transport.ReadMsg() start")
	response, err := m.transport.ReadMsg()
	//fmt.Println("m.transport.ReadMsg() end")
	if err != nil {
		if e, ok := err.(transport.ErrCode); ok {
			return &ErrResponseCode{Code: int(e)}
		}
		switch err {
		case io.EOF, context.Canceled:
			return err
		default:
			return errors.Wrap(err, "reading message")
		}
	}

	if m.serviceModeActivated {
		var obj tl.Object
		// сервисные сообщения ГАРАНТИРОВАННО в теле содержат TL.
		obj, err = tl.DecodeUnknownObject(response.GetMsg())
		if err != nil {
			return errors.Wrap(err, "parsing object")
		}
		m.serviceChannel <- obj
		return nil
	}
	//fmt.Println(response.GetSeqNo())
	//fmt.Println("processResponse start")
	err = m.processResponse(response)
	//fmt.Println("processResponse end")
	if err != nil {
		return errors.Wrap(err, "processing response")
	}
	return nil
}

func (m *MTProto) processResponse(msg messages.Common) error {
	var data tl.Object
	var err error
	if et, ok := m.expectedTypes.Get(msg.GetMsgID()); ok && len(et) > 0 {
		data, err = tl.DecodeUnknownObject(msg.GetMsg(), et...)
	} else {
		data, err = tl.DecodeUnknownObject(msg.GetMsg())
	}
	if err != nil {
		return errors.Wrap(err, "unmarshaling response")
	}

messageTypeSwitching:
	switch message := data.(type) {
	case *objects.MessageContainer:
		for _, v := range *message {
			err := m.processResponse(v)
			if err != nil {
				return errors.Wrap(err, "processing item in container")
			}
		}

	case *objects.BadServerSalt:
		m.serverSalt = message.NewSalt
		err := m.SaveSession()
		check(err)

		//fmt.Println("BadServerSalt m.mutex.Lock()")
		//m.mutex.Lock()
		//for _, k := range m.responseChannels.Keys() {
		//	v, _ := m.responseChannels.Get(k)
		//	v <- &errorSessionConfigsChanged{}
		//}
		//m.mutex.Unlock()
		//fmt.Println("BadServerSalt m.mutex.Unlock()")

	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		err := m.SaveSession()
		fmt.Println("objects.NewSessionCreated")
		if err != nil {
			m.warnError(errors.Wrap(err, "saving session"))
		}

	case *objects.Pong:
		// игнорим, пришло и пришло, че бубнить то
		m.writeRPCResponse(int(message.MsgID), message)
		fmt.Println("PingID",message.PingID)

	case  *objects.MsgsAck:
		fmt.Println("objects.MsgsAck")

	case *objects.BadMsgNotification:
		pp.Println(message)
		panic(message) // for debug, looks like this message is important
		return BadMsgErrorFromNative(message)

	case *objects.RpcResult:
		obj := message.Obj
		if v, ok := obj.(*objects.GzipPacked); ok {
			obj = v.Obj
		}

		err := m.writeRPCResponse(int(message.ReqMsgID), obj)
		if err != nil {
			return errors.Wrap(err, "writing RPC response")
		}

	case *objects.GzipPacked:
		// sometimes telegram server returns gzip for unknown reason. so, we are extracting data from gzip and
		// reprocess it again
		data = message.Obj
		goto messageTypeSwitching

	default:
		processed := false
		for _, f := range m.serverRequestHandlers {
			processed = f(message)
			if processed {
				break
			}
		}
		if !processed {
			m.warnError(errors.New("got nonsystem message from server: " + reflect.TypeOf(message).String()))
		}
	}

	if (msg.GetSeqNo() & 1) != 0 {
		_, err := m.MakeRequest(&objects.MsgsAck{MsgIDs: []int64{int64(msg.GetMsgID())}})
		if err != nil {
			return errors.Wrap(err, "sending ack")
		}
	}

	return nil
}

// tryToProcessErr пытается автоматически решить ошибку полученную от сервера. в случае успеха вернет nil,
// в случае если нет способа решить эту проблему, возвращается сама ошибка
// если в процессе решения появлиась еще одна ошибка, то она оборачивается в errors.Wrap, основная
// игнорируется (потому что гарантируется, что обработка ошибки надежна, и параллельная ошибка это что-то из
// ряда вон выходящее)
func (m *MTProto) tryToProcessErr(e *ErrResponseCode) error {
	switch e.Message {
	case "PHONE_MIGRATE_X":
		newIP, found := m.dclist[e.AdditionalInfo.(int)]
		if !found {
			return errors.Wrapf(e, "DC with id %v not found", e.AdditionalInfo)
		}

		err := m.Disconnect()
		if err != nil {
			return errors.Wrap(err, "disconnecting")
		}
		m.routineswg.Wait()
		m.addr = newIP
		m.encrypted = false
		m.sessionId = utils.GenerateSessionID()
		m.serviceChannel = make(chan tl.Object)
		m.serviceModeActivated = false
		m.responseChannels = utils.NewSyncIntObjectChan()
		m.expectedTypes = utils.NewSyncIntReflectTypes()
		m.seqNo = 0

		err = m.CreateConnection()
		if err != nil {
			return errors.Wrap(err, "recreating connection")
		}
		return errors.New("PHONE_MIGRATE_X_NewIP") // 返回一个重定向错误
	case "FILE_MIGRATE_X":
		return e
	default:
		return e
	}
}

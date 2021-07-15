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
	transport    map[int64]*transport.Transport
	ConnId int64
	stopRoutines map[int64] context.CancelFunc// stopping ping, read, etc. routines
	routineswg   sync.WaitGroup     // WaitGroup for being sure that all routines are stopped
	Reconnecting	bool
	Ctx context.Context
	Cancelfunc context.CancelFunc

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

func (m *MTProto) Close()  {
	//关闭
	if m.Cancelfunc != nil {
		m.Cancelfunc()
	}
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
	connId,err := m.connect(ctx)
	if err != nil {
		return err
	}
	if m.stopRoutines==nil{
		m.stopRoutines = make(map[int64]context.CancelFunc)
	}
	m.stopRoutines[connId] = cancelfunc

	fmt.Println(connId,"链接服务器成功!")
	// start reading responses from the server
	m.StartReadingResponses(ctx,connId)

	// get new authKey if need
	if !m.encrypted {
		err = m.makeAuthKey()
		if err != nil {
			return errors.Wrap(err, "making auth key")
		}
	}

	// start keepalive pinging
	m.StartPinging(ctx,connId)

	return nil
}

const defaultTimeout = 65 * time.Second // 60 seconds is maximum timeouts without pings

func (m *MTProto) connect(ctx context.Context) (int64,error) {
	var err error
	tran, err := transport.NewTransport(
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
		return 0,errors.Wrap(err, "can't connect")
	}
	m.ConnId=time.Now().UnixNano()
	if m.transport == nil {
		m.transport = make(map[int64]*transport.Transport)
	}
	m.transport[m.ConnId] = &tran

	CloseOnCancel(ctx, *m.transport[m.ConnId])
	return m.ConnId,nil
}

func (m *MTProto) makeRequest(data tl.Object,conID int64, expectedTypes ...reflect.Type) (any, error) {
	resp, err := m.sendPacket(data,conID, expectedTypes...)
	//fmt.Printf("m seqNo:%d MSgid:%d\n",m.seqNo,m.msgId)
	if err != nil {
		if strings.Contains(err.Error(),"use of closed network connection") || strings.Contains(err.Error(),"connection was aborted"){
			// 如果链接断开
			//err:=m.Reconnect()
			//if err != nil {
			//	return nil, errors.Wrap(err, "makeRequest Reconnect")
			//}

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

			return m.makeRequest(data,conID, expectedTypes...)

		case *errorSessionConfigsChanged:
			fmt.Println("errorSessionConfigsChanged")
			return m.makeRequest(data,conID, expectedTypes...)
		case *BadMsgError:
			return nil, errors.New("Reconnect")
		case *objects.BadServerSalt:
			err:=m.Reconnect()
			if err != nil {
				return nil, errors.New("BadServerSalt")
			}
			return m.makeRequest(data, conID,expectedTypes...)

		}

		return tl.UnwrapNativeTypes(response), nil
	case <-time.After(60 * time.Second): //超时60s
		return nil, errors.New("makeRequest waiting timeout")
	}

	return nil, nil
}

// Disconnect is closing current TCP connection and stopping all routines like pinging, reading etc.
func (m *MTProto) Disconnect(ConnId int64) error {
	// stop all routines
	m.stopRoutines[ConnId]()

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
	err := m.Disconnect(m.ConnId)
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
	//m.routineswg.Wait()
	err = m.CreateConnection()
	return errors.Wrap(err, "recreating connection")
}

// StartPinging pings the server that everything is fine, the client is online
// you just need to run and forget about it
func (m *MTProto) StartPinging(ctx context.Context,connId int64) {
	m.routineswg.Add(1)

	go func() {
		ticker := time.NewTicker(time.Minute)
		fmt.Println(connId,"ping start")
		defer ticker.Stop()
		defer m.routineswg.Done()
		defer func() {
			fmt.Println(connId,"StartPinging is exit!")
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
					//go func() {
					//	err = m.Reconnect()
					//	if err != nil {
					//		m.warnError(errors.Wrap(err, "can't reconnect"))
					//	}
					//}()
					//return
				}else {
					fmt.Println("ping succsesful")
				}
			}
		}
	}()
}

func (m *MTProto) StartReadingResponses(ctx context.Context,connId int64) {
	m.routineswg.Add(1)
	go func() {
		defer m.routineswg.Done()
		defer func() {
			fmt.Println(connId,"StartReadingResponses is exit!")
		}()
		fmt.Println(connId,"StartReadingResponses start")
		for {
			select {
			case <-ctx.Done():
				return
			default:
				err:=m.readMsg(connId)
				if err != nil {
					time.Sleep(time.Millisecond*100)
				}
				//switch err {
				//case nil: // skip
				//case context.Canceled:
				//	return
				//case io.EOF:
				//	fmt.Println("ioEOF")
				//	go func() {
				//		err = m.Reconnect()
				//		if err != nil {
				//			m.warnError(errors.Wrap(err, "can't reconnect"))
				//		}
				//	}()
				//	return
				//default:
				//	if strings.Contains(err.Error(),"i/o timeout"){
				//		fmt.Println(err.Error())
				//		go func() {
				//			err = m.Reconnect()
				//			if err != nil {
				//				m.warnError(errors.Wrap(err, "can't reconnect"))
				//			}
				//		}()
				//		return
				//		//err = m.Reconnect()
				//		//if err != nil {
				//		//	m.warnError(errors.Wrap(err, "can't reconnect"))
				//		//}else {
				//		//	fmt.Println("timeout重新链接成功")
				//		//	continue
				//		//}
				//
				//	}
				//	check(err)
				//}
			}
		}
	}()
}

func (m *MTProto) readMsg(connId int64) error {
	if m.transport == nil {
		return errors.New("must setup connection before reading messages")
	}
	//m.routineswg.Add(1)
	//fmt.Println("m.transport.ReadMsg() start")
	response, err := (*m.transport[connId]).ReadMsg()
	//m.routineswg.Done()
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
	fmt.Printf("%d 收到msgID:%d\n",connId,response.GetMsgID())

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
		m.DebugPrintf("解码消息:%d 类型:BadServerSalt Msgid:%d\n",message.BadMsgID,msg.GetMsgID())
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
		m.writeRPCResponse(int(message.BadMsgID), message)


	case *objects.NewSessionCreated:
		m.serverSalt = message.ServerSalt
		err := m.SaveSession()
		m.DebugPrintf("解码消息:%d 类型:NewSessionCreated Msgid:%d\n",message.FirstMsgID,msg.GetMsgID())
		if err != nil {
			m.warnError(errors.Wrap(err, "saving session"))
		}

	case *objects.Pong:
		// игнорим, пришло и пришло, че бубнить то
		m.writeRPCResponse(int(message.MsgID), message)
		m.DebugPrintf("解码消息:%d 类型:Pong Msgid:%d\n",message.MsgID,msg.GetMsgID())

	case  *objects.MsgsAck:
		m.DebugPrintf("解码消息:%v 类型:MsgsAck Msgid:%d\n",message.MsgIDs,msg.GetMsgID())

	case *objects.BadMsgNotification:
		m.DebugPrintf("解码消息:%d 类型:BadMsgNotification Msgid:%d\n",message.BadMsgID,msg.GetMsgID())
		panic(message) // for debug, looks like this message is important
		return BadMsgErrorFromNative(message)

	case *objects.RpcResult:
		m.DebugPrintf("解码消息:%d 类型:RpcResult Msgid:%d\n",message.ReqMsgID,msg.GetMsgID())
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

		err := m.Disconnect(m.ConnId)
		if err != nil {
			return errors.Wrap(err, "disconnecting")
		}
		//time.Sleep(time.Second)
		m.routineswg.Wait()
		m.addr = newIP
		m.encrypted = false
		//m.sessionId = utils.GenerateSessionID()
		//m.serviceChannel = make(chan tl.Object)
		m.serviceModeActivated = false
		//m.responseChannels = utils.NewSyncIntObjectChan()
		//m.expectedTypes = utils.NewSyncIntReflectTypes()
		//m.seqNo = 0

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

func (m *MTProto) DebugPrintf(format string, a ...interface{}) (n int, err error) {
	return fmt.Printf(format,a...)
}
package main

import (
	"bufio"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/k0kubun/pp"
	"github.com/pkg/errors"
	"github.com/xelaj/go-dry"
	"github.com/xelaj/mtproto/telegram"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	utils "github.com/xelaj/mtproto/examples/example_utils"
)
const AppID = 4995960
const AppHash = "ca928547b9035ac7a4219a4441883f64"

func main() {
	println("firstly, you need to authorize. after example 'auth', you will sign in")
	phoneNumber:="+77474357863"
	// helper variables
	appStorage := utils.PrepareAppStorageForExamples()
	sessionFile := filepath.Join(appStorage, fmt.Sprintf("%s_Session.json",strings.ReplaceAll(phoneNumber,"+","")))
	publicKeys := filepath.Join(appStorage, "tg_public_keys.pem")

	// edit these params for you!
	client, err := telegram.NewClient(telegram.ClientConfig{
		// where to store session configuration. must be set
		SessionFile: sessionFile,
		// host address of mtproto server. Actually, it can be any mtproxy, not only official
		ServerHost: "149.154.167.50:443",
		// public keys file is path to file with public keys, which you must get from https://my.telelgram.org
		PublicKeysFile:  publicKeys,
		AppID:           AppID,                              // app id, could be find at https://my.telegram.org
		AppHash:         AppHash, // app hash, could be find at https://my.telegram.org
		InitWarnChannel: true,                               // if we want to get errors, otherwise, client.Warnings will be set nil
		ProxyUrl: "socks5://xiaotianwm_1011:xiaotian@gate5.rola-ip.co:2137",
	})
	dry.PanicIfErr(err)
	client.Warnings = make(chan error) // required to initialize, if we want to get errors
	utils.ReadWarningsToStdErr(client.Warnings)
	signedIn, err := client.IsSessionRegistred()
	if err != nil {
		panic(errors.Wrap(err, "can't check that session is registred"))
	}

	if signedIn {
		println("You've already signed in!")
	}else {
		println("singedOUT!!!")
		os.Exit(0)
	}
	client.AddCustomServerRequestHandler(func(i interface{}) bool {
		pp.Println(i)

		switch i.(type) {
		case *telegram.UpdateShort:
			return true
		}
		return false
	})
	go func() {
		for{
			//fmt.Println("MessagesGetDialogs start")
			TGPrintlResult(client.MessagesGetDialogs(&telegram.MessagesGetDialogsParams{OffsetPeer: telegram.InputPeer(&telegram.InputPeerEmpty{})}))
			//fmt.Println("MessagesGetDialogs end")
			time.Sleep(time.Minute)
		}
	}()
	defaulPeer :=telegram.InputPeer(&telegram.InputPeerUser{UserID: 962210352,AccessHash: -1498843611134224383})//dreamzza
	for{
		choise :=rawinput("选择操作")
		switch choise {
		case "群成员获取":
			hash := "txtdx"

			//搜索群组
			resolved, err := client.ContactsResolveUsername(hash)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}

			channel := resolved.Chats[0].(*telegram.Channel)
			fmt.Println(channel.ID)
			inCh := telegram.InputChannel(&telegram.InputChannelObj{
				ChannelID:  channel.ID,
				AccessHash: channel.AccessHash,
			})
			//total, err := client.GetPossibleAllParticipantsOfGroup(&telegram.InputChannelObj{
			//	ChannelID:  channel.ID,
			//	AccessHash: channel.AccessHash,
			//})
			////
			//dry.PanicIfErr(err)
			//pp.Println(total, len(total))
			//msg,err:=client.ChannelsGetFullChannel(inCh)
			//
			//fmt.Println(msg)
			res := make(map[int]struct{})
			totalCount := 10 // at least 100
			offset := 0
			for offset < totalCount {
				resp, err := client.ChannelsGetParticipants(
					inCh,
					telegram.ChannelParticipantsFilter(&telegram.ChannelParticipantsRecent{}),
					int32(offset),
					100,
					0,
				)
				if err != nil {
					fmt.Println(err.Error())
					continue
				}
				data := resp.(*telegram.ChannelsChannelParticipantsObj)
				totalCount = int(data.Count)
				for _, participant := range data.Participants {
					switch user := participant.(type) {
					case *telegram.ChannelParticipantSelf:
						res[int(user.UserID)] = struct{}{}
					case *telegram.ChannelParticipantObj:
						res[int(user.UserID)] = struct{}{}
					case *telegram.ChannelParticipantAdmin:
						res[int(user.UserID)] = struct{}{}
					case *telegram.ChannelParticipantCreator:
						res[int(user.UserID)] = struct{}{}
					default:
						pp.Println(user)
						panic("что?")
					}
				}

				offset += 100
				pp.Println(offset, totalCount)
			}
		case "二维码授权登录":
			//tg://login?token=AQKFwOlgB5GdexZlvoUd7FcvAXgn_MPrwLWmV4i6nj224Q
			tokens := rawinput("token")
			token,err:=base64.RawURLEncoding.DecodeString(tokens)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			auth,err:=client.AuthAcceptLoginToken(token)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			pp.Println(auth)
		case "获取频道消息":
			msgs,err:=client.MessagesGetAllChats([]int32{})
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			fmt.Println(msgs)
		case "获取指定聊天记录":
			username :=rawinput("username")
			cont,err:=client.ContactsResolveUsername(username)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			fmt.Println(cont)
			tgUserobj :=cont.Users[0].(*telegram.UserObj)
			TGPrintlResult(client.MessagesGetCommonChats(telegram.InputUser(&telegram.InputUserObj{UserID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),100,100))
		case "获取对话框列表":
			TGPrintlResult(client.MessagesGetDialogs(&telegram.MessagesGetDialogsParams{OffsetPeer: telegram.InputPeer(&telegram.InputPeerEmpty{})}))
		case "查看指定对话框":
			TGPrintlResult(client.MessagesGetPeerDialogs([]telegram.InputDialogPeer{telegram.InputDialogPeer(&telegram.InputDialogPeerObj{Peer: defaulPeer})}))

			client.MessagesReadHistory(defaulPeer,16)// 标记已读
		case "发送文本消息":
			username:=rawinput("username")
			cont,err:=client.ContactsResolveUsername(username)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			if len(cont.Users)>0{
				sendContent:=rawinput("发送内容")
				tgUserobj :=cont.Users[0].(*telegram.UserObj)
				TGPrintlResult(client.MessagesSendMessage(&telegram.MessagesSendMessageParams{Peer: telegram.InputPeer(&telegram.InputPeerUser{UserID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),
					Message: sendContent,RandomID: time.Now().UnixNano()}))
			}else {
				sendContent:=rawinput("发送内容")
				tgUserobj :=cont.Chats[0].(*telegram.Channel)
				TGPrintlResult(client.MessagesSendMessage(&telegram.MessagesSendMessageParams{Peer: telegram.InputPeer(&telegram.InputPeerChannel{ChannelID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),
					Message: sendContent,RandomID: time.Now().UnixNano()}))

			}
		case "查看群对话框":
			hash := "hao123fuck"
			//搜索群组
			resolved, err := client.ContactsResolveUsername(hash)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}

			channel := resolved.Chats[0].(*telegram.Channel)
			TGPrintlResult(client.MessagesGetPeerDialogs([]telegram.InputDialogPeer{telegram.InputDialogPeer(&telegram.InputDialogPeerObj{Peer: &telegram.InputPeerChannel{ChannelID: channel.ID,AccessHash: channel.AccessHash}})}))
		case "获取历史消息":
			hash := "txtdx"
			//搜索群组
			resolved, err := client.ContactsResolveUsername(hash)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}

			channel := resolved.Chats[0].(*telegram.Channel)
			fmt.Println(channel)
			TGPrintlResult(client.MessagesGetHistory(&telegram.MessagesGetHistoryParams{Peer: getChannelPeer(channel),Limit: 10,Hash: 0}))

		case "发送带链接消息":
			username :="dreamzza"
			cont,err:=client.ContactsResolveUsername(username)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			tgUserobj :=cont.Users[0].(*telegram.UserObj)
			ScheduleDate:=int32(time.Now().Unix()+30)

			TGPrintlResult(client.MessagesSendMessage(&telegram.MessagesSendMessageParams{Peer: telegram.InputPeer(&telegram.InputPeerUser{UserID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),NoWebpage: true,
				Message: "你好啊!看看?这是一条定时消息,不包含网页预览,点击你好啊可以打开网页",RandomID: time.Now().UnixNano(),Entities: []telegram.MessageEntity{telegram.MessageEntity(&telegram.MessageEntityTextURL{0,4,"https://baidu.com"})},
			ScheduleDate: ScheduleDate}))

		case "上传资源":
			img,err:=ioutil.ReadFile(`C:\Users\Administrator\Pictures\1.jpg`)
			//Md5Checksum := getMd5String(img)
			dry.PanicIfErr(err)
			fileid:=time.Now().UnixNano()
			filep:=0
			for filep = 0; filep < len(img)/524288; filep++ {
				updata:=img[:524288]
				upb,err:=client.UploadSaveFilePart(fileid, int32(filep),updata)
				dry.PanicIfErr(err)
				fmt.Println(upb)
				img = img[524288:]
			}
			if len(img)>0{
				upb,err:=client.UploadSaveFilePart(fileid, int32(filep),img)
				dry.PanicIfErr(err)
				fmt.Println(upb)
				filep++
			}

			TGPrintlResult(client.MessagesUploadMedia(defaulPeer,telegram.InputMedia(&telegram.InputMediaUploadedPhoto{File: telegram.InputFile(&telegram.InputFileObj{ID: fileid,Parts: int32(filep),Name: "美女哦.jpg"})})))
		case "发送图片":
			username :="zzkkccy"
			cont,err:=client.ContactsResolveUsername(username)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}
			tgUserobj :=cont.Users[0].(*telegram.UserObj)
			FileReference,_:=base64.RawStdEncoding.DecodeString("AGDqLz3gew+k8RfJVyLz+eL9T3tV")
			TGPrintlResult(client.MessagesSendMedia(&telegram.MessagesSendMediaParams{Peer: telegram.InputPeer(&telegram.InputPeerUser{UserID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),
				Message: "美女哦!",RandomID: time.Now().UnixNano(),Media: telegram.InputMedia(&telegram.InputMediaPhoto{ID:telegram.InputPhoto(&telegram.InputPhotoObj{ID: 5427239946923652209,AccessHash: -7019033494897652845,FileReference: FileReference}) })}))
		case "更新头像":
			FileReference,_:=base64.RawStdEncoding.DecodeString("AGDqLz3gew+k8RfJVyLz+eL9T3tV")
			TGPrintlResult(client.PhotosUpdateProfilePhoto(telegram.InputPhoto(&telegram.InputPhotoObj{ID: 5427239946923652209,AccessHash: -7019033494897652845,FileReference: FileReference})))
		case "上传头像":
			img,err:=ioutil.ReadFile(`C:\Users\Administrator\Pictures\2.jpg`)
			//Md5Checksum := getMd5String(img)
			dry.PanicIfErr(err)
			fileid:=time.Now().UnixNano()
			filep:=0
			for filep = 0; filep < len(img)/524288; filep++ {
				updata:=img[:524288]
				upb,err:=client.UploadSaveFilePart(fileid, int32(filep),updata)
				dry.PanicIfErr(err)
				fmt.Println(upb)
				img = img[524288:]
			}
			if len(img)>0{
				upb,err:=client.UploadSaveFilePart(fileid, int32(filep),img)
				dry.PanicIfErr(err)
				fmt.Println(upb)
				filep++
			}
			TGPrintlResult(client.PhotosUploadProfilePhoto(telegram.InputFile(&telegram.InputFileObj{ID: fileid,Parts: int32(filep)}),nil,0))
		case "下载文件":
			// 此处涉及授权转移https://core.telegram.org/api/datacenter 暂时不搞
			//FileReference,_:=base64.RawStdEncoding.DecodeString("AGDrUhNLOir11oi21zpuTm+YyvJP")
			TGPrintlResult(client.UploadGetFile(&telegram.UploadGetFileParams{Location: telegram.InputFileLocation(&telegram.InputDocumentFileLocation{ID: 6296397664816726796,AccessHash: 1177560048428552522,ThumbSize: "",FileReference: []uint8{
				0x01, 0x00, 0x00, 0x00, 0x22, 0x60, 0xeb, 0x56, 0xc0, 0x8e, 0x83, 0x73, 0x96, 0x2f, 0x44, 0x69,
				0x7e, 0x41, 0x23, 0xbb, 0x82, 0xcf, 0x31, 0x2b, 0x6e,
			}}),Offset: 0,Limit: 524288}))
		case "进入频道":
			hash := "hao123fuck"
			//搜索群组
			resolved, err := client.ContactsResolveUsername(hash)
			if err != nil {
				fmt.Println(err.Error())
				continue
			}

			channel := resolved.Chats[0].(*telegram.Channel)
			fmt.Println(channel.ID)
			inCh := telegram.InputChannel(&telegram.InputChannelObj{
				ChannelID:  channel.ID,
				AccessHash: channel.AccessHash,
			})
			TGPrintlResult(client.ChannelsJoinChannel(inCh))
		case "重新链接":
			err:=client.Reconnect()
			if err != nil {
				fmt.Println(err.Error())
				continue
			}else {
				fmt.Println("重新链接成功")
			}
		case "上线状态":
			TGPrintlResult(client.AccountUpdateStatus(false))
		case "离线状态":
			TGPrintlResult(client.AccountUpdateStatus(true))
		case "模拟输入":
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageTypingAction{}))
			time.Sleep(time.Second*2)
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageCancelAction{}))
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageGamePlayAction{}))
			time.Sleep(time.Second*2)
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageCancelAction{}))
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageRecordAudioAction{}))
			time.Sleep(time.Second*2)
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageCancelAction{}))
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageUploadAudioAction{}))
			time.Sleep(time.Second*2)
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageCancelAction{}))
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageGeoLocationAction{}))
			time.Sleep(time.Second*2)
			client.MessagesSetTyping(defaulPeer,0,telegram.SendMessageAction(&telegram.SendMessageCancelAction{}))
		case "获取服务器salts":
			TGPrintlResult(client.GetFutureSalts(32))
		}
	}
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}

func rawinput(title string) string {
	fmt.Print(title+":")
	outv, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	outv = strings.ReplaceAll(outv, "\n", "")
	return outv
}

func TGPrintlResult(data interface{},err error) string {
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	s,err:=json.Marshal(data)
	if err != nil {
		fmt.Println(err.Error())
		return ""
	}
	fmt.Println(string(s))
	return string(s)
}
func getMd5String(b []byte) string {
	return fmt.Sprintf("%X", md5.Sum(b))
}

func getUserPeer(obj *telegram.UserObj) telegram.InputPeer {
	return telegram.InputPeer(&telegram.InputPeerUser{UserID: obj.ID,AccessHash: obj.AccessHash})
}
func getChannelPeer(obj *telegram.Channel) telegram.InputPeer {
	return telegram.InputPeer(&telegram.InputPeerChannel{ChannelID: obj.ID,AccessHash: obj.AccessHash})
}
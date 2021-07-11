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
	}
	client.AddCustomServerRequestHandler(func(i interface{}) bool {
		pp.Println(i)

		switch i.(type) {
		case *telegram.UpdateShort:
			return true
		}
		return false
	})

	defaulPeer :=telegram.InputPeer(&telegram.InputPeerUser{UserID: 962210352,AccessHash: -1498843611134224383})//dreamzza
	for{
		choise :=rawinput("选择操作")
		switch choise {
		case "群成员获取":
			hash := "txtdx"

			//搜索群组
			resolved, err := client.ContactsResolveUsername(hash)
			dry.PanicIfErr(err)

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
				dry.PanicIfErr(err)
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
			dry.PanicIfErr(err)
			auth,err:=client.AuthAcceptLoginToken(token)
			dry.PanicIfErr(err)
			pp.Println(auth)
		case "获取频道消息":
			msgs,err:=client.MessagesGetAllChats([]int32{})
			dry.PanicIfErr(err)
			fmt.Println(msgs)
		case "获取指定聊天记录":
			username :=rawinput("username")
			cont,err:=client.ContactsResolveUsername(username)
			dry.PanicIfErr(err)
			fmt.Println(cont)
			tgUserobj :=cont.Users[0].(*telegram.UserObj)
			TGPrintlResult(client.MessagesGetCommonChats(telegram.InputUser(&telegram.InputUserObj{UserID: tgUserobj.ID,AccessHash: tgUserobj.AccessHash}),100,100))
		case "获取对话框列表":
			TGPrintlResult(client.MessagesGetDialogs(&telegram.MessagesGetDialogsParams{OffsetPeer: telegram.InputPeer(&telegram.InputPeerEmpty{})}))
		case "查看指定对话框":
			TGPrintlResult(client.MessagesGetPeerDialogs([]telegram.InputDialogPeer{telegram.InputDialogPeer(&telegram.InputDialogPeerObj{Peer: defaulPeer})}))
			// 标记已读
			client.MessagesReadHistory(defaulPeer,16)
		case "发送文本消息":
			TGPrintlResult(client.MessagesSendMessage(&telegram.MessagesSendMessageParams{Peer: defaulPeer,
				Message: "你好啊!",RandomID: time.Now().UnixNano()}))
		case "发送带链接消息":
			username :="dreamzza"
			cont,err:=client.ContactsResolveUsername(username)
			dry.PanicIfErr(err)
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
			dry.PanicIfErr(err)
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
	dry.PanicIfErr(err)
	s,err:=json.Marshal(data)
	dry.PanicIfErr(err)
	fmt.Println(string(s))
	return string(s)
}
func getMd5String(b []byte) string {
	return fmt.Sprintf("%X", md5.Sum(b))
}
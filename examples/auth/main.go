package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/k0kubun/pp"
	"github.com/pkg/errors"
	"github.com/xelaj/go-dry"
	"github.com/xelaj/mtproto"
	"github.com/xelaj/mtproto/telegram"

	utils "github.com/xelaj/mtproto/examples/example_utils"
)

const AppID = 4995960
const AppHash = "ca928547b9035ac7a4219a4441883f64"

func main() {

	phoneNumber := "+51968338062"

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

	// Please, don't spam auth too often, if you have session file, don't repeat auth process, please.
	signedIn, err := client.IsSessionRegistred()
	if err != nil {
		panic(errors.Wrap(err, "can't check that session is registred"))
	}

	if signedIn {
		println("You've already signed in!")
		os.Exit(0)
	}

	setCode, err := client.AuthSendCode(
		phoneNumber, AppID, AppHash, &telegram.CodeSettings{},
	)

	if err != nil {
		if strings.Contains(err.Error(),"PHONE_MIGRATE_X_NewIP"){
			// 重定向错误,更新客户端配置
			err = client.RefreshServerConfig()
			if err != nil {
				panic(err)
			}
			setCode, err = client.AuthSendCode(
				phoneNumber, AppID, AppHash, &telegram.CodeSettings{},
			)
		}
	}

	// this part shows how to deal with errors (if you want of course. No one
	// like errors, but the can be return sometimes)
	if err != nil {
		errResponse := &mtproto.ErrResponseCode{}
		if !errors.As(err, &errResponse) {
			// some strange error, looks like a bug actually
			pp.Println(err)
			panic(err)
		} else {
			if errResponse.Message == "AUTH_RESTART" {
				println("Oh crap! You accidentally restart authorization process!")
				println("You should login only once, if you'll spam 'AuthSendCode' method, you can be")
				println("timeouted to loooooooong long time. You warned.")
			} else if errResponse.Message == "FLOOD_WAIT_X" {
				println("No way... You've reached flood timeout! Did i warn you? Yes, i am. That's what")
				println("happens, when you don't listen to me...")
				println()
				timeoutDuration := time.Second * time.Duration(errResponse.AdditionalInfo.(int))

				println("Repeat after " + timeoutDuration.String())
			} else {
				println("Oh crap! Got strange error:")
				pp.Println(errResponse)
			}

			os.Exit(1)
		}
	}

	fmt.Print("Auth code: ")
	code, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	code = strings.ReplaceAll(code, "\n", "")

	auth, err := client.AuthSignIn(
		phoneNumber,
		setCode.PhoneCodeHash,
		code,
	)
	if err == nil {
		pp.Println(auth)
		auth2, _ := client.AuthSignUp(
			phoneNumber,
			setCode.PhoneCodeHash,
			"张",
			"小生",
		)
		pp.Println(auth2)
		fmt.Println("Success! You've signed in!")
		return
	}

	// if you don't have password protection — THAT'S ALL! You're already logged in.
	// but if you have 2FA, you need to make few more steps:

	// could be some errors:
	errResponse := &mtproto.ErrResponseCode{}
	ok := errors.As(err, &errResponse)
	// checking that error type is correct, and error msg is actualy ask for password
	if !ok || errResponse.Message != "SESSION_PASSWORD_NEEDED" {
		fmt.Println("SignIn failed:", err)
		println("\n\nMore info about error:")
		pp.Println(err)
		os.Exit(1)
	}

	fmt.Print("Password:")
	password, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	password = strings.ReplaceAll(password, "\n", "")

	accountPassword, err := client.AccountGetPassword()
	dry.PanicIfErr(err)

	// GetInputCheckPassword is fast response object generator
	inputCheck, err := telegram.GetInputCheckPassword(password, accountPassword)
	dry.PanicIfErr(err)

	auth, err = client.AuthCheckPassword(inputCheck)
	dry.PanicIfErr(err)

	pp.Println(auth)
	fmt.Println("Success! You've signed in!")
}

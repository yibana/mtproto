package main

import (
	"fmt"
	"github.com/pkg/errors"
	"path/filepath"
	"strings"

	"github.com/k0kubun/pp"
	"github.com/xelaj/go-dry"
	"github.com/xelaj/mtproto/telegram"

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

	pp.Println(client.AllUsersInChannel(-1001224870613))
}

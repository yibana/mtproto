// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/xelaj/mtproto/blob/master/LICENSE for details

package telegram

import (
	"github.com/pkg/errors"
	"github.com/xelaj/errs"
	dry "github.com/xelaj/go-dry"
	"reflect"
	"runtime"

	"github.com/xelaj/mtproto"
	"github.com/xelaj/mtproto/internal/keys"
	"strconv"
	"strings"
)

type Client struct {
	*mtproto.MTProto
	config       *ClientConfig
	serverConfig *Config
	initConnectionParams *InitConnectionParams
}

type ClientConfig struct {
	SessionFile     string
	ServerHost      string
	PublicKeysFile  string
	DeviceModel     string
	SystemVersion   string
	AppVersion      string
	AppID           int
	AppHash         string
	InitWarnChannel bool
	ProxyUrl string
}

const (
	warnChannelDefaultCapacity = 100
)

func NewClient(c ClientConfig) (*Client, error) { //nolint: gocritic arg is not ptr cause we call
	//                                                               it only once, don't care
	//                                                               about copying big args.
	if !dry.FileExists(c.PublicKeysFile) {
		return nil, errs.NotFound("file", c.PublicKeysFile)
	}

	if !dry.PathIsWritable(c.SessionFile) {
		return nil, errs.Permission(c.SessionFile).Scope("write")
	}

	if c.DeviceModel == "" {
		c.DeviceModel = "Unknown"
	}

	if c.SystemVersion == "" {
		c.SystemVersion = runtime.GOOS + "/" + runtime.GOARCH
	}

	if c.AppVersion == "" {
		c.AppVersion = "v0.0.0"
	}

	publicKeys, err := keys.ReadFromFile(c.PublicKeysFile)
	if err != nil {
		return nil, errors.Wrap(err, "reading public keys")
	}

	m, err := mtproto.NewMTProto(mtproto.Config{
		AuthKeyFile: c.SessionFile,
		ServerHost:  c.ServerHost,
		PublicKey:   publicKeys[0],
		ProxyUrl: c.ProxyUrl,
	})
	if err != nil {
		return nil, errors.Wrap(err, "setup common MTProto client")
	}

	if c.InitWarnChannel {
		m.Warnings = make(chan error, warnChannelDefaultCapacity)
	}

	err = m.CreateConnection()
	if err != nil {
		return nil, errors.Wrap(err, "creating connection")
	}

	client := &Client{
		MTProto: m,
		config:  &c,
	}

	//client.AddCustomServerRequestHandler(client.handleSpecialRequests())
	client.initConnectionParams = &InitConnectionParams{
		ApiID:          int32(c.AppID),
		DeviceModel:    c.DeviceModel,
		SystemVersion:  c.SystemVersion,
		AppVersion:     c.AppVersion,
		SystemLangCode: "en", // can't be edited, cause docs says that a single possible parameter
		LangCode:       "en",
		Query:          &HelpGetConfigParams{},
	}
	resp, err := client.InvokeWithLayer(ApiVersion, client.initConnectionParams)

	if err != nil {
		return nil, errors.Wrap(err, "getting server configs")
	}

	config, ok := resp.(*Config)
	if !ok {
		return nil, errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}

	client.serverConfig = config

	dcList := make(map[int]string)
	for _, dc := range config.DcOptions {
		if dc.Cdn {
			continue
		}
		if strings.Contains(dc.IpAddress,":"){
			//不允许IPV6的地址段
			continue
		}
		dcList[int(dc.ID)] = dc.IpAddress + ":" + strconv.Itoa(int(dc.Port))
	}

	client.SetDCList(dcList)
	return client, nil
}
func (m *Client) RefreshServerConfig() error {
	//如果服务器地址重定向,则更新配置
	resp, err := m.InvokeWithLayer(ApiVersion, m.initConnectionParams)

	if err != nil {
		return  errors.Wrap(err, "getting server configs")
	}

	config, ok := resp.(*Config)
	if !ok {
		return  errors.New("got wrong response: " + reflect.TypeOf(resp).String())
	}

	m.serverConfig = config
	return nil
}

func (m *Client) IsSessionRegistred() (bool, error) {
	_, err := m.UsersGetFullUser(&InputUserSelf{})
	if err == nil {
		return true, nil
	}
	var errCode *mtproto.ErrResponseCode
	if errors.As(err, &errCode) {
		if errCode.Message == "AUTH_KEY_UNREGISTERED" {
			return false, nil
		}
		return false, err
	} else {
		return false, err
	}
}

/*
func (c *Client) handleSpecialRequests() func(any) bool {
	return func(i any) bool {
		switch msg := i.(type) {
		case *UpdatesObj:
			pp.Println(msg, "UPDATE")
			return true
		case *UpdateShort:
			pp.Println(msg, "SHORT UPDATE")
			return true
		}

		return false
	}
}
*/
//----------------------------------------------------------------------------

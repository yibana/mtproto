// Copyright (c) 2020-2021 KHS Films
//
// This file is a part of mtproto package.
// See https://github.com/xelaj/mtproto/blob/master/LICENSE for details

package mtproto

// methods (or functions, you name it), which are taken from here https://core.telegram.org/schema/mtproto
// in fact, these ones are "aliases" of the methods of the described objects (which
// are in internal/mtproto/objects). The idea is taken from github.com/xelaj/vk

import (
	"github.com/xelaj/mtproto/internal/encoding/tl"
	"github.com/xelaj/mtproto/internal/mtproto/objects"
)

func (m *MTProto) reqPQ(nonce *tl.Int128) (*objects.ResPQ, error) {
	return objects.ReqPQ(m, nonce)
}

func (m *MTProto) reqDHParams(nonce, serverNonce *tl.Int128, p, q []byte, publicKeyFingerprint int64, encryptedData []byte) (objects.ServerDHParams, error) {
	return objects.ReqDHParams(m, nonce, serverNonce, p, q, publicKeyFingerprint, encryptedData)
}

func (m *MTProto) setClientDHParams(nonce, serverNonce *tl.Int128, encryptedData []byte) (objects.SetClientDHParamsAnswer, error) {
	return objects.SetClientDHParams(m, nonce, serverNonce, encryptedData)
}

func (m *MTProto) ping(pingID int64) (*objects.Pong, error) {
	return objects.Ping(m, pingID)
}

func (m *MTProto) ping_delay_disconnect(pingID int64,disconnect_delay int32) (*objects.Pong, error) {
	return objects.Ping_Delay_Disconnect(m, pingID,disconnect_delay)
}

func (m *MTProto) GetFutureSalts(num int32) (*objects.FutureSalts, error) {
	return objects.GetFutureSalts(m, num)
}
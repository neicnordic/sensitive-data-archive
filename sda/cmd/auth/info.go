package main

import (
	"encoding/base64"
	"io"
	"os"
	"path/filepath"

	"github.com/kataras/iris/v12"
	log "github.com/sirupsen/logrus"
)

type Info struct {
	ClientID  string `json:"client_id"`
	OidcURI   string `json:"oidc_uri"`
	PublicKey string `json:"public_key"`
	InboxURI  string `json:"inbox_uri"`
}

// Reads the public key file and returns the public key
func readPublicKeyFile(filename string) (string, error) {
	log.Info("Reading Public key file")
	file, err := os.Open(filepath.Clean(filename))
	if err != nil {
		return "", err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(data), err
}

// getInfo returns information needed by the client to authenticate
func (auth AuthHandler) getInfo(ctx iris.Context) {
	info := Info{ClientID: auth.OAuth2Config.ClientID, OidcURI: auth.Config.OIDC.Provider, PublicKey: auth.pubKey, InboxURI: auth.Config.S3Inbox}

	err := ctx.JSON(info)
	if err != nil {
		log.Error("Failure to get Info ", err)

		return
	}
}

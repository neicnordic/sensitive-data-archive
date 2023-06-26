package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kataras/iris/v12"
	"github.com/neicnordic/crypt4gh/keys"
	log "github.com/sirupsen/logrus"
)

type Info struct {
	ClientID  string `json:"client_id"`
	OidcURI   string `json:"oidc_uri"`
	PublicKey string `json:"public_key"`
	InboxURI  string `json:"inbox_uri"`
}

// Reads the public key file and returns the public key
func readPublicKeyFile(filename string) (key *[32]byte, err error) {
	log.Info("Reading Public key file")
	file, err := os.Open(filepath.Clean(filename))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	publicKey, err := keys.ReadPublicKey(file)
	if err != nil {
		return nil, fmt.Errorf("error while reading public key file %s: %v", filename, err)
	}

	return &publicKey, err
}

// getInfo returns information needed by the client to authenticate
func (auth AuthHandler) getInfo(ctx iris.Context) {
	info := Info{ClientID: auth.OAuth2Config.ClientID, OidcURI: auth.Config.JwtIssuer, PublicKey: auth.pubKey, InboxURI: auth.Config.S3Inbox}

	err := ctx.JSON(info)
	if err != nil {
		log.Error("Failure to get Info ", err)

		return
	}
}

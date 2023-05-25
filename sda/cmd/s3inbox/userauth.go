package main

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/lestrrat-go/jwx/jwk"
	log "github.com/sirupsen/logrus"
)

// Authenticator is an interface that takes care of authenticating users to the
// S3 proxy. It contains only one method, Authenticate.
type Authenticator interface {
	// Authenticate inspects an http.Request and returns nil if the user is
	// authenticated, otherwise an error is returned.
	Authenticate(r *http.Request) (jwt.MapClaims, error)
}

// AlwaysAllow is an Authenticator that always authenticates
type AlwaysAllow struct{}

// NewAlwaysAllow returns a new AlwaysAllow authenticator.
func NewAlwaysAllow() *AlwaysAllow {
	return &AlwaysAllow{}
}

// Authenticate authenticates everyone.
func (u *AlwaysAllow) Authenticate(_ *http.Request) (jwt.MapClaims, error) {
	return nil, nil
}

// ValidateFromToken is an Authenticator that reads the public key from
// supplied file
type ValidateFromToken struct {
	pubkeys map[string][]byte
}

// NewValidateFromToken returns a new ValidateFromToken, reading the key from
// the supplied file.
func NewValidateFromToken(pubkeys map[string][]byte) *ValidateFromToken {
	return &ValidateFromToken{pubkeys}
}

// Authenticate verifies that the token included in the http.Request
// is valid
func (u *ValidateFromToken) Authenticate(r *http.Request) (claims jwt.MapClaims, err error) {
	var ok bool

	// Verify signature by parsing the token with the given key
	tokenStr := r.Header.Get("X-Amz-Security-Token")
	if tokenStr == "" {
		return nil, fmt.Errorf("no access token supplied")
	}

	token, err := jwt.Parse(tokenStr, func(tokenStr *jwt.Token) (interface{}, error) { return nil, nil })
	// Return error if token is broken (without claims)
	if claims, ok = token.Claims.(jwt.MapClaims); !ok {
		return nil, fmt.Errorf("broken token (claims are empty): %v\nerror: %s", claims, err)
	}

	strIss := fmt.Sprintf("%v", claims["iss"])
	// Poor string unescaper for elixir
	strIss = strings.ReplaceAll(strIss, "\\", "")

	log.Debugf("Looking for key for %s", strIss)

	iss, err := url.ParseRequestURI(strIss)
	if err != nil || iss.Hostname() == "" {
		return nil, fmt.Errorf("failed to get issuer from token (%v)", strIss)
	}

	switch token.Header["alg"] {
	case "ES256":
		key, err := jwt.ParseECPublicKeyFromPEM(u.pubkeys[iss.Hostname()])
		if err != nil {
			return nil, fmt.Errorf("failed to parse EC public key (%v)", err)
		}
		_, err = jwt.Parse(tokenStr, func(tokenStr *jwt.Token) (interface{}, error) { return key, nil })
		if err != nil {
			return nil, fmt.Errorf("signed token (ES256) not valid: %v, (token was %s)", err, tokenStr)
		}
	case "RS256":
		key, err := jwt.ParseRSAPublicKeyFromPEM(u.pubkeys[iss.Hostname()])
		if err != nil {
			return nil, fmt.Errorf("failed to parse RSA256 public key (%v)", err)
		}
		_, err = jwt.Parse(tokenStr, func(tokenStr *jwt.Token) (interface{}, error) { return key, nil })
		if err != nil {
			return nil, fmt.Errorf("signed token (RS256) not valid: %v, (token was %s)", err, tokenStr)
		}
	default:
		return nil, fmt.Errorf("unsupported algorithm %s", token.Header["alg"])
	}

	// Check whether token username and filepath match
	str, err := url.ParseRequestURI(r.URL.Path)
	if err != nil || str.Path == "" {
		return nil, fmt.Errorf("failed to get path from query (%v)", r.URL.Path)
	}

	path := strings.Split(str.Path, "/")
	username := path[1]

	// Case for Elixir and CEGA usernames: Replace @ with _ character
	if strings.Contains(fmt.Sprintf("%v", claims["sub"]), "@") {
		if strings.ReplaceAll(fmt.Sprintf("%v", claims["sub"]), "@", "_") != username {
			return nil, fmt.Errorf("token supplied username %s but URL had %s", claims["sub"], username)
		}
	} else if claims["sub"] != username {
		return nil, fmt.Errorf("token supplied username %s but URL had %s", claims["sub"], username)
	}

	return claims, nil
}

// Function for reading the ega key in []byte
func (u *ValidateFromToken) getjwtkey(jwtpubkeypath string) error {
	err := filepath.Walk(jwtpubkeypath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				log.Debug("Reading file: ", filepath.Join(filepath.Clean(jwtpubkeypath), info.Name()))
				keyData, err := os.ReadFile(filepath.Join(filepath.Clean(jwtpubkeypath), info.Name()))
				if err != nil {
					return fmt.Errorf("token file error: %v", err)
				}
				nameMatch := strings.TrimSuffix(info.Name(), filepath.Ext(info.Name()))
				u.pubkeys[nameMatch] = keyData
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to get public key files (%v)", err)
	}

	return nil
}

// Function for fetching the elixir key from the JWK and transform it to []byte
func (u *ValidateFromToken) getjwtpubkey(jwtpubkeyurl string) error {
	jwkURL, err := url.ParseRequestURI(jwtpubkeyurl)
	if err != nil || jwkURL.Scheme == "" || jwkURL.Host == "" {
		if err != nil {
			return err
		}

		return fmt.Errorf("jwtpubkeyurl is not a proper URL (%s)", jwkURL)
	}
	log.Debug("jwkURL: ", jwkURL.Scheme)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	set, err := jwk.Fetch(ctx, jwtpubkeyurl)
	if err != nil {
		return fmt.Errorf("jwk.Fetch failed (%v) for %s", err, jwtpubkeyurl)
	}
	keyEl, ok := set.Get(0)
	if !ok {
		return fmt.Errorf("failed to materialize public key (%v)", err)
	}
	pkeyBytes, err := x509.MarshalPKIXPublicKey(keyEl)
	if err != nil {
		return fmt.Errorf("failed to marshal public key (%v)", err)
	}

	log.Debugf("Getting key from %s", jwtpubkeyurl)
	r, err := http.Get(jwtpubkeyurl) // nolint G107
	if err != nil {
		return fmt.Errorf("failed to get JWK (%v)", err)
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("failed to read key response (%v)", err)
	}
	defer r.Body.Close()

	var keytype map[string][]map[string]string
	err = json.Unmarshal(b, &keytype)
	if err != nil {
		return fmt.Errorf("failed to unmarshal key response (%v, response was %s)", err, b)
	}
	keyData := pem.EncodeToMemory(
		&pem.Block{
			Type:  keytype["keys"][0]["kty"] + " PUBLIC KEY",
			Bytes: pkeyBytes,
		},
	)
	u.pubkeys[jwkURL.Hostname()] = keyData
	log.Debugf("Registered public key for %s", jwkURL.Hostname())

	return nil
}

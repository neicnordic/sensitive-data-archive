package authenticator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	log "github.com/sirupsen/logrus"
)

type Authenticator interface {
	Authenticate() gin.HandlerFunc
}

type validateFromToken struct {
	Keyset jwk.Set
}

func NewAuthenticator() (Authenticator, error) {
	authenticator := &validateFromToken{jwk.NewSet()}
	switch {
	case jwtPubKeyPath != "":
		log.Info("new authenticator from jwt public key path")
		if err := authenticator.readJwtPubKeyPath(jwtPubKeyPath); err != nil {
			return nil, err
		}
	case jwtPubKeyUrl != "":
		log.Info("new authenticator from jwt public key url")
		if err := authenticator.fetchJwtPubKeyURL(jwtPubKeyUrl); err != nil {
			return nil, err
		}
	}
	return authenticator, nil
}

func (u *validateFromToken) readJwtPubKeyPath(jwtPubKeyPath string) error {
	err := filepath.Walk(jwtPubKeyPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				log.Debug("Reading file: ", filepath.Join(filepath.Clean(jwtPubKeyPath), info.Name()))
				keyData, err := os.ReadFile(filepath.Join(filepath.Clean(jwtPubKeyPath), info.Name()))
				if err != nil {
					return fmt.Errorf("key file error: %v", err)
				}

				key, err := jwk.ParseKey(keyData, jwk.WithPEM(true))
				if err != nil {
					return fmt.Errorf("parseKey failed: %v", err)
				}

				if err := jwk.AssignKeyID(key); err != nil {
					return fmt.Errorf("assignKeyID failed: %v", err)
				}

				if err := u.Keyset.AddKey(key); err != nil {
					return fmt.Errorf("failed to add key to set: %v", err)
				}
			}

			return nil
		})
	if err != nil {
		return fmt.Errorf("failed to get public key files (%v)", err)
	}

	return nil
}

func (u *validateFromToken) fetchJwtPubKeyURL(jwtPubKeyUrl string) error {
	jwkURL, err := url.ParseRequestURI(jwtPubKeyUrl)
	if err != nil || jwkURL.Scheme == "" || jwkURL.Host == "" {
		if err != nil {
			return err
		}

		return fmt.Errorf("jwtPubKeyUrl is not a proper URL (%s)", jwkURL)
	}
	log.Info("jwkURL: ", jwtPubKeyUrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u.Keyset, err = jwk.Fetch(ctx, jwtPubKeyUrl)
	if err != nil {
		return fmt.Errorf("jwk.Fetch failed (%v) for %s", err, jwtPubKeyUrl)
	}

	for it := u.Keyset.Keys(context.Background()); it.Next(context.Background()); {
		pair := it.Pair()
		key := pair.Value.(jwk.Key)
		if err := jwk.AssignKeyID(key); err != nil {
			return fmt.Errorf("AssignKeyID failed: %v", err)
		}
	}

	return nil
}

// Authenticate verifies that the token included in the http.Request is valid
func (u *validateFromToken) Authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		log.Info("validating auth")
		if u == nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		var tokenString string
		switch {
		case c.GetHeader("X-Amz-Security-Token") != "":
			tokenString = c.GetHeader("X-Amz-Security-Token")
		case c.GetHeader("Authorization") != "":
			authStr := c.GetHeader("Authorization")
			var err error
			tokenString, err = readTokenFromHeader(authStr)
			if err != nil {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		default:
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		if tokenString == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse([]byte(tokenString), jwt.WithKeySet(u.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		if err != nil {
			log.Warnf("failed to parse token: %v", err)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		iss, err := url.ParseRequestURI(token.Issuer())
		if err != nil || iss.Hostname() == "" {
			log.Warnf("failed to get issuer from token (%v)", iss)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("token", token)
		c.Next()
	}
}

func readTokenFromHeader(authStr string) (string, error) {
	headerParts := strings.Split(authStr, " ")
	if headerParts[0] != "Bearer" {
		return "", errors.New("authorization scheme must be bearer")
	}
	if len(headerParts) != 2 || headerParts[1] == "" {
		return "", errors.New("token string is missing from authorization header")
	}

	return headerParts[1], nil
}

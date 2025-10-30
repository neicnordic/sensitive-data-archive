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
	config *authenticatorConfig
}

func NewAuthenticator(options ...func(config *authenticatorConfig)) (Authenticator, error) {
	authenticator := &validateFromToken{
		Keyset: jwk.NewSet(),
		config: conf.clone(),
	}

	for _, option := range options {
		option(authenticator.config)
	}

	if authenticator.config.jwtPubKeyPath != "" {
		log.Info("authenticator add jwt public from key path")
		if err := authenticator.readJwtPubKeyPath(); err != nil {
			return nil, err
		}
	}
	if authenticator.config.jwtPubKeyUrl != "" {
		log.Info("authenticator add jwt public key from url")
		if err := authenticator.fetchJwtPubKeyURL(); err != nil {
			return nil, err
		}
	}

	return authenticator, nil
}

func (u *validateFromToken) readJwtPubKeyPath() error {
	err := filepath.Walk(u.config.jwtPubKeyPath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				log.Debug("Reading file: ", filepath.Join(filepath.Clean(u.config.jwtPubKeyPath), info.Name()))
				keyData, err := os.ReadFile(filepath.Join(filepath.Clean(u.config.jwtPubKeyPath), info.Name()))
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

func (u *validateFromToken) fetchJwtPubKeyURL() error {
	jwkURL, err := url.ParseRequestURI(u.config.jwtPubKeyUrl)
	if err != nil || jwkURL.Scheme == "" || jwkURL.Host == "" {
		if err != nil {
			return err
		}

		return fmt.Errorf("jwtPubKeyUrl is not a proper URL (%s)", jwkURL)
	}
	log.Info("jwkURL: ", u.config.jwtPubKeyUrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u.Keyset, err = jwk.Fetch(ctx, u.config.jwtPubKeyUrl)
	if err != nil {
		return fmt.Errorf("jwk.Fetch failed (%v) for %s", err, u.config.jwtPubKeyUrl)
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

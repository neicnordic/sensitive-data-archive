package userauth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	log "github.com/sirupsen/logrus"
)

// Authenticator is an interface that takes care of authenticating users to the
// S3 proxy. It contains only one method, Authenticate.
type Authenticator interface {
	// Authenticate inspects an http.Request and returns nil if the user is
	// authenticated, otherwise an error is returned.
	Authenticate(r *http.Request) (jwt.Token, error)
}

// ValidateFromToken is an Authenticator that reads the public key from
// supplied file
type ValidateFromToken struct {
	Keyset jwk.Set
}

// NewValidateFromToken returns a new ValidateFromToken, reading the key from
// the supplied file.
func NewValidateFromToken(keyset jwk.Set) *ValidateFromToken {
	return &ValidateFromToken{keyset}
}

// Authenticate verifies that the token included in the http.Request is valid
func (u *ValidateFromToken) Authenticate(r *http.Request) (jwt.Token, error) {
	// Verify signature by parsing the token with the given key
	if u == nil {
		return nil, fmt.Errorf("error validating token keyset")
	}
	switch {

	case r.Header.Get("X-Amz-Security-Token") != "":
		tokenStr := r.Header.Get("X-Amz-Security-Token")
		if tokenStr == "" {
			return nil, fmt.Errorf("no access token supplied")
		}
		token, err := jwt.Parse([]byte(tokenStr), jwt.WithKeySet(u.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		if err != nil {
			return nil, err
		}

		iss, err := url.ParseRequestURI(token.Issuer())
		if err != nil || iss.Hostname() == "" {
			return nil, fmt.Errorf("failed to get issuer from token (%v)", iss)
		}

		return token, nil

	case r.Header.Get("Authorization") != "":
		authStr := r.Header.Get("Authorization")
		tokenStr, err := readTokenFromHeader(authStr)
		if err != nil {
			return nil, fmt.Errorf("auth header not valid: %s, (header was %s)", err.Error(), authStr)
		}
		token, err := jwt.Parse([]byte(tokenStr), jwt.WithKeySet(u.Keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithValidate(true))
		if err != nil {
			return nil, fmt.Errorf("signed token not valid: %s, (token was %s)", err.Error(), tokenStr)
		}

		iss, err := url.ParseRequestURI(token.Issuer())
		if err != nil || iss.Hostname() == "" {
			return nil, fmt.Errorf("failed to get issuer from token (%v)", iss)
		}

		return token, nil

	default:
		return nil, fmt.Errorf("no access token supplied")

	}
}

// Function for reading the ega key in []byte
func (u *ValidateFromToken) ReadJwtPubKeyPath(jwtpubkeypath string) error {
	err := filepath.Walk(jwtpubkeypath,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				log.Debug("Reading file: ", filepath.Join(filepath.Clean(jwtpubkeypath), info.Name()))
				keyData, err := os.ReadFile(filepath.Join(filepath.Clean(jwtpubkeypath), info.Name()))
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

// Function for fetching the elixir key from the JWK and transform it to []byte
func (u *ValidateFromToken) FetchJwtPubKeyURL(jwtpubkeyurl string) error {
	jwkURL, err := url.ParseRequestURI(jwtpubkeyurl)
	if err != nil || jwkURL.Scheme == "" || jwkURL.Host == "" {
		if err != nil {
			return err
		}

		return fmt.Errorf("jwtpubkeyurl is not a proper URL (%s)", jwkURL)
	}
	log.Info("jwkURL: ", jwtpubkeyurl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	u.Keyset, err = jwk.Fetch(ctx, jwtpubkeyurl)
	if err != nil {
		return fmt.Errorf("jwk.Fetch failed (%v) for %s", err, jwtpubkeyurl)
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

func readTokenFromHeader(authStr string) (string, error) {
	headerParts := strings.Split(authStr, " ")
	if headerParts[0] != "Bearer" {
		return "", fmt.Errorf("authorization scheme must be bearer")
	}
	if len(headerParts) != 2 || headerParts[1] == "" {
		return "", fmt.Errorf("token string is missing from authorization header")
	}

	return headerParts[1], nil
}

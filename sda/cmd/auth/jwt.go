package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func generateJwtToken(tokenClaims map[string]any, keyPath, alg string) (string, string, error) {
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return "", "", err
	}

	jwtKey, err := jwk.ParseKey(prKey, jwk.WithPEM(true))
	if err != nil {
		return "", "", err
	}
	if err := jwtKey.Set(jwk.AlgorithmKey, alg); err != nil {
		return "", "", err
	}
	if err := jwk.AssignKeyID(jwtKey); err != nil {
		return "", "", err
	}

	token := jwt.New()
	for key, value := range tokenClaims {
		if err := token.Set(key, value); err != nil {
			return "", "", err
		}
	}
	expireDate, _ := token.Get(jwt.ExpirationKey)

	tokenString, err := jwt.Sign(token, jwt.WithKey(jwa.KeyAlgorithmFrom(alg), jwtKey))
	if err != nil {
		return "", "", err
	}

	return string(tokenString), expireDate.(time.Time).Format("2006-01-02 15:04:05"), nil
}

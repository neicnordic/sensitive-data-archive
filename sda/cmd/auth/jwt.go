package main

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/lestrrat-go/jwx/v4/jwa"
	"github.com/lestrrat-go/jwx/v4/jwk"
	"github.com/lestrrat-go/jwx/v4/jwt"
)

func generateJwtToken(tokenClaims map[string]any, keyPath, alg string) (string, string, error) {
	prKey, err := os.ReadFile(filepath.Clean(keyPath))
	if err != nil {
		return "", "", err
	}

	jwtKey, err := jwk.ParseKey(prKey, jwk.WithX509(true))
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
	expireDate, _ := token.Expiration()

	keyAlg, err := jwa.KeyAlgorithmFrom(alg)
	if err != nil {
		return "", "", errors.New("unknown key algorithm")
	}
	tokenString, err := jwt.Sign(token, jwt.WithKey(keyAlg, jwtKey))
	if err != nil {
		return "", "", err
	}

	return string(tokenString), expireDate.Format("2006-01-02 15:04:05"), nil
}

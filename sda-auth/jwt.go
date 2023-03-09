package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

type Claims struct {
	Email string `json:"email,omitempty"`
	KeyID string `json:"kid,omitempty"`
	jwt.RegisteredClaims
}

func generateJwtToken(claims *Claims, key, alg string) (string, string, error) {
	// Create a new token object by specifying signing method and the needed claims
	ttl := 200 * time.Hour
	expireDate := time.Now().UTC().Add(ttl)
	claims.ExpiresAt = jwt.NewNumericDate(expireDate)

	token := jwt.NewWithClaims(jwt.GetSigningMethod(alg), claims)

	data, err := os.ReadFile(key)
	if err != nil {
		return "", "", fmt.Errorf("Failed to read signingkey, reason: %v", err)
	}
	claims.KeyID = fmt.Sprintf("%x", sha256.Sum256(data))

	var tokenString string
	switch alg {
	case "ES256":
		pk, err := jwt.ParseECPrivateKeyFromPEM(data)
		if err != nil {
			return "", "", err
		}
		tokenString, err = token.SignedString(pk)
		if err != nil {
			return "", "", err
		}
	case "RS256":
		pk, err := jwt.ParseRSAPrivateKeyFromPEM(data)
		if err != nil {
			return "", "", err
		}
		tokenString, err = token.SignedString(pk)
		if err != nil {
			return "", "", err
		}
	}

	return tokenString, expireDate.Format("2006-01-02 15:04:05"), nil
}

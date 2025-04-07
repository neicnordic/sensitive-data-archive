package main

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

// OIDCIdentity represents an OIDC user instance
type OIDCIdentity struct {
	User                 string
	Passport             []string
	RawToken             string
	ResignedToken        string
	Fullname             string
	Email                string
	EdupersonEntitlement []string
	ExpDateRaw           string
	ExpDateResigned      string
}

// Configure an OpenID Connect aware OAuth2 client.
func getOidcClient(conf config.OIDCConfig) (oauth2.Config, *oidc.Provider, error) {
	contx := context.Background()
	provider, err := oidc.NewProvider(contx, conf.Provider)
	if err != nil {
		return oauth2.Config{}, nil, err
	}

	oauth2Config := oauth2.Config{
		ClientID:     conf.ID,
		ClientSecret: conf.Secret,
		RedirectURL:  conf.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "ga4gh_passport_v1 profile email eduperson_entitlement"},
	}

	return oauth2Config, provider, err
}

// Authenticate with an Oidc client.against OIDC AAI
func authenticateWithOidc(oauth2Config oauth2.Config, provider *oidc.Provider, code, jwkURL string) (OIDCIdentity, error) {
	contx := context.Background()
	defer contx.Done()
	var idStruct OIDCIdentity

	oauth2Token, err := oauth2Config.Exchange(contx, code)
	if err != nil {
		log.Error("Failed to fetch oauth2 code")

		return idStruct, err
	}

	// Extract the Access Token from OAuth2 token.
	rawAccessToken := oauth2Token.AccessToken
	if rawAccessToken == "" {
		log.Error("Failed to extract access token from OAuth2 token")

		return idStruct, err
	}

	// Validate raw token signature and get expiration date
	_, rawExpDate, err := validateToken(rawAccessToken, jwkURL)
	if err != nil {
		return idStruct, fmt.Errorf("could not validate raw jwt against pub key, reason: %v", err)
	}

	var verifier = provider.Verifier(&oidc.Config{ClientID: oauth2Config.ClientID})

	// Parse and verify Access Token payload.
	_, err = verifier.Verify(contx, rawAccessToken)
	if err != nil {
		log.Error("Failed to verify id token")

		return idStruct, err
	}

	// Fetch user information
	userInfo, err := provider.UserInfo(contx, oauth2.StaticTokenSource(oauth2Token))
	if err != nil {
		log.Error("Failed to get userinfo")

		return idStruct, err
	}

	// Extract custom passports, name and email claims
	var claims struct {
		PassportClaim        []string `json:"ga4gh_passport_v1"`
		FullnameClaim        string   `json:"name"`
		EmailClaim           string   `json:"email"`
		EdupersonEntitlement []string `json:"eduperson_entitlement"`
	}
	if err := userInfo.Claims(&claims); err != nil {
		log.Error("Failed to get custom claims")

		return idStruct, err
	}

	idStruct = OIDCIdentity{
		User:                 userInfo.Subject,
		RawToken:             rawAccessToken,
		ResignedToken:        rawAccessToken,
		Passport:             claims.PassportClaim,
		Fullname:             claims.FullnameClaim,
		Email:                claims.EmailClaim,
		EdupersonEntitlement: claims.EdupersonEntitlement,
		ExpDateRaw:           rawExpDate,
		ExpDateResigned:      rawExpDate,
	}

	return idStruct, err
}

// Validate raw (OIDC) jwt against public key from jwk. Return parsed jwt and its expiration date.
func validateToken(rawJwt, jwksURL string) (*jwt.Token, string, error) {
	set, err := jwk.Fetch(context.Background(), jwksURL)
	if err != nil {
		return nil, "", fmt.Errorf("%s", err.Error())
	}
	for it := set.Keys(context.Background()); it.Next(context.Background()); {
		pair := it.Pair()
		key := pair.Value.(jwk.Key)
		if err := jwk.AssignKeyID(key); err != nil {
			return nil, "", fmt.Errorf("AssignKeyID failed: %v", err)
		}
	}

	token, err := jwt.Parse(
		[]byte(rawJwt),
		jwt.WithKeySet(set, jws.WithInferAlgorithmFromKey(true)),
		jwt.WithValidate(true),
		jwt.WithMinDelta(10*time.Second, jwt.ExpirationKey, jwt.IssuedAtKey),
	)
	if err != nil {
		return nil, "", fmt.Errorf("signed token not valid: %s, (token was %s)", err.Error(), rawJwt)
	}

	return &token, token.Expiration().Format("2006-01-02 15:04:05"), err
}

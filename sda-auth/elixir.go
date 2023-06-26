package main

import (
	"fmt"
	"time"

	"github.com/coreos/go-oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/lestrrat/go-jwx/jwk"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

// ElixirIdentity represents an Elixir user instance
type ElixirIdentity struct {
	User     string
	Passport []string
	Token    string
	Profile  string
	Email    string
	ExpDate  string
}

// Configure an OpenID Connect aware OAuth2 client.
func getOidcClient(conf ElixirConfig) (oauth2.Config, *oidc.Provider) {
	contx := context.Background()
	provider, err := oidc.NewProvider(contx, conf.Provider)
	if err != nil {
		log.Fatal(err)
	}

	oauth2Config := oauth2.Config{
		ClientID:     conf.ID,
		ClientSecret: conf.Secret,
		RedirectURL:  conf.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "ga4gh_passport_v1 profile email"},
	}

	return oauth2Config, provider
}

// Authenticate with an Oidc client.against Elixir AAI
func authenticateWithOidc(oauth2Config oauth2.Config, provider *oidc.Provider, code, jwkURL string) (ElixirIdentity, error) {

	contx := context.Background()
	defer contx.Done()
	var idStruct ElixirIdentity

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
		PassportClaim []string `json:"ga4gh_passport_v1"`
		ProfileClaim  string   `json:"name"`
		EmailClaim    string   `json:"email"`
	}
	if err := userInfo.Claims(&claims); err != nil {
		log.Error("Failed to get custom claims")

		return idStruct, err
	}

	idStruct = ElixirIdentity{
		User:     userInfo.Subject,
		Token:    rawAccessToken,
		Passport: claims.PassportClaim,
		Profile:  claims.ProfileClaim,
		Email:    claims.EmailClaim,
		ExpDate:  rawExpDate,
	}

	return idStruct, err
}

// Validate raw (Elixir) jwt against public key from jwk. Return parsed jwt and its expiration date.
func validateToken(rawJwt, jwksURL string) (*jwt.Token, string, error) {

	// Fetch public key
	set, err := jwk.Fetch(jwksURL)
	if err != nil {
		return nil, "", fmt.Errorf(err.Error())
	}
	pubKey, err := set.Keys[0].Materialize()
	if err != nil {
		return nil, "", fmt.Errorf("failed to materialize public key %s", err.Error())
	}

	token, err := jwt.Parse(rawJwt, func(token *jwt.Token) (interface{}, error) {

		// Validate that the alg is what we expect: RSA or ES
		_, okRSA := token.Method.(*jwt.SigningMethodRSA)
		_, okES := token.Method.(*jwt.SigningMethodECDSA)
		if !(okRSA || okES) {
			return nil, fmt.Errorf("unexpected signing method")
		}

		return pubKey, nil
	})

	// Validate the error
	v, _ := err.(*jwt.ValidationError)

	// If error is for signature validation
	if err != nil && v.Errors == jwt.ValidationErrorSignatureInvalid {
		return nil, "", fmt.Errorf("signature not valid: %s", err.Error())
	}

	// Verify token dates. Ignores clock skew, but that should be
	// irrelevant here since tokens are relatively long-lived
	if err := token.Claims.(jwt.MapClaims).Valid(); err != nil {
		return nil, "", fmt.Errorf(err.Error())
	}

	var expireDate time.Time
	switch d := token.Claims.(jwt.MapClaims)["exp"].(type) {
	case float64:
		expireDate = time.Unix(int64(d), 0)
	case int64:
		expireDate = time.Unix(d, 0)
	default:
		return nil, "", fmt.Errorf("failed to read expiration date from token")
	}

	return token, expireDate.Format("2006-01-02 15:04:05"), err
}

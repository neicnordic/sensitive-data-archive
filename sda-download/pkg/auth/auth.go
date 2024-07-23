package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jws"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sda-download/internal/config"
	"github.com/neicnordic/sda-download/internal/database"
	"github.com/neicnordic/sda-download/pkg/request"
	log "github.com/sirupsen/logrus"
)

// Details stores an OIDCDetails struct
var Details OIDCDetails

// OIDCDetails is used to draw the response bytes to a struct
type OIDCDetails struct {
	Userinfo string `json:"userinfo_endpoint"`
	JWK      string `json:"jwks_uri"`
}

// GetOIDCDetails requests OIDC configuration information
func GetOIDCDetails(url string) (OIDCDetails, error) {
	log.Debugf("requesting OIDC config from %s", url)
	// Prepare response body struct
	var u OIDCDetails
	// Do request
	response, err := request.MakeRequest("GET", url, nil, nil)
	if err != nil {
		log.Errorf("request failed, %s", err)

		return u, err
	}
	// Parse response
	err = json.NewDecoder(response.Body).Decode(&u)
	if err != nil {
		log.Errorf("failed to parse JSON response, %s", err)

		return u, err
	}
	defer response.Body.Close()
	log.Debugf("received OIDC config %s from %s", u, url)

	return u, nil
}

// VerifyJWT verifies the token signature
func VerifyJWT(o OIDCDetails, token string) (jwt.Token, error) {
	log.Debug("verifying JWT signature")
	// we create a basic context
	// context.TODO and context.Background would have worked as well
	// we wanted to have it detailed, and we don't want it to hang forever
	// 30 seconds should be enough
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	keyset, err := jwk.Fetch(ctx, o.JWK, jwk.WithHTTPClient(request.Client))
	if err != nil {
		log.Errorf("failed to request JWK set from %s, %s", o.JWK, err)

		return nil, err
	}
	key, valid := keyset.Key(0)
	if !valid {
		log.Errorf("cannot get key from set , %s", err)
	}

	verifiedToken, err := jwt.Parse([]byte(token), jwt.WithKeySet(keyset, jws.WithInferAlgorithmFromKey(true)), jwt.WithVerifyAuto(nil, jwk.WithHTTPClient(request.Client)))
	if err != nil {
		log.Debugf("failed to infer validation from token, reason %s", err)

		// we try with RSA256 which is in most of the providers out there
		verifiedToken, err = jwt.Parse([]byte(token), jwt.WithKey(jwa.RS256, key), jwt.WithVerifyAuto(nil, jwk.WithHTTPClient(request.Client)))
		if err != nil {
			log.Errorf("failed to verify token as RSA256 signature of token %s, %s", token, err)

			return nil, err
		}
	}
	log.Debug(verifiedToken)
	log.Debug("JWT signature verified")

	return verifiedToken, nil
}

// GetToken parses the token string from a `http.Header`. The token string can
// come with either the S3 "X-Amz-Security-Token" header or the "Authorization"
// header. The "X-Amz-Security-Token" header is checked first, since it requires
// less formatting.
var GetToken = func(headers http.Header) (string, int, error) {
	log.Debug("parsing access token from header")

	// Check "X-Amz-Security-Token" header first
	header := headers.Get("X-Amz-Security-Token")
	if len(header) != 0 {
		return header, 0, nil
	}

	// Otherwise, check (and process) the Authorization header
	header = headers.Get("Authorization")
	if len(header) == 0 {
		log.Debug("authorization check failed")

		return "", 401, errors.New("access token must be provided")
	}

	// Check that Bearer scheme is used
	headerParts := strings.Split(header, " ")
	if headerParts[0] != "Bearer" {
		log.Debug("authorization check failed")

		return "", 400, errors.New("authorization scheme must be bearer")
	}

	// Check that header contains a token string
	var token string
	if len(headerParts) == 2 {
		token = headerParts[1]
	} else {
		log.Debug("authorization check failed")

		return "", 400, errors.New("token string is missing from authorization header")
	}
	log.Debug("access token found")

	return token, 0, nil
}

type JKU struct {
	URL string `json:"jku"`
}

// Visas is used to draw the response bytes to a struct
type Visas struct {
	Visa []string `json:"ga4gh_passport_v1"`
}

// Visa is used to draw the dataset name out of the visa
type Visa struct {
	Type    string `json:"type"`
	Dataset string `json:"value"`
}

// GetVisas requests the list of visas from userinfo endpoint
var GetVisas = func(o OIDCDetails, token string) (*Visas, error) {
	log.Debugf("requesting visas from %s", o.Userinfo)
	// Set headers
	headers := map[string]string{}
	headers["Authorization"] = "Bearer " + token
	// Do request
	response, err := request.MakeRequest("GET", o.Userinfo, headers, nil)
	if err != nil {
		log.Errorf("request failed, %s", err)

		return nil, err
	}
	// Parse response
	var v Visas
	err = json.NewDecoder(response.Body).Decode(&v)
	if err != nil {
		log.Errorf("failed to parse JSON response, %s", err)

		return nil, err
	}
	log.Debug("visas received")

	return &v, nil
}

// GetPermissions parses visas and finds matching dataset names from the database, returning a list of matches
var GetPermissions = func(visas Visas) []string {
	log.Debug("parsing permissions from visas")
	datasets := []string{} // default empty array

	log.Debugf("number of visas to check: %d", len(visas.Visa))

	// Iterate visas
	for _, v := range visas.Visa {

		// Check that visa is of type ControlledAccessGrants
		if checkVisaType(v, "ControlledAccessGrants") {
			// Check that visa is valid and return visa token
			verifiedVisa, valid := validateVisa(v)
			if valid {
				// Parse the dataset name out of the value field
				datasets = getDatasets(verifiedVisa, datasets)
			}
		}

	}

	log.Debugf("matched datasets: %s", datasets)

	return datasets
}

func checkVisaType(visa string, visaType string) bool {

	log.Debug("checking visa type")

	unknownToken, err := jwt.Parse([]byte(visa), jwt.WithVerify(false))
	if err != nil {
		log.Errorf("failed to parse visa, %s", err)

		return false
	}
	unknownTokenVisaClaim := unknownToken.PrivateClaims()["ga4gh_visa_v1"]
	unknownTokenVisa := Visa{}
	unknownTokenVisaClaimJSON, err := json.Marshal(unknownTokenVisaClaim)
	if err != nil {
		log.Errorf("failed to parse unknown visa claim: %s to JSON, with error: %s", unknownTokenVisaClaim, err)

		return false
	}
	err = json.Unmarshal(unknownTokenVisaClaimJSON, &unknownTokenVisa)
	if err != nil {
		log.Errorf("failed to parse unknown visa claim: %s to JSON, with error: %s", unknownTokenVisaClaim, err)

		return false
	}
	if unknownTokenVisa.Type != visaType {
		log.Debugf("visa is not of type: %s, skip", visaType)

		return false
	}
	log.Debug("visa type check passed")

	return true

}

func validateVisa(visa string) (jwt.Token, bool) {
	log.Debug("start visa validation")

	// Extract header from header.payload.signature
	log.Debug("start visa validation")
	// Extract header from header.payload.signature
	header, err := jws.Parse([]byte(visa))
	if err != nil {
		log.Errorf("failed to parse visa header, %s", err)

		return nil, false
	}
	// Extract payload from header.payload.signature
	payload, err := jwt.Parse([]byte(visa), jwt.WithVerify(false))
	if err != nil {
		log.Errorf("failed to parse visa header, %s", err)

		return nil, false
	}
	// Parse the jku key from header
	o := OIDCDetails{
		JWK: header.Signatures()[0].ProtectedHeaders().JWKSetURL(),
	}
	ok := validateTrustedIss(config.Config.OIDC.TrustedList, payload.Issuer(), o.JWK)
	// Verify that visa comes from a trusted issuer
	if !ok {
		log.Infof("combination of iss: %s and jku: %s is not trusted", payload.Issuer(), o.JWK)

		return nil, false
	}
	wl := config.Config.OIDC.Whitelist
	log.Debugf("whitelist: %v", wl)

	// Verify visa signature
	var verifiedVisa jwt.Token
	if wl != nil {
		verifiedVisa, err = jwt.Parse([]byte(visa), jwt.WithVerifyAuto(nil, jwk.WithHTTPClient(request.Client), jwk.WithFetchWhitelist(wl)))
		if err != nil {
			log.Errorf("failed to verify token signature of token %s, %s", visa, err)

			return nil, false
		}
	} else {
		verifiedVisa, err = VerifyJWT(o, visa)
		if err != nil {
			log.Errorf("failed to verify token signature of token %s, %s", visa, err)

			return nil, false
		}
	}

	// Validate visa claims, exp, iat, nbf
	if err := jwt.Validate(verifiedVisa); err != nil {
		log.Error("failed to validate visa")

		return nil, false
	}
	log.Debug("visa validated")

	return verifiedVisa, true
}

func getDatasets(parsedVisa jwt.Token, datasets []string) []string {
	visaClaim := parsedVisa.PrivateClaims()["ga4gh_visa_v1"]
	visa := Visa{}
	visaClaimJSON, err := json.Marshal(visaClaim)
	if err != nil {
		log.Errorf("failed to parse visa claim to JSON, %s, %s", err, visaClaim)

		return datasets
	}
	err = json.Unmarshal(visaClaimJSON, &visa)
	if err != nil {
		log.Errorf("failed to parse visa claim JSON into struct, %s, %s", err, visaClaimJSON)

		return datasets
	}
	exists, err := database.CheckDataset(visa.Dataset)
	if err != nil {
		log.Debugf("visa contained dataset %s which doesn't exist in this instance, skip", visa.Dataset)

		return datasets
	}
	if exists {
		log.Debugf("checking dataset list for duplicates of %s", visa.Dataset)
		// check that dataset name doesn't already exist in return list,
		// we can get duplicates when using multiple AAIs
		for i := range datasets {
			if datasets[i] == visa.Dataset {
				log.Debugf("found a duplicate: dataset %s is already found, skip", visa.Dataset)

				return datasets
			}
		}
		log.Debugf("no duplicates of dataset: %s, add dataset to list of permissions", visa.Dataset)
		datasets = append(datasets, visa.Dataset)
	}

	return datasets
}

// ValidateTrustedIss searches a nested list of TrustedISS
// looking for a map with a specific key value pair for iss.
// If found checkISS returns true
// if the list is nil it returns true, as the path for the trusted issue file was not set
func validateTrustedIss(obj []config.TrustedISS, issuerValue string, jkuValue string) bool {
	log.Debugf("check combination of iss: %s and jku: %s", issuerValue, jkuValue)
	if obj != nil {
		for _, value := range obj {
			if cleanURL(value.ISS) == cleanURL(issuerValue) && cleanURL(value.JKU) == cleanURL(jkuValue) {
				return true
			}
		}

		return false
	}

	return true
}

func cleanURL(clean string) string {
	u, _ := url.Parse(clean)
	u.Path = path.Clean(u.RequestURI())

	return u.String()
}

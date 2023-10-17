package userauth

import (
	"fmt"
	"net/http"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

// AlwaysAllow is an Authenticator that always authenticates
type AlwaysAllow struct{}

// NewAlwaysAllow returns a new AlwaysAllow authenticator.
func NewAlwaysAllow() *AlwaysAllow {
	return &AlwaysAllow{}
}

// Authenticate authenticates everyone.
func (u *AlwaysAllow) Authenticate(_ *http.Request) (jwt.Token, error) {
	return jwt.New(), nil
}

// AlwaysAllow is an Authenticator that always authenticates
type AlwaysDeny struct{}

// Authenticate does not authenticate anyone.
func (u *AlwaysDeny) Authenticate(_ *http.Request) (jwt.Token, error) {
	return nil, fmt.Errorf("denied")
}

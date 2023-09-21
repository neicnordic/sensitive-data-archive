package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/coreos/go-oidc"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/iris-contrib/middleware/cors"
	"github.com/kataras/iris/v12"
	"github.com/kataras/iris/v12/sessions"
	log "github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
)

type LoginOption struct {
	Name string
	URL  string
}

type OIDCData struct {
	S3Conf   map[string]string
	ElixirID ElixirIdentity
}

type AuthHandler struct {
	Config       *Config
	OAuth2Config oauth2.Config
	OIDCProvider *oidc.Provider
	htmlDir      string
	staticDir    string
}

func (auth AuthHandler) getInboxConfig(ctx iris.Context, authType string) {

	log.Infoln(ctx.Request().URL.Path)

	s := sessions.Get(ctx)
	s3conf := s.GetFlash(authType)
	if s3conf == nil {
		ctx.Redirect("/")

		return
	}
	s3cfmap := s3conf.(map[string]string)
	ctx.ResponseWriter().Header().Set("Content-Disposition", "attachment; filename=s3cmd.conf")
	s3c := "[default]\n"

	for k, v := range s3cfmap {
		entry := fmt.Sprintf("%s = %s\n", k, v)
		s3c += entry
	}

	_, err := io.Copy(ctx.ResponseWriter(), strings.NewReader(s3c))
	if err != nil {
		log.Error("Failed to write s3config response: ", err)

		return
	}
}

// getMain returns the index.html page
func (auth AuthHandler) getMain(ctx iris.Context) {

	ctx.ViewData("infoUrl", auth.Config.InfoURL)
	ctx.ViewData("infoText", auth.Config.InfoText)
	err := ctx.View("index.html")
	if err != nil {
		log.Error("Failed to view index page: ", err)

		return
	}
}

// getLoginOptions returns the available login providers as JSON
func (auth AuthHandler) getLoginOptions(ctx iris.Context) {

	// Elixir is always available
	response := []LoginOption{{Name: "Elixir", URL: "/elixir"}}
	// Only add the CEGA option if it has both id and secret
	if auth.Config.Cega.ID != "" && auth.Config.Cega.Secret != "" {
		response = append(response, LoginOption{Name: "EGA", URL: "/ega/login"})
	}
	err := ctx.JSON(response)
	if err != nil {
		log.Error("Failed to create JSON login options: ", err)

		return
	}
}

// postEGA handles post requests for logging in using EGA
func (auth AuthHandler) postEGA(ctx iris.Context) {

	s := sessions.Get(ctx)

	userform := ctx.FormValues()
	username := userform["username"][0]
	password := userform["password"][0]

	res, err := authenticateWithCEGA(auth.Config.Cega, username)

	if err != nil {
		log.Error(err)
	}

	defer res.Body.Close()

	switch res.StatusCode {
	case 200:
		if err != nil {
			log.Error(err)

			return
		}

		var ur CegaUserResponse
		err = json.NewDecoder(res.Body).Decode(&ur)

		if err != nil {
			log.Error("Failed to parse response: ", err)

			return
		}

		hash := ur.PasswordHash

		ok := verifyPassword(password, hash)

		if ok {
			log.WithFields(log.Fields{"authType": "cega", "user": username}).Info("Valid password entered by user")
			claims := &Claims{
				username,
				"",
				jwt.RegisteredClaims{
					IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
					Issuer:   auth.Config.JwtIssuer,
					Subject:  username,
				},
			}
			token, expDate, err := generateJwtToken(claims, auth.Config.JwtPrivateKey, auth.Config.JwtSignatureAlg)
			if err != nil {
				log.Errorf("error when generating token: %v", err)
			}

			s3conf := getS3ConfigMap(token, auth.Config.S3Inbox, username)
			idStruct := EGAIdentity{User: username, Token: token, ExpDate: expDate}
			s.SetFlash("ega", s3conf)
			err = ctx.View("ega.html", idStruct)
			if err != nil {
				log.Error("Failed to parse response: ", err)

				return
			}

		} else {
			log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Invalid password entered by user")
			s.SetFlash("message", "Provided credentials are not valid")
			ctx.Redirect("/ega/login", iris.StatusSeeOther)
		}

	case 404:
		log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Failed to authenticate user")
		s.SetFlash("message", "EGA authentication server could not be contacted")
		ctx.Redirect("/ega/login", iris.StatusSeeOther)

	default:
		log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Failed to authenticate user")
		s.SetFlash("message", "Provided credentials are not valid")
		ctx.Redirect("/ega/login", iris.StatusSeeOther)
	}
}

// getEGALogin returns the EGA login form
func (auth AuthHandler) getEGALogin(ctx iris.Context) {

	s := sessions.Get(ctx)
	message := s.GetFlashString("message")
	if message == "" {
		err := ctx.View("loginform.html")
		if err != nil {
			log.Error("Failed to return to login form: ", err)

			return
		}

		return
	}
	err := ctx.View("loginform.html", EGALoginError{Reason: message})
	if err != nil {
		log.Error("Failed to view invalid credentials form: ", err)

		return
	}
}

// getEGAConf returns an s3config file for an elixir login
func (auth AuthHandler) getEGAConf(ctx iris.Context) {
	auth.getInboxConfig(ctx, "ega")
}

// getElixir redirects to the elixir page defined in auth.Config
func (auth AuthHandler) getElixir(ctx iris.Context) {
	state := uuid.New()
	ctx.SetCookie(&http.Cookie{Name: "state", Value: state.String(), Secure: true})

	redirectURI := ctx.Request().URL.Query().Get("redirect_uri")
	if redirectURI != "" {
		redirectParam := oauth2.SetAuthURLParam("redirect_uri", redirectURI)
		ctx.Redirect(auth.OAuth2Config.AuthCodeURL(state.String(), redirectParam))
	} else {
		ctx.Redirect(auth.OAuth2Config.AuthCodeURL(state.String()))
	}
}

// elixirLogin authenticates the user with return values from the elixir
// login page and returns the resulting data to the getElixirLogin page, or
// getElixirCORSLogin endpoint.
func (auth AuthHandler) elixirLogin(ctx iris.Context) *OIDCData {
	state := ctx.Request().URL.Query().Get("state")
	sessionState := ctx.GetCookie("state")

	if state != sessionState {
		log.Errorf("State of incoming request (%s) does not match with your session's state (%s)", state, sessionState)
		_, err := ctx.Writef("Authentication failed. You may need to clear your session cookies and try again.")
		if err != nil {
			log.Error("Failed to write response: ", err)

			return nil
		}

		return nil
	}

	code := ctx.Request().URL.Query().Get("code")
	idStruct, err := authenticateWithOidc(auth.OAuth2Config, auth.OIDCProvider, code, auth.Config.Elixir.jwkURL)
	if err != nil {
		log.WithFields(log.Fields{"authType": "elixir"}).Errorf("authentication failed: %s", err)
		_, err := ctx.Writef("Authentication failed. You may need to clear your session cookies and try again.")
		if err != nil {
			log.Error("Failed to write response: ", err)

			return nil
		}

		return nil
	}

	if auth.Config.ResignJwt {
		claims := &Claims{
			idStruct.Email,
			"",
			jwt.RegisteredClaims{
				IssuedAt: jwt.NewNumericDate(time.Now().UTC()),
				Issuer:   auth.Config.JwtIssuer,
				Subject:  idStruct.User,
			},
		}
		token, expDate, err := generateJwtToken(claims, auth.Config.JwtPrivateKey, auth.Config.JwtSignatureAlg)
		if err != nil {
			log.Errorf("error when generating token: %v", err)
		}
		idStruct.Token = token
		idStruct.ExpDate = expDate
	}

	log.WithFields(log.Fields{"authType": "elixir", "user": idStruct.User}).Infof("User was authenticated")
	s3conf := getS3ConfigMap(idStruct.Token, auth.Config.S3Inbox, idStruct.User)

	return &OIDCData{S3Conf: s3conf, ElixirID: idStruct}
}

// getElixirLogin renders the `elixir.html` template to the given iris context
func (auth AuthHandler) getElixirLogin(ctx iris.Context) {

	oidcData := auth.elixirLogin(ctx)
	if oidcData == nil {
		return
	}

	s := sessions.Get(ctx)
	s.SetFlash("elixir", oidcData.S3Conf)
	err := ctx.View("elixir.html", oidcData.ElixirID)
	if err != nil {
		log.Error("Failed to view login form: ", err)

		return
	}
}

// getElixirCORSLogin returns the oidc data as JSON to the given iris context
func (auth AuthHandler) getElixirCORSLogin(ctx iris.Context) {

	oidcData := auth.elixirLogin(ctx)
	if oidcData == nil {
		return
	}

	err := ctx.JSON(oidcData)
	if err != nil {
		log.Error("Failed to view login form: ", err)

		return
	}
}

// getElixirConf returns an s3config file for an elixir login
func (auth AuthHandler) getElixirConf(ctx iris.Context) {
	auth.getInboxConfig(ctx, "elixir")
}

// globalHeaders presets common response headers
func globalHeaders(ctx iris.Context) {

	ctx.ResponseWriter().Header().Set("X-Content-Type-Options", "nosniff")
	ctx.Next()
}

// addCSPheaders implements CSP and recommended complementary policies
func addCSPheaders(ctx iris.Context) {

	ctx.ResponseWriter().Header().Set("Content-Security-Policy", "default-src 'self';"+
		"script-src-elem 'self';"+
		"img-src 'self' data:;"+
		"frame-ancestors 'none';"+
		"form-action 'self'")

	ctx.ResponseWriter().Header().Set("Referrer-Policy", "no-referrer")
	ctx.ResponseWriter().Header().Set("X-Frame-Options", "DENY") // legacy option, obsolete by CSP frame-ancestors in new browsers
	ctx.Next()
}

func main() {

	// Initialise config
	config, err := NewConfig()
	if err != nil {
		log.Errorf("Failed to generate config, reason: %v", err)
		os.Exit(1)
	}

	// Initialise OIDC client
	oauth2Config, provider := getOidcClient(config.Elixir)

	// Create handler struct for the web server
	authHandler := AuthHandler{
		Config:       config,
		OAuth2Config: oauth2Config,
		OIDCProvider: provider,
		htmlDir:      "./frontend/templates",
		staticDir:    "./frontend/static",
	}

	// Initialise web server
	app := iris.New()

	// Start sessions handler in order to send flash messages
	sess := sessions.New(sessions.Config{Cookie: "_session_id", AllowReclaim: true})

	if config.Server.CORS.AllowOrigin != "" {
		// Set CORS context
		corsContext := cors.New(cors.Options{
			AllowedOrigins:   strings.Split(config.Server.CORS.AllowOrigin, ","),
			AllowedMethods:   strings.Split(config.Server.CORS.AllowMethods, ","),
			AllowCredentials: config.Server.CORS.AllowCredentials,
		})
		app.Use(corsContext)
	}

	app.Use(sess.Handler())

	app.RegisterView(iris.HTML(authHandler.htmlDir, ".html"))
	app.HandleDir("/public", iris.Dir(authHandler.staticDir))

	app.Get("/", addCSPheaders, authHandler.getMain)
	app.Get("/login-options", authHandler.getLoginOptions)

	// EGA endpoints
	app.Post("/ega", authHandler.postEGA)
	app.Get("/ega/s3conf", authHandler.getEGAConf)
	app.Get("/ega/login", addCSPheaders, authHandler.getEGALogin)

	// Elixir endpoints
	app.Get("/elixir", authHandler.getElixir)
	app.Get("/elixir/s3conf", authHandler.getElixirConf)
	app.Get("/elixir/login", authHandler.getElixirLogin)
	app.Get("/elixir/cors_login", authHandler.getElixirCORSLogin)

	app.UseGlobal(globalHeaders)

	if config.Server.Cert != "" && config.Server.Key != "" {

		log.Infoln("Serving content using https")
		err = app.Run(iris.TLS("0.0.0.0:8080", config.Server.Cert, config.Server.Key))
	} else {

		log.Infoln("Serving content using http")
		server := &http.Server{
			Addr:              "0.0.0.0:8080",
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       30 * time.Second,
			ReadHeaderTimeout: 3 * time.Second,
		}
		err = app.Run(iris.Server(server))
	}
	if err != nil {
		log.Error("Failed to start server:", err)
	}
}

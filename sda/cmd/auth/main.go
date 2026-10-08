package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/neicnordic/sensitive-data-archive/internal/config"
	configv2 "github.com/neicnordic/sensitive-data-archive/internal/config/v2"
	"github.com/neicnordic/sensitive-data-archive/internal/database"
	"github.com/neicnordic/sensitive-data-archive/internal/database/postgres"
	"github.com/neicnordic/sensitive-data-archive/pkg/observability"
	"github.com/rs/cors"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
)

type LoginOption struct {
	Name string
	URL  string
}

type OIDCData struct {
	S3ConfInbox    map[string]string
	S3ConfDownload map[string]string
	OIDCID         OIDCIdentity
}

type AuthHandler struct {
	Config       config.AuthConf
	OAuth2Config oauth2.Config
	OIDCProvider *oidc.Provider
	htmlDir      string
	staticDir    string
	pubKey       string
	db           database.Database
	templates    *template.Template
	sessions     *sessionStore
}

// render executes the named template with data and writes the result. The
// page is rendered in full before anything is written, so a failed render
// leaves the response untouched for the caller to write something else.
func (auth AuthHandler) render(w http.ResponseWriter, name string, data map[string]any) error {
	var page bytes.Buffer
	if err := auth.templates.ExecuteTemplate(&page, name, data); err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := page.WriteTo(w); err != nil {
		log.Error("Failed to write page: ", err)
	}

	return nil
}

// writeJSON writes v as a JSON response.
func writeJSON(w http.ResponseWriter, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(body)

	return err
}

// getS3Config retrieves S3 config from session flash and serves it as a
// downloadable s3cmd file with the specified fileName. Redirects to home if
// config is missing.
func (auth AuthHandler) getS3Config(w http.ResponseWriter, r *http.Request, authType string, fileName string) {
	log.Infoln(r.URL.Path)

	s3cfmap, ok := auth.sessions.PopFlash(r, authType).(map[string]string)
	if !ok {
		http.Redirect(w, r, "/", http.StatusFound)

		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", fileName))

	s3c := "[default]\n"

	for k, v := range s3cfmap {
		entry := fmt.Sprintf("%s = %s\n", k, v)
		s3c += entry
	}

	_, err := io.Copy(w, strings.NewReader(s3c))
	if err != nil {
		log.Error("Failed to write s3config response: ", err)

		return
	}
}

// getMain returns the index.html page
func (auth AuthHandler) getMain(w http.ResponseWriter, _ *http.Request) {
	data := map[string]any{
		"infoUrl":  auth.Config.InfoURL,
		"infoText": auth.Config.InfoText,
	}
	err := auth.render(w, "index.html", data)
	if err != nil {
		log.Error("Failed to view index page: ", err)

		return
	}
}

// getLoginOptions returns the available login providers as JSON
func (auth AuthHandler) getLoginOptions(w http.ResponseWriter, _ *http.Request) {
	var response []LoginOption
	// Only add the OIDC option if it has both id and secret
	if auth.Config.OIDC.ID != "" && auth.Config.OIDC.Secret != "" {
		response = append(response, LoginOption{Name: "Lifescience-RI", URL: "/oidc"})
	}

	// Only add the CEGA option if it has both id and secret
	if auth.Config.Cega.ID != "" && auth.Config.Cega.Secret != "" {
		response = append(response, LoginOption{Name: "EGA", URL: "/ega/login"})
	}
	err := writeJSON(w, response)
	if err != nil {
		log.Error("Failed to create JSON login options: ", err)

		return
	}
}

// postEGA handles post requests for logging in using EGA
func (auth AuthHandler) postEGA(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	password := r.FormValue("password")

	res, err := authenticateWithCEGA(auth.Config.Cega, username)

	if err != nil {
		log.Errorf("No response from cega, error: %v", err)
		res = &http.Response{
			Body:       io.NopCloser(nil),
			StatusCode: http.StatusInternalServerError,
		}
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case 200:
		var ur CegaUserResponse
		err := json.NewDecoder(res.Body).Decode(&ur)

		if err != nil {
			log.Error("Failed to parse cega response: ", err)
			auth.sessions.SetFlash(w, r, "message", "Problems connecting to EGA authentication server")
			http.Redirect(w, r, "/ega/login", http.StatusSeeOther)

			return
		}

		hash := ur.PasswordHash

		ok := verifyPassword(password, hash)

		if ok {
			log.WithFields(log.Fields{"authType": "cega", "user": username}).Info("Valid password entered by user")
			claims := map[string]any{
				jwt.ExpirationKey: time.Now().UTC().Add(time.Duration(auth.Config.JwtTTL) * time.Hour),
				jwt.IssuedAtKey:   time.Now().UTC(),
				jwt.IssuerKey:     auth.Config.JwtIssuer,
				jwt.SubjectKey:    username,
			}
			token, expDate, err := generateJwtToken(claims, auth.Config.JwtPrivateKey, auth.Config.JwtSignatureAlg)
			if err != nil {
				log.Errorf("error when generating token: %v", err)
				auth.sessions.SetFlash(w, r, "message", "Unexpected error, please try again.")
				http.Redirect(w, r, "/ega/login", http.StatusSeeOther)

				return
			}

			s3conf := getS3ConfigMap(token, auth.Config.S3Inbox, username)
			auth.sessions.SetFlash(w, r, "ega", s3conf)

			data := map[string]any{
				"infoUrl":  auth.Config.InfoURL,
				"infoText": auth.Config.InfoText,
				"User":     username,
				"Token":    token,
				"ExpDate":  expDate,
			}
			err = auth.render(w, "ega.html", data)

			if err != nil {
				log.Error("Failed to create view: ", err)

				// Nothing has been written yet, so show an error message and
				// an opportunity to log in again instead.
				data["Reason"] = "Unexpected error, please try again."
				err = auth.render(w, "loginform.html", data)
				log.Error("Failed to create backup view: ", err)
			}
		} else {
			log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Invalid password entered by user")
			auth.sessions.SetFlash(w, r, "message", "Provided credentials are not valid")
			http.Redirect(w, r, "/ega/login", http.StatusSeeOther)
		}

	case 500, 502, 503:
		log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Failed to authenticate user")
		auth.sessions.SetFlash(w, r, "message", "EGA authentication server could not be contacted")
		http.Redirect(w, r, "/ega/login", http.StatusSeeOther)

	case 401:
		log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Failed to authenticate service (auth_cega_id/secret)")
		auth.sessions.SetFlash(w, r, "message", "Problems connecting to EGA authentication server")
		http.Redirect(w, r, "/ega/login", http.StatusSeeOther)
	default:
		log.WithFields(log.Fields{"authType": "cega", "user": username}).Error("Failed to authenticate user")
		auth.sessions.SetFlash(w, r, "message", "Provided credentials are not valid")
		http.Redirect(w, r, "/ega/login", http.StatusSeeOther)
	}
}

// getEGALogin returns the EGA login form
func (auth AuthHandler) getEGALogin(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"infoUrl":  auth.Config.InfoURL,
		"infoText": auth.Config.InfoText,
	}
	if message, _ := auth.sessions.PopFlash(r, "message").(string); message != "" {
		data["Reason"] = message
	}
	err := auth.render(w, "loginform.html", data)
	if err != nil {
		log.Error("Failed to view invalid credentials form: ", err)
	}
}

// getEGAConf returns an s3config file for an oidc login
func (auth AuthHandler) getEGAConf(w http.ResponseWriter, r *http.Request) {
	auth.getS3Config(w, r, "ega", "s3cmd-inbox.conf")
}

// getOIDC redirects to the oidc page defined in auth.Config
func (auth AuthHandler) getOIDC(w http.ResponseWriter, r *http.Request) {
	state := uuid.New()
	http.SetCookie(w, &http.Cookie{Name: "state", Value: state.String(), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})

	var authOptions []oauth2.AuthCodeOption

	redirectURI := r.URL.Query().Get("redirect_uri")
	if redirectURI != "" {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("redirect_uri", redirectURI))
	}

	// Ask the provider for the required authentication context, e.g. multi
	// factor authentication. The returned acr is verified in elixirLogin.
	if len(auth.Config.OIDC.AcrValues) > 0 {
		authOptions = append(authOptions, oauth2.SetAuthURLParam("acr_values", strings.Join(auth.Config.OIDC.AcrValues, " ")))
	}

	http.Redirect(w, r, auth.OAuth2Config.AuthCodeURL(state.String(), authOptions...), http.StatusFound)
}

// elixirLogin authenticates the user with return values from the oidc
// login page and returns the resulting data to the getOIDCLogin page, or
// getOIDCCORSLogin endpoint.
func (auth AuthHandler) elixirLogin(w http.ResponseWriter, r *http.Request) *OIDCData {
	state := r.URL.Query().Get("state")
	var sessionState string
	if cookie, err := r.Cookie("state"); err == nil {
		sessionState = cookie.Value
	}

	if state != sessionState {
		log.Errorf("State of incoming request (%s) does not match with your session's state (%s)", state, sessionState)
		_, err := fmt.Fprint(w, "Authentication failed. You may need to clear your session cookies and try again.")
		if err != nil {
			log.Error("Failed to write response: ", err)

			return nil
		}

		return nil
	}

	// The provider reports a refused authorization by redirecting back with an
	// error instead of a code. Without this the empty code would be exchanged,
	// and the provider's description of what went wrong would be lost.
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		description := r.URL.Query().Get("error_description")
		log.WithFields(log.Fields{"authType": "oidc"}).Errorf("provider refused the authorization request: %s (%s)", providerError, description)
		// The error code comes from the query string, so pin the content type
		// rather than leave it to be sniffed, and quote what is echoed back.
		w.Header().Set("Content-Type", "text/plain")
		if _, err := fmt.Fprintf(w, "Authentication failed. The login provider refused the request: %q", providerError); err != nil {
			log.Error("Failed to write response: ", err)
		}

		return nil
	}

	code := r.URL.Query().Get("code")
	idStruct, err := authenticateWithOidc(auth.OAuth2Config, auth.OIDCProvider, code, auth.Config.OIDC)
	if err != nil {
		log.WithFields(log.Fields{"authType": "oidc"}).Errorf("authentication failed: %s", err)
		_, err := fmt.Fprintf(w, "%s", loginFailureMessage(err))
		if err != nil {
			log.Error("Failed to write response: ", err)

			return nil
		}

		return nil
	}
	err = auth.db.UpdateUserInfo(r.Context(), idStruct.User, idStruct.Fullname, idStruct.Email, idStruct.EdupersonEntitlement)
	if err != nil {
		log.Warn("Could not log user info.")
	}

	if auth.Config.ResignJwt {
		log.Debugf("Resigning token for user %s", idStruct.User)
		claims := map[string]any{
			jwt.ExpirationKey: time.Now().UTC().Add(time.Duration(auth.Config.JwtTTL) * time.Hour),
			jwt.IssuedAtKey:   time.Now().UTC(),
			jwt.IssuerKey:     auth.Config.JwtIssuer,
			jwt.SubjectKey:    idStruct.User,
		}
		token, expDate, err := generateJwtToken(claims, auth.Config.JwtPrivateKey, auth.Config.JwtSignatureAlg)
		if err != nil {
			log.Errorf("error when generating token: %v", err)
		}
		idStruct.ResignedToken = token
		idStruct.ExpDateResigned = expDate
	}

	log.WithFields(log.Fields{"authType": "oidc", "user": idStruct.User}).Infof("User was authenticated")
	s3confInbox := getS3ConfigMap(idStruct.ResignedToken, auth.Config.S3Inbox, idStruct.User)
	s3confDownload := getS3ConfigMap(idStruct.RawToken, auth.Config.S3Inbox, idStruct.User)

	return &OIDCData{S3ConfInbox: s3confInbox, S3ConfDownload: s3confDownload, OIDCID: idStruct}
}

// loginFailureMessage returns the message shown to a user whose login failed.
// A rejected authentication context gets its own message, since telling the
// user to clear their cookies is useless advice when the real problem is that
// they did not log in with the method the service requires.
func loginFailureMessage(err error) string {
	if errors.Is(err, ErrAcrNotAccepted) {
		return "Authentication failed. This service requires you to log in with a stronger " +
			"authentication method, for example two factor authentication. Please log in again " +
			"using the required method."
	}

	return "Authentication failed. You may need to clear your session cookies and try again."
}

// getOIDCLogin renders the `oidc.html` template
func (auth AuthHandler) getOIDCLogin(w http.ResponseWriter, r *http.Request) {
	oidcData := auth.elixirLogin(w, r)
	if oidcData == nil {
		return
	}

	auth.sessions.SetFlash(w, r, "oidcInbox", oidcData.S3ConfInbox)
	auth.sessions.SetFlash(w, r, "oidcDownload", oidcData.S3ConfDownload)
	data := map[string]any{
		"cegaID":          auth.Config.Cega.ID,
		"infoUrl":         auth.Config.InfoURL,
		"infoText":        auth.Config.InfoText,
		"User":            oidcData.OIDCID.User,
		"Fullname":        oidcData.OIDCID.Fullname,
		"Passport":        oidcData.OIDCID.Passport,
		"RawToken":        oidcData.OIDCID.RawToken,
		"ResignedToken":   oidcData.OIDCID.ResignedToken,
		"ExpDateRaw":      oidcData.OIDCID.ExpDateRaw,
		"ExpDateResigned": oidcData.OIDCID.ExpDateResigned,
	}

	err := auth.render(w, "oidc.html", data)
	if err != nil {
		log.Error("Failed to view login form: ", err)

		return
	}
}

// getOIDCCORSLogin returns the oidc data as JSON
func (auth AuthHandler) getOIDCCORSLogin(w http.ResponseWriter, r *http.Request) {
	oidcData := auth.elixirLogin(w, r)
	if oidcData == nil {
		return
	}

	err := writeJSON(w, oidcData)
	if err != nil {
		log.Error("Failed to view login form: ", err)

		return
	}
}

// getOIDCConfInbox returns an s3config file for uploading to the Inbox
func (auth AuthHandler) getOIDCConfInbox(w http.ResponseWriter, r *http.Request) {
	auth.getS3Config(w, r, "oidcInbox", "s3cmd-inbox.conf")
}

// getOIDCConfDownload returns an s3config file for downloading from the Archive
func (auth AuthHandler) getOIDCConfDownload(w http.ResponseWriter, r *http.Request) {
	auth.getS3Config(w, r, "oidcDownload", "s3cmd-download.conf")
}

// globalHeaders presets common response headers
func globalHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// addCSPheaders implements CSP and recommended complementary policies
func addCSPheaders(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self';"+
			"script-src-elem 'self';"+
			"img-src 'self' data:;"+
			"frame-ancestors 'none';"+
			"form-action 'self'")

		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY") // legacy option, obsolete by CSP frame-ancestors in new browsers
		next(w, r)
	}
}

// rejectHead answers HEAD requests with 405. ServeMux hands them to the GET
// handlers, and some of those have side effects, like handing out a one-shot
// s3cmd config or exchanging a login code. Iris did not route HEAD at all.
func rejectHead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}
		next.ServeHTTP(w, r)
	})
}

// filesOnly is a file system that hides directories, so that the static file
// server does not list them.
type filesOnly struct {
	fs http.FileSystem
}

func (f filesOnly) Open(name string) (http.File, error) {
	file, err := f.fs.Open(name)
	if err != nil {
		return nil, err
	}

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		_ = file.Close()

		return nil, os.ErrNotExist
	}

	return file, nil
}

// corsHandler applies the CORS configuration with rs/cors, and keeps two
// things the Iris middleware it replaces did: requests from an origin, or with
// a method, that is not allowed are refused with 403 before they reach a
// handler, and a wildcard origin together with credentials echoes the
// requesting origin, since browsers refuse "*" for credentialed requests.
func corsHandler(conf config.CORSConfig, next http.Handler) http.Handler {
	origins := strings.Split(conf.AllowOrigin, ",")
	methods := strings.Split(strings.ToUpper(conf.AllowMethods), ",")
	options := cors.Options{
		AllowedOrigins:       origins,
		AllowedMethods:       methods,
		AllowCredentials:     conf.AllowCredentials,
		OptionsSuccessStatus: http.StatusOK,
	}
	if conf.AllowCredentials && slices.Contains(origins, "*") {
		options.AllowOriginFunc = func(string) bool { return true }
	}
	c := cors.New(options)
	handler := c.Handler(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			method := r.Method
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				method = r.Header.Get("Access-Control-Request-Method")
			}
			method = strings.ToUpper(method)
			if !c.OriginAllowed(r) || (method != http.MethodOptions && !slices.Contains(methods, method)) {
				w.Header().Add("Vary", "Origin")
				http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)

				return
			}
		}
		handler.ServeHTTP(w, r)
	})
}

// router returns the handler serving all endpoints of the service.
func (auth AuthHandler) router(corsConf config.CORSConfig) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /public/", http.StripPrefix("/public/", http.FileServer(filesOnly{http.Dir(auth.staticDir)})))

	mux.HandleFunc("GET /{$}", addCSPheaders(auth.getMain))
	mux.HandleFunc("GET /login-options", auth.getLoginOptions)

	// EGA endpoints
	mux.HandleFunc("POST /ega", auth.postEGA)
	mux.HandleFunc("GET /ega/s3conf", auth.getEGAConf)
	mux.HandleFunc("GET /ega/login", addCSPheaders(auth.getEGALogin))

	// OIDC endpoints
	mux.HandleFunc("GET /oidc", auth.getOIDC)
	mux.HandleFunc("GET /oidc/s3conf-inbox", auth.getOIDCConfInbox)
	mux.HandleFunc("GET /oidc/s3conf-download", auth.getOIDCConfDownload)
	mux.HandleFunc("GET /oidc/login", auth.getOIDCLogin)
	mux.HandleFunc("GET /oidc/cors_login", auth.getOIDCCORSLogin)

	// Endpoint for client login info
	mux.HandleFunc("GET /info", auth.getInfo)

	handler := globalHeaders(rejectHead(mux))

	if corsConf.AllowOrigin != "" {
		handler = corsHandler(corsConf, handler)
	}

	return otelhttp.NewMiddleware("http-server")(handler)
}

func main() {
	// Initialise config
	if err := configv2.Load(); err != nil {
		log.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}

	conf, err := config.NewConfig("auth")
	if err != nil {
		log.Errorf("Failed to generate config, reason: %v", err)
		os.Exit(1)
	}

	ctx := context.Background()

	shutdown, err := observability.SetupOTelSDK(ctx, "sda-auth")
	if err != nil {
		log.Errorf("failed to setup OTel SDK: %v", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		if err := shutdown(shutdownCtx); err != nil {
			slog.Error("failed to shutdown OTel SDK", "err", err)
		}
		shutdownCancel()
	}()
	ctx, startupSpan := observability.StartSpan(ctx, "start up")
	defer startupSpan.End()

	var oauth2Config oauth2.Config
	var provider *oidc.Provider

	if conf.Auth.OIDC.ID != "" && conf.Auth.OIDC.Secret != "" {
		// Initialise OIDC client
		oauth2Config, provider = getOidcClient(conf.Auth.OIDC)
	}

	// Create handler struct for the web server
	authHandler := AuthHandler{
		Config:       conf.Auth,
		OAuth2Config: oauth2Config,
		OIDCProvider: provider,
		htmlDir:      "./frontend/templates",
		staticDir:    "./frontend/static",
		pubKey:       "",
		// Sessions only carry flash messages from one request to the next
		sessions: newSessionStore(10 * time.Minute),
	}
	go authHandler.sessions.expireLoop(ctx)

	authHandler.templates, err = template.ParseGlob(filepath.Join(authHandler.htmlDir, "*.html"))
	if err != nil {
		log.Panicf("Failed to parse templates: %s", err.Error())
	}

	// Connect to DB
	authHandler.db, err = postgres.NewPostgresSQLDatabase(ctx)
	if err != nil {
		log.Error(err)
		panic(err)
	}
	dbSchemaVersion, err := authHandler.db.SchemaVersion()
	if err != nil {
		log.Errorf("database connection issue: %v", err)
		panic(err)
	}
	if dbSchemaVersion < 14 {
		err := fmt.Errorf("database schema v14 is required, current: %d", dbSchemaVersion)
		log.Error(err.Error())
		panic(err)
	}
	defer authHandler.db.Close()

	authHandler.pubKey, err = readPublicKeyFile(authHandler.Config.PublicFile)
	if err != nil {
		log.Panicf("Failed to read public key: %s", err.Error())
	}

	server := &http.Server{
		Addr:              "0.0.0.0:8080",
		Handler:           authHandler.router(conf.Server.CORS),
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		ReadHeaderTimeout: 3 * time.Second,
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		var err error
		if conf.Server.Cert != "" && conf.Server.Key != "" {
			log.Infoln("Serving content using https")
			err = server.ListenAndServeTLS(conf.Server.Cert, conf.Server.Key)
		} else {
			log.Infoln("Serving content using http")
			err = server.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	startupSpan.End()

	select {
	case sig := <-sigc:
		log.Infof("received shutdown signal: %s", sig)
	case err := <-serverErr:
		log.Error("Failed to start server:", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Errorf("server shutdown error: %v", err)
	}
}

package rbac

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/suite"
)

type RbacTestSuite struct {
	suite.Suite

	tempDir string
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(RbacTestSuite))
}

type mockAuthenticator struct {
	user string
}

func (a *mockAuthenticator) authenticate(c *gin.Context) {
	token := jwt.New()
	if err := token.Set("sub", a.user); err != nil {
		c.AbortWithStatus(http.StatusUnauthorized)

		return
	}
	c.Set("token", token)
}

func (ts *RbacTestSuite) SetupSuite() {
	ts.tempDir = ts.T().TempDir()

	if err := os.WriteFile(filepath.Join(ts.tempDir, "rbac.policy"), []byte(`
{
  "policy": [
	{
	  "role": "admin",
	  "path": "/admin/test",
	  "action": "(GET)|(POST)"
	},{
	  "role": "submitter",
	  "path": "/submitter/test",
	  "action": "(GET)|(POST)"
	}
  ],
  "roles": [
	{
	  "role": "admin",
	  "rolebinding": "submission"
	},
	{
	  "role": "admin_test_user",
	  "rolebinding": "admin"
	}, {
	  "role": "submitter_test_user",
	  "rolebinding": "submitter"
	}
  ]
}`), 0600); err != nil {
		ts.FailNow("failed to write rbac policy file due to %v", err)
	}
}

func setupGinEngine(mockAuthMiddleware, rbacMiddleware func(c *gin.Context)) *gin.Engine {
	ginEngine := gin.New()
	ginEngine.Use(mockAuthMiddleware, rbacMiddleware)

	ginEngine.GET("/admin/test", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusOK)
	})
	ginEngine.GET("/submitter/test", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusOK)
	})

	return ginEngine
}

func (ts *RbacTestSuite) TestRbac_AdminUser() {
	ma := &mockAuthenticator{"admin_test_user"}
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(ma.authenticate, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/test", nil)

	ginEngine.ServeHTTP(w, req)

	ts.Equal(http.StatusOK, w.Code)
}

func (ts *RbacTestSuite) TestRbac_SubmitterUser_AdminEndpoint() {
	ma := &mockAuthenticator{"submitter_test_user"}
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(ma.authenticate, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/admin/test", nil)

	ginEngine.ServeHTTP(w, req)
	ts.Equal(http.StatusUnauthorized, w.Code)
}

func (ts *RbacTestSuite) TestRbac_SubmitterUser() {
	ma := &mockAuthenticator{"submitter_test_user"}
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(ma.authenticate, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/submitter/test", nil)

	ginEngine.ServeHTTP(w, req)
	ts.Equal(http.StatusOK, w.Code)
}

func (ts *RbacTestSuite) TestRbac_UnknownUser_SubmitterEndpoint() {
	ma := &mockAuthenticator{"unknown"}
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(ma.authenticate, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/submitter/test", nil)

	ginEngine.ServeHTTP(w, req)
	ts.Equal(http.StatusUnauthorized, w.Code)
}

func (ts *RbacTestSuite) TestRbac_NoTokenInContext() {
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(func(c *gin.Context) {
		c.Next()
	}, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/submitter/test", nil)

	ginEngine.ServeHTTP(w, req)
	ts.Equal(http.StatusUnauthorized, w.Code)
}

func (ts *RbacTestSuite) TestRbac_UnknownEndpoint() {
	ma := &mockAuthenticator{"submitter_test_user"}
	rbac, err := NewRbac(
		RbacPolicyFilePath(filepath.Join(ts.tempDir, "rbac.policy")),
	)
	if err != nil {
		ts.FailNow(err.Error())
	}

	ginEngine := setupGinEngine(ma.authenticate, rbac.Enforce())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/unknown/test", nil)

	ginEngine.ServeHTTP(w, req)
	ts.Equal(http.StatusUnauthorized, w.Code)
}

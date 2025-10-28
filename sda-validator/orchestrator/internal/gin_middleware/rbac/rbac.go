package rbac

import (
	"net/http"
	"os"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwt"
	log "github.com/sirupsen/logrus"
)

type Rbac interface {
	Enforce() gin.HandlerFunc
}

type rbac struct {
	e *casbin.Enforcer
}

func NewRbac() (Rbac, error) {

	rbacPolicy, err := os.ReadFile(rbacPolicyFilePath)
	if err != nil {
		return nil, err
	}

	m, _ := model.NewModelFromString(RbacModel)
	casbinEnforcer, err := casbin.NewEnforcer(m, NewAdapter(rbacPolicy))
	if err != nil {
		return nil, err
	}

	return &rbac{e: casbinEnforcer}, nil
}

func (rbac *rbac) Enforce() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, ok := c.Get("token")
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		if _, ok := token.(jwt.Token); !ok {
			log.Warnf("token from gin context is not a jwt.Token")
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		ok, err := rbac.e.Enforce(token.(jwt.Token).Subject(), c.Request.URL.Path, c.Request.Method)
		if err != nil {
			log.Debugf("rbac enforcement failed, reason: %s", err.Error())
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "not authorized"})
			return
		}

		c.Next()
	}
}

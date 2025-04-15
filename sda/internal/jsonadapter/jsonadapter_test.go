package jsonadapter

import (
	"fmt"
	"os"
	"testing"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type AdapterTestSuite struct {
	suite.Suite
	Model          model.Model
	EmptyPolicy    []byte
	ExpectedGroups [][]string
	ExpectedPolicy [][]string
	DefaultPolicy  []byte
	File           *os.File
}

func TestConfigTestSuite(t *testing.T) {
	suite.Run(t, new(AdapterTestSuite))
}

func (suite *AdapterTestSuite) SetupSuite() {
	suite.Model, _ = model.NewModelFromString(Model)
	suite.EmptyPolicy = []byte(`{"policy":[],"roles":[]}`)
	suite.DefaultPolicy = []byte(`{"policy":[{"role":"admin","path":"/keys/*","action":"(GET)|(POST)|(PUT)"},
		{"role":"submission","path":"/dataset/create","action":"POST"},
		{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
		{"role":"submission","path":"/file/ingest","action":"POST"},
		{"role":"submission","path":"/file/accession","action":"POST"}],
		"roles":[{"role":"admin","rolebinding":"submission"},
		{"role":"dummy@example.org","rolebinding":"admin"},
		{"role":"foo@example.org","rolebinding":"submission"}]}`)
	suite.ExpectedPolicy = [][]string{{"admin", "/keys/*", "(GET)|(POST)|(PUT)"},
		{"submission", "/dataset/create", "POST"},
		{"submission", "/dataset/release/*dataset", "POST"},
		{"submission", "/file/ingest", "POST"},
		{"submission", "/file/accession", "POST"}}
	suite.ExpectedGroups = [][]string{{"admin", "submission"}, {"dummy@example.org", "admin"}, {"foo@example.org", "submission"}}

	suite.File, _ = os.CreateTemp("", "policy")
	_, err := suite.File.Write(suite.DefaultPolicy)
	if err != nil {
		suite.FailNow("failed to write policy file to disk")
	}
}

func (suite *AdapterTestSuite) TearDownSuite() {
	os.RemoveAll(suite.File.Name())
}

func (suite *AdapterTestSuite) TestAdapter_empty() {
	a := NewAdapter(&suite.EmptyPolicy)
	e, err := casbin.NewEnforcer(suite.Model, a)
	assert.NoError(suite.T(), err, "New enforcer failed on empty policy")
	p, err := e.GetPolicy()
	assert.NoError(suite.T(), err, "failed to get policy")
	assert.Equal(suite.T(), [][]string(nil), p)
}

func (suite *AdapterTestSuite) TestAdapter() {
	a := NewAdapter(&suite.DefaultPolicy)
	e, err := casbin.NewEnforcer(suite.Model, a)
	assert.NoError(suite.T(), err, "New enforcer failed withpolicy")
	p, err := e.GetPolicy()
	assert.Equal(suite.T(), len(suite.ExpectedPolicy), len(p))
	assert.NoError(suite.T(), err, "failed to get policy")
	assert.True(suite.T(), util.Array2DEquals(suite.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, suite.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(suite.T(), err, "failed to get groups")
	assert.True(suite.T(), util.Array2DEquals(suite.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, suite.ExpectedGroups))
}

func (suite *AdapterTestSuite) TestAdapter_fromFile() {
	b, err := os.ReadFile(suite.File.Name())
	assert.NoError(suite.T(), err, "failed to read json file")
	e, err := casbin.NewEnforcer(suite.Model, NewAdapter(&b))
	assert.NoError(suite.T(), err, "New enforcer failed with policy from file: %s", suite.File.Name())
	p, err := e.GetPolicy()
	assert.Equal(suite.T(), len(suite.ExpectedPolicy), len(p))
	assert.NoError(suite.T(), err, "failed to get policy")
	assert.True(suite.T(), util.Array2DEquals(suite.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, suite.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(suite.T(), err, "failed to get groups")
	assert.True(suite.T(), util.Array2DEquals(suite.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, suite.ExpectedGroups))
}

func (suite *AdapterTestSuite) TestAdapter_save() {
	a := NewAdapter(&suite.EmptyPolicy)
	e, err := casbin.NewEnforcer(suite.Model, a)
	assert.NoError(suite.T(), err, "New enforcer failed without policy")

	_, err = e.AddPolicy("foo", "/data/*", "GET")
	assert.NoError(suite.T(), err, "failed to add policy")

	_, err = e.AddPolicy("foo", "/data/:value", "POST")
	assert.NoError(suite.T(), err, "failed to add policy")

	assert.NoError(suite.T(), e.SavePolicy(), "failed to save policy")
	assert.Equal(suite.T(), 2, len(a.policy))
}

func (suite *AdapterTestSuite) TestAdapter_notImplemented() {
	a := NewAdapter(&suite.EmptyPolicy)
	assert.Error(suite.T(), a.AddPolicy("", "", []string{""}))
	assert.Error(suite.T(), a.RemovePolicy("", "", []string{""}))
	assert.Error(suite.T(), a.RemoveFilteredPolicy("", "", 0, ""))
}

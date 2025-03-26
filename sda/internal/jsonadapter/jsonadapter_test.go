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

func (ats *AdapterTestSuite) SetupSuite() {
	ats.Model, _ = model.NewModelFromString(Model)
	ats.EmptyPolicy = []byte(`{"policy":[],"roles":[]}`)
	ats.DefaultPolicy = []byte(`{"policy":[{"role":"admin","path":"/keys/*","action":"(GET)|(POST)|(PUT)"},
		{"role":"submission","path":"/dataset/create","action":"POST"},
		{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
		{"role":"submission","path":"/file/ingest","action":"POST"},
		{"role":"submission","path":"/file/accession","action":"POST"}],
		"roles":[{"role":"admin","rolebinding":"submission"},
		{"role":"dummy@example.org","rolebinding":"admin"},
		{"role":"foo@example.org","rolebinding":"submission"}]}`)
	ats.ExpectedPolicy = [][]string{{"admin", "/keys/*", "(GET)|(POST)|(PUT)"},
		{"submission", "/dataset/create", "POST"},
		{"submission", "/dataset/release/*dataset", "POST"},
		{"submission", "/file/ingest", "POST"},
		{"submission", "/file/accession", "POST"}}
	ats.ExpectedGroups = [][]string{{"admin", "submission"}, {"dummy@example.org", "admin"}, {"foo@example.org", "submission"}}

	ats.File, _ = os.CreateTemp("", "policy")
	_, err := ats.File.Write(ats.DefaultPolicy)
	if err != nil {
		ats.FailNow("failed to write policy file to disk")
	}
}

func (ats *AdapterTestSuite) TearDownSuite() {
	os.RemoveAll(ats.File.Name())
}

func (ats *AdapterTestSuite) TestAdapter_empty() {
	a := NewAdapter(&ats.EmptyPolicy)
	e, err := casbin.NewEnforcer(ats.Model, a)
	assert.NoError(ats.T(), err, "New enforcer failed on empty policy")
	p, err := e.GetPolicy()
	assert.NoError(ats.T(), err, "failed to get policy")
	assert.Equal(ats.T(), [][]string(nil), p)
}

func (ats *AdapterTestSuite) TestAdapter() {
	a := NewAdapter(&ats.DefaultPolicy)
	e, err := casbin.NewEnforcer(ats.Model, a)
	assert.NoError(ats.T(), err, "New enforcer failed withpolicy")
	p, err := e.GetPolicy()
	assert.Equal(ats.T(), len(ats.ExpectedPolicy), len(p))
	assert.NoError(ats.T(), err, "failed to get policy")
	assert.True(ats.T(), util.Array2DEquals(ats.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, ats.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(ats.T(), err, "failed to get groups")
	assert.True(ats.T(), util.Array2DEquals(ats.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, ats.ExpectedGroups))
}

func (ats *AdapterTestSuite) TestAdapter_fromFile() {
	b, err := os.ReadFile(ats.File.Name())
	assert.NoError(ats.T(), err, "failed to read json file")
	e, err := casbin.NewEnforcer(ats.Model, NewAdapter(&b))
	assert.NoError(ats.T(), err, "New enforcer failed with policy from file: %s", ats.File.Name())
	p, err := e.GetPolicy()
	assert.Equal(ats.T(), len(ats.ExpectedPolicy), len(p))
	assert.NoError(ats.T(), err, "failed to get policy")
	assert.True(ats.T(), util.Array2DEquals(ats.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, ats.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(ats.T(), err, "failed to get groups")
	assert.True(ats.T(), util.Array2DEquals(ats.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, ats.ExpectedGroups))
}

func (ats *AdapterTestSuite) TestAdapter_save() {
	a := NewAdapter(&ats.EmptyPolicy)
	e, err := casbin.NewEnforcer(ats.Model, a)
	assert.NoError(ats.T(), err, "New enforcer failed without policy")

	_, err = e.AddPolicy("foo", "/data/*", "GET")
	assert.NoError(ats.T(), err, "failed to add policy")

	_, err = e.AddPolicy("foo", "/data/:value", "POST")
	assert.NoError(ats.T(), err, "failed to add policy")

	assert.NoError(ats.T(), e.SavePolicy(), "failed to save policy")
	assert.Equal(ats.T(), 2, len(a.policy))
}

func (ats *AdapterTestSuite) TestAdapter_notImplemented() {
	a := NewAdapter(&ats.EmptyPolicy)
	assert.Error(ats.T(), a.AddPolicy("", "", []string{""}))
	assert.Error(ats.T(), a.RemovePolicy("", "", []string{""}))
	assert.Error(ats.T(), a.RemoveFilteredPolicy("", "", 0, ""))
}

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

func (ts *AdapterTestSuite) SetupSuite() {
	ts.Model, _ = model.NewModelFromString(Model)
	ts.EmptyPolicy = []byte(`{"policy":[],"roles":[]}`)
	ts.DefaultPolicy = []byte(`{"policy":[{"role":"admin","path":"/keys/*","action":"(GET)|(POST)|(PUT)"},
		{"role":"submission","path":"/dataset/create","action":"POST"},
		{"role":"submission","path":"/dataset/release/*dataset","action":"POST"},
		{"role":"submission","path":"/file/ingest/:fileid","action":"POST"},
		{"role":"submission","path":"/file/accession","action":"POST"}],
		"roles":[{"role":"admin","rolebinding":"submission"},
		{"role":"dummy@example.org","rolebinding":"admin"},
		{"role":"foo@example.org","rolebinding":"submission"}]}`)
	ts.ExpectedPolicy = [][]string{{"admin", "/keys/*", "(GET)|(POST)|(PUT)"},
		{"submission", "/dataset/create", "POST"},
		{"submission", "/dataset/release/*dataset", "POST"},
		{"submission", "/file/ingest/:fileid", "POST"},
		{"submission", "/file/accession", "POST"}}
	ts.ExpectedGroups = [][]string{{"admin", "submission"}, {"dummy@example.org", "admin"}, {"foo@example.org", "submission"}}

	ts.File, _ = os.CreateTemp("", "policy")
	_, err := ts.File.Write(ts.DefaultPolicy)
	if err != nil {
		ts.FailNow("failed to write policy file to disk")
	}
}

func (ts *AdapterTestSuite) TearDownSuite() {
	os.RemoveAll(ts.File.Name())
}

func (ts *AdapterTestSuite) TestAdapter_empty() {
	a := NewAdapter(&ts.EmptyPolicy)
	e, err := casbin.NewEnforcer(ts.Model, a)
	assert.NoError(ts.T(), err, "New enforcer failed on empty policy")
	p, err := e.GetPolicy()
	assert.NoError(ts.T(), err, "failed to get policy")
	assert.Equal(ts.T(), [][]string(nil), p)
}

func (ts *AdapterTestSuite) TestAdapter() {
	a := NewAdapter(&ts.DefaultPolicy)
	e, err := casbin.NewEnforcer(ts.Model, a)
	assert.NoError(ts.T(), err, "New enforcer failed withpolicy")
	p, err := e.GetPolicy()
	assert.Equal(ts.T(), len(ts.ExpectedPolicy), len(p))
	assert.NoError(ts.T(), err, "failed to get policy")
	assert.True(ts.T(), util.Array2DEquals(ts.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, ts.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(ts.T(), err, "failed to get groups")
	assert.True(ts.T(), util.Array2DEquals(ts.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, ts.ExpectedGroups))
}

func (ts *AdapterTestSuite) TestAdapter_fromFile() {
	b, err := os.ReadFile(ts.File.Name())
	assert.NoError(ts.T(), err, "failed to read json file")
	e, err := casbin.NewEnforcer(ts.Model, NewAdapter(&b))
	assert.NoError(ts.T(), err, "New enforcer failed with policy from file: %s", ts.File.Name())
	p, err := e.GetPolicy()
	assert.Equal(ts.T(), len(ts.ExpectedPolicy), len(p))
	assert.NoError(ts.T(), err, "failed to get policy")
	assert.True(ts.T(), util.Array2DEquals(ts.ExpectedPolicy, p), fmt.Sprintf("Policy: %v, supposed to be %v", p, ts.ExpectedPolicy))

	g, err := e.GetGroupingPolicy()
	assert.NoError(ts.T(), err, "failed to get groups")
	assert.True(ts.T(), util.Array2DEquals(ts.ExpectedGroups, g), fmt.Sprintf("Groups: %v, supposed to be %v", g, ts.ExpectedGroups))
}

func (ts *AdapterTestSuite) TestAdapter_save() {
	a := NewAdapter(&ts.EmptyPolicy)
	e, err := casbin.NewEnforcer(ts.Model, a)
	assert.NoError(ts.T(), err, "New enforcer failed without policy")

	_, err = e.AddPolicy("foo", "/data/*", "GET")
	assert.NoError(ts.T(), err, "failed to add policy")

	_, err = e.AddPolicy("foo", "/data/:value", "POST")
	assert.NoError(ts.T(), err, "failed to add policy")

	assert.NoError(ts.T(), e.SavePolicy(), "failed to save policy")
	assert.Equal(ts.T(), 2, len(a.policy))
}

func (ts *AdapterTestSuite) TestAdapter_notImplemented() {
	a := NewAdapter(&ts.EmptyPolicy)
	assert.Error(ts.T(), a.AddPolicy("", "", []string{""}))
	assert.Error(ts.T(), a.RemovePolicy("", "", []string{""}))
	assert.Error(ts.T(), a.RemoveFilteredPolicy("", "", 0, ""))
}

// Custom JOSN parser for the RBAC library used by the api

package rbac

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

type Adapter struct {
	source []byte
	policy []CasbinRule
}

type CasbinRule struct {
	PType string
	V0    string
	V1    string
	V2    string
}

type jsonPolicy struct {
	Policy []policy
	Roles  []roles
}

type policy struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	Role   string `json:"role"`
}

type roles struct {
	Role        string `json:"role"`
	RoleBinding string `json:"rolebinding"`
}

const RbacModel = `[request_definition]
r = sub, obj, act
[policy_definition]
p = sub, obj, act
[role_definition]
g = _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = (g(r.sub, p.sub) || p.sub == "*") && (keyMatch(r.obj, p.obj)||keyMatch2(r.obj, p.obj)) && regexMatch(r.act, p.act)`

func NewAdapter(source []byte) *Adapter {
	return &Adapter{policy: []CasbinRule{}, source: source}
}

func (a *Adapter) LoadPolicy(m model.Model) error {
	err := a.loadFromBuffer(m)
	if err != nil {
		return err
	}

	return nil
}

func (a *Adapter) loadFromBuffer(m model.Model) error {
	if len(a.source) == 0 {
		return nil
	}

	var input jsonPolicy
	err := json.Unmarshal(a.source, &input)
	if err != nil {
		return err
	}

	for _, p := range input.Policy {
		if err := persist.LoadPolicyLine(fmt.Sprintf("p,%s,%s,%s", p.Role, p.Path, p.Action), m); err != nil {
			return err
		}
	}

	for _, r := range input.Roles {
		if err := persist.LoadPolicyLine(fmt.Sprintf("g,%s,%s", r.Role, r.RoleBinding), m); err != nil {
			return err
		}
	}

	return nil
}

// AddPolicy adds a policy rule to the storage.
func (a *Adapter) AddPolicy(_ string, _ string, _ []string) error {
	return errors.New("not implemented")
}

// RemovePolicy removes a policy rule from the storage.
func (a *Adapter) RemovePolicy(_ string, _ string, _ []string) error {
	return errors.New("not implemented")
}

// RemoveFilteredPolicy removes policy rules that match the filter from the storage.
func (a *Adapter) RemoveFilteredPolicy(_ string, _ string, _ int, _ ...string) error {
	return errors.New("not implemented")
}

// SavePolicy saves policy
func (a *Adapter) SavePolicy(m model.Model) error {
	a.policy = []CasbinRule{}
	var rules []CasbinRule
	for ptype, ast := range m["p"] {
		for _, rule := range ast.Policy {
			rules = append(rules, CasbinRule{PType: ptype, V0: rule[0], V1: rule[1], V2: rule[2]})
		}
	}

	for ptype, ast := range m["g"] {
		for _, rule := range ast.Policy {
			rules = append(rules, CasbinRule{PType: ptype, V0: rule[0], V1: rule[1]})
		}
	}

	a.policy = rules

	return a.saveToBuffer()
}

func (a *Adapter) saveToBuffer() error {
	data, err := json.Marshal(a.policy)
	if err == nil {
		a.source = data
	}

	return err
}

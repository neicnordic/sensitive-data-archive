package validators

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type ValidatorsTestSuite struct {
	suite.Suite
	mockCommandExecutor *mockCommandExecutor
}

func (ts *ValidatorsTestSuite) SetupTest() {
	ts.mockCommandExecutor = &mockCommandExecutor{}
	commandExecutor = ts.mockCommandExecutor
}

func TestJobPreparationWorkerTestSuite(t *testing.T) {
	suite.Run(t, new(ValidatorsTestSuite))
}

type mockCommandExecutor struct {
	mock.Mock
}

func (m *mockCommandExecutor) Execute(name string, args ...string) ([]byte, error) {
	mockArgs := m.Called(name, args)

	if val, ok := mockArgs.Get(0).([]byte); ok {
		return val, mockArgs.Error(1)
	}
	return nil, mockArgs.Error(1)
}

func (ts *ValidatorsTestSuite) TestInit() {

	validatorDescription1 := &ValidatorDescription{
		ValidatorId:       "mock-validator-1",
		Name:              "mock validator 1",
		Description:       "Mock validator 1",
		Version:           "v0.0.1",
		Mode:              "mock",
		PathSpecification: nil,
		ValidatorPath:     "*",
	}
	validatorDescription2 := &ValidatorDescription{
		ValidatorId:       "mock-validator-2",
		Name:              "mock validator 2",
		Description:       "Mock validator 2",
		Version:           "v0.0.2",
		Mode:              "mock",
		PathSpecification: nil,
		ValidatorPath:     "*",
	}

	vd1Json, err := json.Marshal(validatorDescription1)
	if err != nil {
		ts.FailNow("failed to marshal validator description", err)
	}
	vd2Json, err := json.Marshal(validatorDescription2)
	if err != nil {
		ts.FailNow("failed to marshal validator description", err)
	}

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"/mock-validator-1.sif",
			"--describe"}).Return(vd1Json, nil)

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"/mock-validator-2.sif",
			"--describe"}).Return(vd2Json, nil)

	ts.NoError(Init([]string{"/mock-validator-1.sif", "/mock-validator-2.sif"}))
	ts.Len(Validators, 2)

	vd1, ok := Validators["mock-validator-1"]
	if !ok {
		ts.FailNow("mock-validator-1 does not exist")
	}
	ts.Equal(vd1.ValidatorPath, "/mock-validator-1.sif")

	vd2, ok := Validators["mock-validator-2"]
	if !ok {
		ts.FailNow("mock-validator-2 does not exist")
	}
	ts.Equal(vd2.ValidatorPath, "/mock-validator-2.sif")
}

func (ts *ValidatorsTestSuite) TestInit_Error() {

	ts.mockCommandExecutor.On("Execute",
		"apptainer",
		[]string{"run",
			"--userns",
			"--net",
			"--network", "none",
			"/mock-validator-1.sif",
			"--describe"}).Return(nil, errors.New("expected error from apptainer"))

	ts.EqualError(Init([]string{"/mock-validator-1.sif"}), "failed to execute describe command towards path: /mock-validator-1.sif, error: expected error from apptainer")
}

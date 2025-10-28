package validators

import (
	"encoding/json"
	"fmt"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/command_executor"
)

var Validators map[string]*ValidatorDescription

type ValidatorDescription struct {
	ValidatorId       string   `json:"validatorId"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Version           string   `json:"version"`
	Mode              string   `json:"mode"`
	PathSpecification []string `json:"pathSpecification"`

	ValidatorPath string // The path this validator is available at
}

var commandExecutor command_executor.CommandExecutor

func init() {
	commandExecutor = command_executor.OsCommandExecutor{}
	Validators = make(map[string]*ValidatorDescription)
}

func Init(validatorsPaths []string) error {

	for _, path := range validatorsPaths {
		out, err := commandExecutor.Execute(
			"apptainer",
			"run",
			"--userns",
			"--net",
			"--network", "none",
			path,
			"--describe")
		if err != nil {
			return fmt.Errorf("failed to execute describe command towards path: %s, error: %v", path, err)
		}

		vd := new(ValidatorDescription)
		if err := json.Unmarshal(out, vd); err != nil {
			return fmt.Errorf("failed to unmarshal response from describe command towards path: %s, error: %v", path, err)
		}
		vd.ValidatorPath = path

		Validators[vd.ValidatorId] = vd
	}

	return nil
}

func (vd *ValidatorDescription) RequiresFileContent() bool {
	return vd.Mode != "file-structure"
}

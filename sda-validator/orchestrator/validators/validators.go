package validators

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/neicnordic/sensitive-data-archive/sda-validator/orchestrator/internal/commandexecutor"
)

var Validators map[string]*ValidatorDescription

type ValidatorDescription struct {
	ValidatorID       string   `json:"validatorID"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Version           string   `json:"version"`
	Mode              string   `json:"mode"`
	PathSpecification []string `json:"pathSpecification"`

	ValidatorPath string // The path this validator is available at
}

var commandExecutor commandexecutor.CommandExecutor

func init() {
	commandExecutor = commandexecutor.OsCommandExecutor{}
	Validators = make(map[string]*ValidatorDescription)
}

func Init(validatorsPaths []string) error {
	for _, path := range validatorsPaths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("failed to stat file: %s, error: %v", path, err)
		}

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

		Validators[vd.ValidatorID] = vd
	}

	return nil
}

func (vd *ValidatorDescription) RequiresFileContent() bool {
	return vd.Mode != "file-structure"
}

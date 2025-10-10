
1. Create a .def file
2. Build the .sif file 
```bash
apptainer build --userns 100-200_word_validator.sif 100-200_word_validator.def
```
3. Build the sandbox directory
```bash
apptainer build --sandbox 100-200_word_validator_sandbox 100-200_word_validator.sif
```
4. archive sandbox directory
```bash
tar -czvf 100-200_word_validator.tar.gz 100-200_word_validator_sandbox
```
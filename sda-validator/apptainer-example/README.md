# Apptainer examples
This contains 2 example [Apptainer](https://apptainer.org/) validators following the [Validator Framework Specification](https://github.com/GenomicDataInfrastructure/validation-framework/blob/main/validator-specification.md)

[100-200 Word Validator](100-200_word_validator.def)) validates that a file has between 100 and 200 words

[Always Success](always_success.def) always returns success("passed") on validations, unless no files are provided.  

## Build and compile 
```bash
apptainer build 100-200_word_validator.sif 100-200_word_validator.def
apptainer build always_success.sif always_success.def
```
Testing
=======

We have implemented 2 types of testsuite:

-   one set of *unit tests* to test the functionalities of the code;
-   one set of *integration tests* to test the overall architecture.

The latter does actually deploy a chosen setup and runs several
scenarios, users will utilize the system as a whole.

> NOTE:
> Unit tests and integration tests are automatically executed with every
> push and PR to the `NeIC Github repo` via Github Actions.

In order to replicate integration tests on a local machine see:
[sda-pipeline Local testing
howto](https://github.com/neicnordic/sda-pipeline/tree/master/dev_utils#readme)

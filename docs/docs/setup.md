Installation
============

The sources for SDA can be downloaded and installed from the [NeIC
Github repo](https://github.com/neicnordic/sda-pipeline).

```bash
$ git clone https://github.com/neicnordic/sda-pipeline.git
$ go build
```

The recommended method is however to use one of our deployment
strategies:

-   [Kubernetes Helm charts](https://github.com/neicnordic/sda-helm/);
-   [Docker
    Swarm](https://github.com/neicnordic/LocalEGA-deploy-swarm/).

Configuration
-------------

Starting the SDA submission services require a running Database and
Message Broker, the setup for those components is detailed in:

- [Database Setup](db.md);
- [Local Message Broker](connection.md#local-message-broker).

[Data Retrieval API](dataout.md) requires a working Database in order to be set up.

# Middlewares
- The default middleware is `TokenMiddleware`, which expects an access token, that can be sent to AAI in return for GA4GH visas.
- One may create custom middlewares in this `middleware` package, and register them to the `availableMiddlewares` in [config.go](../../internal/config/config.go), and adding a case for them in [main.go](../../cmd/main.go).
- A middleware for runtime can then be selected with the `app.middleware` config.
- For custom middlewares, it is important, that they use the `storeDatasets` function available in [middleware.go](middleware.go) to set the permissions for accessing data.

# Graceful
[![License](https://img.shields.io/badge/license-mit-blue.svg?style=for-the-badge)](https://raw.githubusercontent.com/abursavich/graceful/main/LICENSE)
[![GoDev Reference](https://img.shields.io/static/v1?logo=go&logoColor=white&color=00ADD8&label=dev&message=reference&style=for-the-badge)](https://pkg.go.dev/bursavich.dev/graceful)
[![Go Report Card](https://goreportcard.com/badge/bursavich.dev/graceful?style=for-the-badge)](https://goreportcard.com/report/bursavich.dev/graceful)

Package graceful provides graceful shutdown for servers.


## Example

### Dual HTTP Server

```go
// DualServerConfig specifies a server with functions split between
// internal and external use cases and graceful shutdown parameters.
srv := graceful.DualServerConfig{
	// ExternalServer is the server for the primary clients.
    ExternalServer: graceful.HTTPServer(&http.Server{
        Handler: extMux,
    }),
	// InternalServer is the server for health checks, metrics, debugging,
	// profiling, etc. It shuts down after the ExternalServer exits.
    InternalServer: graceful.HTTPServer(&http.Server{
        Handler: intMux,
    }),
	// ShutdownDelay gives time for load balancers to remove the server from
	// their backend pools before it stops listening. It's inserted after a
	// shutdown signal is received and before GracefulShutdown is called on
	// the servers.
    ShutdownDelay: 10 * time.Second,
	// ShutdownGrace gives time for pending requests complete before the server
	// must forcibly shut down. It's the timeout on the context passed to
	// GracefulShutdown.
    ShutdownGrace: 30 * time.Second,
	// Logger optionally specifies a logger which will be used to output info
    // and errors.
    Logger: log,
}
if err := srv.ListenAndServe(ctx, *intAddr, *extAddr); err != nil {
    log.Error(err, "Serving failed")
}
```
# Graceful
[![License](https://img.shields.io/badge/license-mit-blue.svg?style=for-the-badge)](https://raw.githubusercontent.com/abursavich/graceful/main/LICENSE)
[![GoDev Reference](https://img.shields.io/static/v1?logo=go&logoColor=white&color=00ADD8&label=dev&message=reference&style=for-the-badge)](https://pkg.go.dev/bursavich.dev/graceful)
[![Go Report Card](https://goreportcard.com/badge/bursavich.dev/graceful?style=for-the-badge)](https://goreportcard.com/report/bursavich.dev/graceful)

Package graceful provides graceful shutdown for servers.


## Example

### Dual HTTP Server

```go
// DualServerConfig specifies a server split between internal and external
// clients with graceful shutdown parameters.
srv := graceful.DualServerConfig{
	// ExternalServer is the server for primary clients.
	ExternalServer: graceful.HTTPServer(&http.Server{
		Handler: extMux,
	}),
	// InternalServer is the server for health checks, metrics, debugging,
	// profiling, etc. It shuts down after the ExternalServer exits.
	InternalServer: graceful.HTTPServer(&http.Server{
		Handler: intMux,
	}),
	// ShutdownDelay gives time for load balancers to remove the server from
	// their backend pools after a shutdown signal is received and before it
	// stops listening.
	ShutdownDelay: 10 * time.Second,
	// ShutdownGrace gives time for pending requests to complete before the
	// server forcibly shuts down.
	ShutdownGrace: 30 * time.Second,
	// Logger optionally adds the ability to log messages, both errors and not.
	Logger: log,
}
intMux.HandleFunc("/health/alive", func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})
intMux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
	select {
		case <-srv.ShuttingDown():
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			w.WriteHeader(http.StatusOK)
	}
})
if err := srv.ListenAndServe(ctx, *intAddr, *extAddr); err != nil {
	log.Error(err, "Serving failed")
}
```

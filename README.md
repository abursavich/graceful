# Graceful
[![License](https://img.shields.io/badge/license-mit-blue.svg?style=for-the-badge)](https://raw.githubusercontent.com/abursavich/graceful/main/LICENSE)
[![GoDev Reference](https://img.shields.io/static/v1?logo=go&logoColor=white&color=00ADD8&label=dev&message=reference&style=for-the-badge)](https://pkg.go.dev/bursavich.dev/graceful)
[![Go Report Card](https://goreportcard.com/badge/bursavich.dev/graceful?style=for-the-badge)](https://goreportcard.com/report/bursavich.dev/graceful)

Package graceful runs processes, such as servers, with graceful shutdown.


## Example

### Dual HTTP Server

```go
// Create API server.
apiSrv := &http.Server{
	// ...
}
apiLis, err := net.Listen("tcp", cfg.HTTPAddress)
if err != nil {
	log.Error(err, "API HTTP server failed to listen", "addr", cfg.HTTPAddress)
	return err
}
defer apiLis.Close()
log.Info("API HTTP server listening", "addr", apiLis.Addr())

// Create debug server.
goingAway := make(chan struct{})
debugMux := http.NewServeMux()
debugMux.HandleFunc("/health/live", func(w http.ResponseWriter, _ *http.Request) {
	writeStatus(w, http.StatusOK)
})
debugMux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
	select {
	case <-goingAway:
		writeStatus(w, http.StatusServiceUnavailable)
	default:
		writeStatus(w, http.StatusOK)
	}
})
debugSrv := &http.Server{
	Handler: debugMux,
	// ...
}
debugLis, err := net.Listen("tcp", cfg.DebugAddress)
if err != nil {
	log.Error(err, "Debug HTTP server failed to listen", "addr", cfg.DebugAddress)
	return err
}
defer debugLis.Close()
log.Info("Debug HTTP server listening", "addr", debugLis.Addr())


err = graceful.Run(ctx,
	// An OrderedGroup is a composition of Processes. All processes in the group
	// are started concurrently but they're stopped serially. In this case, the
	// debug server keeps serving until after the API server shuts down.
	graceful.OrderedGroup{
		graceful.HTTPServerProcess(apiSrv, apiLis),
		graceful.HTTPServerProcess(debugSrv, debugLis),
	},
	// Logger is used to write info logs about shutdown signals and error logs
	// about failed processes.
	graceful.WithLogger(log),
	// NotifyFuncs are called as soon as the shutdown signal is received, before
	// the shutdown delay. It gives the server time to pre-emptively fail health
	// checks and notify clients that it will be going away.
	graceful.WithNotifyFunc(func() { close(goingAway) }),
	// Delay gives time for clients and load balancers to remove the server from
	// their backend pools after a shutdown signal is received and before the
	// server stops listening.
	graceful.WithDelay(cfg.ShutdownDelay),
	// Grace gives time for pending requests to finish before the server
	// forcibly exits.
	graceful.WithGrace(cfg.ShutdownGrace),
)
if err != nil {
	log.Error(err, "Serving with graceful shutdown failed")
	return err
}
return nil
```

# Graceful
[![License](https://img.shields.io/badge/license-mit-blue.svg?style=for-the-badge)](https://raw.githubusercontent.com/abursavich/graceful/main/LICENSE)
[![GoDev Reference](https://img.shields.io/static/v1?logo=go&logoColor=white&color=00ADD8&label=dev&message=reference&style=for-the-badge)](https://pkg.go.dev/bursavich.dev/graceful)
[![Go Report Card](https://goreportcard.com/badge/bursavich.dev/graceful?style=for-the-badge)](https://goreportcard.com/report/bursavich.dev/graceful)

Package graceful provides graceful shutdown for servers.


## Example

### Dual HTTP Server

```go
goingAway := make(chan struct{})
debugMux.HandleFunc("/health/alive", func(w http.ResponseWriter, _ *http.Request) {
	writeHTTPStatus(w, http.StatusOK)
})
debugMux.HandleFunc("/health/ready", func(w http.ResponseWriter, _ *http.Request) {
	select {
	case <-goingAway:
		writeHTTPStatus(w, http.StatusServiceUnavailable)
	default:
		writeHTTPStatus(w, http.StatusOK)
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

mainSrv := &http.Server{
	// ...
}
mainLis, err := net.Listen("tcp", cfg.HTTPAddress)
if err != nil {
	log.Error(err, "Main HTTP server failed to listen", "addr", cfg.HTTPAddress)
	return err
}
defer mainLis.Close()
log.Info("Main HTTP server listening", "addr", mainLis.Addr())

// An OrderedGroup is a composition of Processes. All processes in the group are
// started concurrently but they're stopped serially. In this case, the debug HTTP
// server keeps serving until after the main HTTP server shuts down.
processGroup := graceful.OrderedGroup{
	graceful.HTTPServerProcess(mainSrv, mainLis),
	graceful.HTTPServerProcess(debugSrv, debugLis),
}
options := graceful.Options{
	// Logger is used to write info logs about shutdown signals
	// and error logs about failed processes.
	graceful.WithLogger(log),
	// Delay gives time for clients and load balancers to remove the server from
	// their backend pools after a shutdown signal is received and before the
	// server stops listening.
	graceful.WithDelay(cfg.ShutdownDelay),
	// Grace gives time for pending requests to finish before the server
	// forcibly exits.
	graceful.WithGrace(cfg.ShutdownGrace),
	// NotifyFuncs are called as soon as the shutdown signal is received, before
	// the shutdown delay. It gives the server time to pre-emptively fail
	// healthchecks and notify clients that it will be going away.
	graceful.WithNotifyFunc(func() { close(goingAway) }),
}
if err := graceful.Run(ctx, processGroup, options...); err != nil {
	log.Error(err, "Serving with graceful shutdown failed")
	return err
}
return nil
```

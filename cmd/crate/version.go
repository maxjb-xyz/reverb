package main

// version is the build version. It is overridden at build time via
// -ldflags "-X main.version=<value>" (see the Makefile and Dockerfile).
// It defaults to "dev" for `go run` / un-stamped builds.
var version = "dev"

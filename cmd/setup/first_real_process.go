package main

import "time"

// A persisted "running" first-real state can only belong to the current setup
// process when it started after this timestamp. On a relaunch, the previous
// goroutine no longer exists, so treating that state as still running would
// unnecessarily strand the user in a false progress state for several minutes.
var firstRealProcessStartedAt = time.Now().UTC()

package main

// eva-eval continuation run's v1 condition, eva (EV-17c): importing v1arm
// registers it with the driver. Its flag, --sign-in, is one of run's
// (continuation.go), which hands it to the condition in the driver's
// options; the condition checks it, and its --eva-md, before any trial
// starts.
import _ "github.com/heyNag/eva-md/internal/eval/continuation/v1arm"

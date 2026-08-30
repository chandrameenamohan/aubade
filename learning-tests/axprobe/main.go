// Command axprobe prints what internal/ax decides about the caller on the
// other end of the pipe, as one line of JSON.
//
// It exists for learning-tests/04-ax-detection.sh: the only honest way to learn
// what agentx does inside a real agent session is to ask it from inside one, and
// then ask it again with the environment scrubbed. Unit tests use
// agentx.MockEnvironment and therefore prove the failure modes, not the live
// behaviour — this probe covers the other half.
//
// It reads the real process environment (that is the point) and writes nothing.
package main

import (
	"encoding/json"
	"os"

	"github.com/chandrameenamohan/aubade/internal/ax"
)

func main() {
	name, isAgent := ax.Caller()

	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(map[string]any{
		"caller":   name,
		"is_agent": isAgent,
		"mode":     ax.OutputMode().String(),
	}); err != nil {
		os.Exit(1)
	}
}

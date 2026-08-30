// Command aubade is the product: an agent-orchestrated morning digest grounded
// in a deterministic, cited toolbox.
package main

import (
	"os"

	"github.com/chandrameenamohan/aubade/internal/cli"
)

func main() {
	if err := cli.NewAubadeCmd().Execute(); err != nil {
		cli.RenderError(os.Stderr, err)
		os.Exit(1)
	}
}

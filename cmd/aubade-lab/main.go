// Command aubade-lab is the internal dev and eval harness: it writes the
// synthetic exam and grades the digest. It is never shipped to a customer.
package main

import (
	"os"

	"github.com/chandrameenamohan/aubade/internal/cli"
)

func main() {
	if err := cli.NewLabCmd().Execute(); err != nil {
		cli.RenderError(os.Stderr, err)
		os.Exit(1)
	}
}

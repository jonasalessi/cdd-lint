// Command cdd measures code quality with Cognitive-Driven Development (CDD)
// and Intrinsic Complexity Points (ICPs).
package main

import (
	"os"

	"github.com/jonasalessi/cdd-lint/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}

package output

import (
	"fmt"
	"os"
)

func WriteStdout(text string) error {
	_, err := fmt.Fprint(os.Stdout, text)
	return err
}

func WriteStderr(text string) error {
	_, err := fmt.Fprint(os.Stderr, text)
	return err
}

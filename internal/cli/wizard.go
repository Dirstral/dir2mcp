package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// IsTTY returns true if stdin is a terminal (for interactive prompts).
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// ReadSecret prompts for a secret with masked input when TTY; plain read otherwise.
func ReadSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			return strings.TrimSpace(scanner.Text()), nil
		}
		return "", scanner.Err()
	}
	b, err := term.ReadPassword(fd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

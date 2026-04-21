package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const maxRetries = 3

// Text prompts the user for a line of text. Returns def if input is empty.
// If required is true and def is empty and input is empty, re-prompts up to maxRetries times.
func Text(in io.Reader, out io.Writer, prompt, def string, required bool) (string, error) {
	r := bufio.NewReader(in)
	for range maxRetries {
		if def != "" {
			fmt.Fprintf(out, "%s [%s]: ", prompt, def)
		} else {
			fmt.Fprintf(out, "%s: ", prompt)
		}

		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		if line != "" {
			return line, nil
		}
		if err != nil {
			if def != "" {
				return def, nil
			}
			if !required {
				return "", nil
			}
			return "", err
		}
		if def != "" {
			return def, nil
		}
		if !required {
			return "", nil
		}
		fmt.Fprintln(out, "This field is required.")
	}
	return "", fmt.Errorf("prompt: no valid input after %d attempts", maxRetries)
}

// Bool prompts yes/no. def=true means [Y/n], def=false means [y/N].
// Accepts: y/Y/yes/YES → true; n/N/no/NO → false; empty → def.
func Bool(in io.Reader, out io.Writer, prompt string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	r := bufio.NewReader(in)
	for range maxRetries {
		fmt.Fprintf(out, "%s %s: ", prompt, hint)
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		case "":
			return def, nil
		default:
			if err != nil {
				return def, err
			}
			fmt.Fprintln(out, "Please enter y or n.")
		}
	}
	return def, fmt.Errorf("prompt: no valid input after %d attempts", maxRetries)
}

// Choice presents a numbered menu. Returns the selected option string.
// If def is non-empty, pressing Enter selects it. Out-of-range index re-prompts.
// Returns an error after maxRetries failed attempts.
func Choice(in io.Reader, out io.Writer, prompt string, options []string, def string) (string, error) {
	r := bufio.NewReader(in)
	for range maxRetries {
		fmt.Fprintln(out, prompt)
		for i, opt := range options {
			fmt.Fprintf(out, "  [%d] %s\n", i+1, opt)
		}
		if def != "" {
			fmt.Fprintf(out, "Enter number (default: %s): ", def)
		} else {
			fmt.Fprint(out, "Enter number: ")
		}

		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		line = strings.TrimSpace(line)

		if line == "" {
			if err != nil {
				if def != "" {
					return def, err
				}
				return "", err
			}
			if def != "" {
				return def, nil
			}
			fmt.Fprintln(out, "Please enter a number.")
			continue
		}

		n, parseErr := strconv.Atoi(line)
		if parseErr != nil || n < 1 || n > len(options) {
			if err != nil {
				return "", err
			}
			fmt.Fprintf(out, "Please enter a number between 1 and %d.\n", len(options))
			continue
		}
		return options[n-1], nil
	}
	return "", fmt.Errorf("prompt: no valid selection after %d attempts", maxRetries)
}

// MultiChoice presents a numbered menu allowing comma-separated selection (e.g. "1,3").
// defs contains the initially-selected options. Pressing Enter with no input returns defs.
// If defs is nil/empty and the user presses Enter, returns (nil, nil) — callers that
// require at least one selection must check the returned slice length.
// Returns an error after maxRetries failed attempts.
func MultiChoice(in io.Reader, out io.Writer, prompt string, options []string, defs []string) ([]string, error) {
	defSet := make(map[string]bool, len(defs))
	for _, d := range defs {
		defSet[d] = true
	}
	r := bufio.NewReader(in)
	for range maxRetries {
		fmt.Fprintln(out, prompt)
		for i, opt := range options {
			if defSet[opt] {
				fmt.Fprintf(out, "  [%d]* %s\n", i+1, opt)
			} else {
				fmt.Fprintf(out, "  [%d]  %s\n", i+1, opt)
			}
		}
		fmt.Fprint(out, "Enter numbers (comma-separated, Enter for defaults): ")

		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		line = strings.TrimSpace(line)

		if line == "" {
			return defs, err
		}

		parts := strings.Split(line, ",")
		result := make([]string, 0, len(parts))
		valid := true
		for _, p := range parts {
			p = strings.TrimSpace(p)
			n, parseErr := strconv.Atoi(p)
			if parseErr != nil || n < 1 || n > len(options) {
				valid = false
				break
			}
			result = append(result, options[n-1])
		}

		if !valid {
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(out, "Please enter valid numbers between 1 and %d.\n", len(options))
			continue
		}
		return result, nil
	}
	return nil, fmt.Errorf("prompt: no valid selection after %d attempts", maxRetries)
}

// Secret reads a masked password from f (must be a real TTY file).
// Uses term.ReadPassword which handles raw-mode entry and terminal restore internally,
// ensuring the terminal is restored even if a panic occurs during the read.
func Secret(f *os.File, out io.Writer, prompt string) (string, error) {
	fmt.Fprintf(out, "%s: ", prompt)
	b, err := term.ReadPassword(int(f.Fd()))
	fmt.Fprintln(out)
	if err != nil {
		return "", err
	}
	s := string(b)
	for i := range b {
		b[i] = 0
	}
	return s, nil
}

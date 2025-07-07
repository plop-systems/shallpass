package main

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// main is the entry point of the SSH wrapper program.
// This version is designed for non-interactive use, such as in provisioning scripts.
func main() {
	// This wrapper expects the password to be piped via standard input.
	// It reads all of stdin until EOF to get the password.
	passwordBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(1)
	}
	password := string(passwordBytes)

	// Prepare the ssh command, passing through all command-line arguments.
	// There is no argument parsing or handling, as requested.
	cmd := exec.Command("ssh", os.Args[1:]...)

	// We need to control ssh's stdin to send the password, so we get a pipe.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		os.Exit(1)
	}

	// Create a pipe. We will use this to read ssh's stdout in our goroutine
	// while it also goes to the user's terminal.
	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		os.Exit(1)
	}

	// Create a MultiWriter. This sends ssh's stdout to two places:
	// 1. os.Stdout: The user's terminal, for direct feedback.
	// 2. stdoutWriter: The write-end of our pipe, so our goroutine can scan it.
	multiWriter := io.MultiWriter(os.Stdout, stdoutWriter)
	cmd.Stdout = multiWriter

	// Standard error from the ssh process can be passed through directly.
	cmd.Stderr = os.Stderr

	// Start the ssh command in the background.
	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}

	// This goroutine's job is to scan the output for the password prompt,
	// send the password, and then exit. It doesn't need to stay alive
	// for the whole session.
	go func() {
		// Ensure pipes are closed when the goroutine finishes.
		defer stdinPipe.Close()
		defer stdoutWriter.Close()

		// We scan the output from the read-end of our pipe.
		scanner := bufio.NewScanner(stdoutReader)
		for scanner.Scan() {
			line := scanner.Text()
			// Check for the password prompt. This is a simple, case-insensitive check.
			if strings.Contains(strings.ToLower(line), "password:") {
				// The prompt has been detected. Write the password we read earlier
				// into the ssh process's standard input.
				io.WriteString(stdinPipe, password)

				// Our job is done. The goroutine can now exit ("bail").
				// The MultiWriter will continue to pass ssh's stdout to the user's terminal.
				return
			}
		}
	}()

	// Wait for the ssh command to complete.
	waitErr := cmd.Wait()

	// If the command completed successfully (exit code 0), waitErr will be nil.
	// In this case, we exit with 0.
	if waitErr == nil {
		os.Exit(0)
	}

	// If the command failed, we try to extract the exit code.
	// We can only do this if the error is of type *exec.ExitError.
	if exitError, ok := waitErr.(*exec.ExitError); ok {
		// The command returned a non-zero exit code.
		// We can get the system-dependent exit status.
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			// Exit our program with the same code as the ssh process.
			os.Exit(status.ExitStatus())
		}
	}

	// If we couldn't get the exit code for some reason, exit with a generic
	// failure code of 1.
	os.Exit(1)
}

package compiler

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	MAX_CONCURRENT_COMPILES = 4
)

// Finds what files are sass compilable in the context's `inputPath`.
func findCompilable(ctx *SassContext) map[string]string {
	compilable := make(map[string]string, 100)

	filepath.Walk(ctx.inputPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fileLogCompilationError(path, err)
		} else if !info.IsDir() && isSassFile(path) && !isPrivateFile(path) {
			compilable[path] = ctx.resolveOutputPath(path)
		}

		return nil
	})

	return compilable
}

// Compiles an individual file
func compile(ctx *SassContext, inputPath string, outputPath string) error {
	// Create the parent directory
	if err := os.MkdirAll(filepath.Dir(outputPath), os.ModePerm); err != nil {
		return err
	}

	// Create the command and grab stdout/stderr
	cmd := ctx.cmd.Create(inputPath)

	// Grab stdout
	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return err
	}

	defer stdout.Close()

	// Grab stderr
	stderr, err := cmd.StderrPipe()

	if err != nil {
		return err
	}

	defer stderr.Close()

	// Run the command
	if err := cmd.Start(); err != nil {
		return err
	}

	// Handle stdout
	stdoutBytes, err := ioutil.ReadAll(stdout)

	if err != nil {
		return err
	}

	// Handle stderr
	stderrBytes, err := ioutil.ReadAll(stderr)

	if err != nil {
		return err
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		return err
	}

	// Print out stderr
	if len(stderrBytes) > 0 {
		for _, line := range strings.Split(string(stderrBytes), "\n") {
			log.Print(line)
		}
	}

	// Process the results
	stdoutString := string(stdoutBytes)

	for _, plugin := range ctx.plugins {
		objs, err := plugin.Objects()

		if err != nil {
			return err
		}

		for _, obj := range objs {
			var newStdoutString string
			err = plugin.Call(fmt.Sprintf("%s.ProcessCss", obj), stdoutString, &newStdoutString)

			if err != nil {
				return err
			}

			stdoutString = newStdoutString
		}
	}

	return ioutil.WriteFile(outputPath, []byte(stdoutString), os.ModePerm)
}

// Compiles many files, as a mapping of input file path -> output file path
func compileMany(ctx *SassContext, mapping map[string]string) bool {
	remaining := len(mapping)
	lock := make(chan bool, MAX_CONCURRENT_COMPILES)
	errorChans := make(map[string]chan error, remaining*2)

	for inputPath, outputPath := range mapping {
		errorChan := make(chan error, 1)
		errorChans[inputPath] = errorChan

		go func(inputPath string, outputPath string, errorChan chan error) {
			lock <- true
			defer func() { <-lock }()

			err := compile(ctx, inputPath, outputPath)
			errorChan <- err
		}(inputPath, outputPath, errorChan)
	}

	hasErrors := false

	for inputPath, errorChan := range errorChans {
		err := <-errorChan

		if err != nil {
			fileLogCompilationError(inputPath, err)
			hasErrors = true
		} else {
			fileLogCompilation(inputPath, mapping[inputPath])
		}
	}

	return hasErrors
}

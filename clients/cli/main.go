package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	directions "github.com/jataware/directions/clients/lib/go"
)

const maxDocumentBytes = 1 << 20

func run(args []string, out, errOut io.Writer) int {
	if len(args) < 2 || (args[0] != "validate" && args[0] != "context") || (args[0] == "validate" && len(args) != 2) {
		fmt.Fprintln(errOut, "usage: directions validate FILE | directions context DIRECTORY [--profile NAME] [--diff-file JSON] [--agents] [--exact-root] [--allow-url URL ...]")
		return 2
	}
	if args[0] == "context" {
		flags := flag.NewFlagSet("context", flag.ContinueOnError)
		flags.SetOutput(errOut)
		profile := flags.String("profile", "default", "selected profile")
		diffFile := flags.String("diff-file", "", "trusted host JSON array of changed paths")
		agents := flags.Bool("agents", false, "include root context file")
		exact := flags.Bool("exact-root", false, "do not discover parent roots")
		var allowedURLs []string
		flags.Func("allow-url", "authorize one exact HTTPS reference (repeatable)", func(value string) error {
			allowedURLs = append(allowedURLs, value)
			return nil
		})
		if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 {
			return 2
		}
		options := directions.ContextOptions{Profile: *profile, IncludeAgents: *agents, ExactRoot: *exact, AllowedURLs: allowedURLs}
		if *diffFile != "" {
			file, err := os.Open(*diffFile)
			if err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
			data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
			file.Close()
			var paths []string
			if err != nil || len(data) > maxDocumentBytes || json.Unmarshal(data, &paths) != nil || paths == nil {
				fmt.Fprintln(errOut, "invalid host diff JSON array (limit 1 MiB)")
				return 1
			}
			options.ChangedPaths = &paths
		}
		result, err := directions.BuildContext(context.Background(), args[1], options)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		if err := json.NewEncoder(out).Encode(result); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
		return 0
	}
	file, err := os.Open(args[1])
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintln(errOut, "configuration must be a regular file")
		return 1
	}
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if len(data) > maxDocumentBytes {
		fmt.Fprintln(errOut, "configuration exceeds 1 MiB limit")
		return 1
	}
	if _, err := directions.Parse(data); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	fmt.Fprintln(out, "Valid Directions document (single file only; overrides and references not resolved).")
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

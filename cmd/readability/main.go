package main

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/mauidude/go-readability"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "readability [file]",
		Short: "Readability is a CLI tool to extract content from an HTML page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := ioutil.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("unable to read file: %w", err)
			}

			doc, err := readability.NewDocument(string(content))
			if err != nil {
				return fmt.Errorf("unable to create document: %w", err)
			}

			doc.MinTextLength, _ = cmd.Flags().GetInt("min-text-length")

			html := doc.Content()
			fmt.Println(html)

			return nil
		},
	}

	rootCmd.Flags().IntP("min-text-length", "l", 0, "minimum text length to consider a node")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

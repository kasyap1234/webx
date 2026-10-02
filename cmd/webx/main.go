package main

import (
	"fmt"
	"os"

	"github.com/kasyap1234/webx/web/fetch"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "webx",
	Short: "Fetch web pages and convert them to markdown",
	Long: `webx fetches developer documentation and converts it to clean
markdown for consumption by coding agents and humans.`,
}

var fetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Fetch a URL and print it as markdown",
	Args:  cobra.ExactArgs(1),
	RunE:  runFetch,
}

func runFetch(cmd *cobra.Command, args []string) error {
	doc, err := fetch.Fetch(cmd.Context(), fetch.FetchRequest{URL: args[0]})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), doc.Markdown)
	return err
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

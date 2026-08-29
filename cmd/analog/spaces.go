package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func spacesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "spaces",
		Short: "List spaces",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := newClient().ListSpaces()
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(rows)
			}
			width := 0
			for _, s := range rows {
				if len(s.Slug) > width {
					width = len(s.Slug)
				}
			}
			for _, s := range rows {
				fmt.Printf("%-*s  %s  [%d cards, %d links, %d open]\n",
					width, s.Slug, s.Title, s.Counts.Cards, s.Counts.Links,
					s.Counts.OpenAnnotations)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func newSpaceCmd() *cobra.Command {
	var title, mode string
	cmd := &cobra.Command{
		Use:   "new <slug>",
		Short: "Create a space",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			if title == "" {
				title = args[0]
			}
			space, err := a.CreateSpace(args[0], title, mode)
			if err != nil {
				return fail(err)
			}
			fmt.Printf("%s  %s\n", space.Slug, a.SpaceURL(args[0]))
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "human-readable title")
	cmd.Flags().StringVar(&mode, "mode", "replace", "replace | branch")
	return cmd
}

func openCmd() *cobra.Command {
	var browser bool
	cmd := &cobra.Command{
		Use:   "open <slug>",
		Short: "Print the URL for a space; --browser to launch it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			url := a.SpaceURL(args[0])
			if _, err := a.GetSpace(args[0]); err != nil {
				return fail(err)
			}
			fmt.Println(url)
			if browser {
				openInBrowser(url)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&browser, "browser", false, "launch the URL")
	return cmd
}

func openInBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func rmSpaceCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rm-space <slug>",
		Short: "Delete a space and everything in it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				errln(fmt.Sprintf("refusing to delete '%s' without --yes", args[0]))
				os.Exit(exitError)
			}
			if err := newClient().DeleteSpace(args[0]); err != nil {
				return fail(err)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm the deletion")
	return cmd
}

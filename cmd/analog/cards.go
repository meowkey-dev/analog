package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/client"
)

func addCmd() *cobra.Command {
	var title, kind, file, text string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "add <slug> [source]",
		Short: "Post a card",
		Long:  "Post a card. `source` is a file path, or - for stdin.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			content := text
			if !cmd.Flags().Changed("text") {
				var err error
				if content, err = readSource(second(args), file); err != nil {
					return err
				}
			}
			nodes, err := newClient().CreateCards(args[0],
				[]client.CardDraft{{Title: title, Content: content, Kind: kind}})
			if err != nil {
				return fail(err)
			}
			if len(nodes) == 0 {
				return fmt.Errorf("server returned no card")
			}
			if asJSON {
				return out(nodes[0])
			}
			fmt.Printf("%s  %s\n", str(nodes[0]["id"]), str(nodes[0]["sp_title"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "card title")
	cmd.Flags().StringVar(&kind, "kind", "md", "md | html | svg | plain")
	cmd.Flags().StringVar(&file, "file", "", "read content from this file")
	cmd.Flags().StringVar(&text, "text", "", "inline content")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func cardsCmd() *cobra.Command {
	var asJSON, deleted bool
	cmd := &cobra.Command{
		Use:   "cards <slug>",
		Short: "List cards: id, title, kind, created_by",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			canvas, err := newClient().GetCanvas(args[0], deleted)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(canvas.Nodes)
			}
			for _, n := range canvas.Nodes {
				kind := str(n["sp_kind"])
				if kind == "" {
					kind = str(n["type"])
				}
				var flags []string
				if str(n["sp_deleted_at"]) != "" {
					flags = append(flags, "deleted")
				}
				if by := str(n["sp_superseded_by"]); by != "" {
					flags = append(flags, "superseded by "+by)
				}
				suffix := ""
				if len(flags) > 0 {
					suffix = "  (" + strings.Join(flags, "; ") + ")"
				}
				rev := n["sp_rev"]
				if rev == nil {
					rev = 1
				}
				fmt.Printf("%s  %-28s  %-5s  %s  rev %v%s\n",
					str(n["id"]), str(n["sp_title"]), kind, str(n["sp_created_by"]),
					rev, suffix)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&deleted, "deleted", false, "include deleted cards")
	return cmd
}

func updateCmd() *cobra.Command {
	var file, text, title, mode, kind string
	var ifMatch int64
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "update <slug> <card_id> [source]",
		Short: "Replace a card's content",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := third(args)
			patch := map[string]any{}
			if cmd.Flags().Changed("text") || source != "" || file != "" {
				content := text
				if !cmd.Flags().Changed("text") {
					var err error
					if content, err = readSource(source, file); err != nil {
						return err
					}
				}
				patch["text"] = content
			}
			if cmd.Flags().Changed("title") {
				patch["sp_title"] = title
			}
			if cmd.Flags().Changed("kind") {
				patch["sp_kind"] = kind
			}
			if len(patch) == 0 {
				return usage("nothing to update: pass a file, --text, --title or --kind")
			}
			var match *int64
			if cmd.Flags().Changed("if-match") {
				match = &ifMatch
			}
			node, err := newClient().UpdateCard(args[0], args[1], patch, mode, match)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(node)
			}
			fmt.Printf("%s  rev %v\n", str(node["id"]), node["sp_rev"])
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read content from this file")
	cmd.Flags().StringVar(&text, "text", "", "inline content")
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&kind, "kind", "", "md | html | svg | plain")
	cmd.Flags().StringVar(&mode, "mode", "", "replace | branch")
	cmd.Flags().Int64Var(&ifMatch, "if-match", 0, "the sp_rev you read")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func rmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <slug> <card_id>",
		Short: "Delete a card (soft; the agent still sees that you removed it)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DeleteCard(args[0], args[1]); err != nil {
				return fail(err)
			}
			return nil
		},
	}
}

func linkCmd() *cobra.Command {
	var label string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "link <slug> <from_id> <to_id>",
		Short: "Link two cards. Always label: unlabelled edges are noise.",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			edge, err := newClient().LinkCards(args[0], args[1], args[2], label)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(edge)
			}
			suffix := ""
			if label != "" {
				suffix = fmt.Sprintf("  '%s'", label)
			}
			fmt.Printf("%s  %s -> %s%s\n", str(edge["id"]), args[1], args[2], suffix)
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "what the edge means")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func unlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <slug> <link_id>",
		Short: "Remove a link",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := newClient().DeleteLink(args[0], args[1]); err != nil {
				return fail(err)
			}
			return nil
		},
	}
}

// --- export / import -----------------------------------------------------------

func exportCmd() *cobra.Command {
	var deleted bool
	cmd := &cobra.Command{
		Use:   "export <slug>",
		Short: "Write the space as JSON Canvas on stdout. Opens in Obsidian.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			canvas, err := newClient().GetCanvas(args[0], deleted)
			if err != nil {
				return fail(err)
			}
			return out(canvas)
		},
	}
	cmd.Flags().BoolVar(&deleted, "deleted", false, "include deleted cards")
	return cmd
}

func importCmd() *cobra.Command {
	var file string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "import <slug> [source]",
		Short: "Merge a JSON Canvas file into a space. Additive: never deletes.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := second(args)
			if source == "" {
				source = stdinMarker
			}
			raw, err := readSource(source, file)
			if err != nil {
				return err
			}
			var canvas client.Canvas
			dec := json.NewDecoder(strings.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&canvas); err != nil {
				return usage("not a JSON Canvas document: %v", err)
			}
			result, err := newClient().ImportCanvas(args[0], canvas)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(result)
			}
			fmt.Printf("imported %d cards, %d links\n",
				len(result.Canvas.Nodes), len(result.Canvas.Edges))
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the canvas from this file")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func uploadCmd() *cobra.Command {
	var title string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "upload <slug> <path>",
		Short: "Upload an image and place it as a JSON Canvas file node",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			media, err := a.UploadMedia(args[0], args[1], "")
			if err != nil {
				return fail(err)
			}
			if title == "" {
				title = filepath.Base(args[1])
			}
			nodes, err := a.CreateNodes(args[0], []client.Node{{
				"type": "file", "file": media.URL, "sp_title": title,
				"width": 360, "height": 280}})
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(nodes[0])
			}
			fmt.Printf("%s  %s\n", str(nodes[0]["id"]), str(nodes[0]["file"]))
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "card title; defaults to the filename")
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

// --- helpers ---------------------------------------------------------------------

func str(v any) string {
	s, _ := v.(string)
	return s
}

func second(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}

func third(args []string) string {
	if len(args) > 2 {
		return args[2]
	}
	return ""
}

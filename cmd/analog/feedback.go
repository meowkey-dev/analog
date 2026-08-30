package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/client"
)

func renderFeedback(f client.Feedback) {
	if f.Summary == "" {
		return // SPEC §4.2: silence means nothing changed
	}
	fmt.Println(f.Summary)

	if len(f.Annotations) > 0 {
		fmt.Println("\ncomments")
		for _, a := range f.Annotations {
			flags := ""
			if a.Stale {
				flags = " (stale)"
			}
			subject := a.CardTitle
			if subject == "" {
				subject = a.CardID
			}
			fmt.Printf("  %s  [%s] %s%s  · %s\n",
				a.ID, a.Motivation, subject, flags, a.Creator)
			lines := strings.Split(a.Body, "\n")
			if len(lines) == 0 {
				lines = []string{""}
			}
			for _, line := range lines {
				fmt.Printf("      %s\n", line)
			}
			fmt.Printf("      resolve: analog resolve %s --reply \"...\"\n", a.ID)
		}
	}

	if len(f.Replies) > 0 {
		fmt.Println("\nreplies on resolve")
		for _, r := range f.Replies {
			subject := str(r["card_title"])
			if subject == "" {
				subject = str(r["card_id"])
			}
			fmt.Printf("  %s  [%s] %s  · %v\n",
				str(r["id"]), str(r["motivation"]), subject, r["actor"])
			for _, line := range strings.Split(str(r["body"]), "\n") {
				fmt.Printf("      %s\n", line)
			}
			fmt.Printf("      reply: %s\n", str(r["reply"]))
		}
	}

	for _, section := range []struct {
		heading string
		rows    []map[string]any
	}{
		{"cards edited", f.CardsEdited},
		{"cards deleted", f.CardsDeleted},
		{"cards moved", f.CardsMoved},
	} {
		if len(section.rows) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", section.heading)
		for _, c := range section.rows {
			changed := ""
			if list, ok := c["changed"].([]any); ok && len(list) > 0 {
				parts := make([]string, 0, len(list))
				for _, v := range list {
					parts = append(parts, str(v))
				}
				changed = "  (" + strings.Join(parts, ", ") + ")"
			}
			fmt.Printf("  %s  %s%s  · %v\n", str(c["id"]), str(c["title"]), changed,
				c["actor"])
		}
	}

	if len(f.LinksAdded) > 0 {
		fmt.Println("\nlinks added")
		for _, l := range f.LinksAdded {
			label := ""
			if v := str(l["label"]); v != "" {
				label = fmt.Sprintf("  \"%s\"", v)
			}
			fmt.Printf("  %s  %v -> %v%s  · %v\n",
				str(l["id"]), l["from"], l["to"], label, l["actor"])
		}
	}
	if len(f.LinksRemoved) > 0 {
		fmt.Println("\nlinks removed")
		for _, l := range f.LinksRemoved {
			fmt.Printf("  %s  · %v\n", str(l["id"]), l["actor"])
		}
	}
}

func feedbackCmd() *cobra.Command {
	var asJSON, watch, peek bool
	var since int64
	cmd := &cobra.Command{
		Use:   "feedback <slug>",
		Short: "What changed since this actor last looked",
		Long: "What changed since this actor last looked. Prints nothing if nothing " +
			"did.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			var from *int64
			if cmd.Flags().Changed("since") {
				from = &since
			}
			first, err := a.GetFeedback(args[0], from, !peek)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				if err := out(first); err != nil {
					return err
				}
			} else {
				renderFeedback(first)
			}
			if !watch {
				return nil
			}

			return a.StreamEvents(context.Background(), args[0], first.Cursor,
				func(event client.Event) error {
					if event.Actor == a.Actor {
						return nil
					}
					time.Sleep(200 * time.Millisecond) // coalesce a burst into one report
					delta, err := a.GetFeedback(args[0], nil, !peek)
					if err != nil {
						return err
					}
					if delta.Summary == "" {
						return nil
					}
					if asJSON {
						if err := out(delta); err != nil {
							return err
						}
					} else {
						renderFeedback(delta)
					}
					return os.Stdout.Sync()
				})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&watch, "watch", false, "keep reporting as things change")
	cmd.Flags().Int64Var(&since, "since", 0, "replay from this seq")
	cmd.Flags().BoolVar(&peek, "peek", false, "do not advance the cursor")
	return cmd
}

// --- annotations ------------------------------------------------------------------

func commentsCmd() *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:   "comments <slug>",
		Short: "List annotations on a space",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var resolved *bool
			if !all {
				open := false
				resolved = &open
			}
			rows, err := newClient().ListAnnotations(args[0], resolved, "")
			if err != nil {
				return fail(err)
			}
			if asJSON {
				return out(rows)
			}
			for _, a := range rows {
				marks := []byte("  ")
				if a.Resolved {
					marks[0] = '*'
				}
				if a.Stale {
					marks[1] = '~'
				}
				subject := a.CardTitle
				if subject == "" {
					subject = a.CardID
				}
				fmt.Printf("%s %s [%s] %s: %s\n",
					a.ID, string(marks), a.Motivation, subject, a.Body)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&all, "all", false, "include resolved")
	return cmd
}

func resolveCmd() *cobra.Command {
	var reply, slug string
	cmd := &cobra.Command{
		Use:   "resolve <annotation_id>",
		Short: "Mark an annotation done. Don't resolve what you haven't acted on.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			var replyPtr *string
			if cmd.Flags().Changed("reply") {
				replyPtr = &reply
			}
			target := slug
			if target == "" {
				target = a.ConfigSpace
			}
			if target == "" {
				// SPEC §4.2 spells `analog resolve a_7f` with no slug at all.
				found, _, err := a.FindAnnotation(args[0])
				if err != nil {
					return fail(err)
				}
				target = found
			}
			annotation, err := a.ResolveAnnotation(target, args[0], replyPtr, true)
			if err != nil {
				return fail(err)
			}
			fmt.Printf("resolved %s in %s\n", annotation.ID, target)
			return nil
		},
	}
	cmd.Flags().StringVar(&reply, "reply", "", "what you did about it")
	cmd.Flags().StringVar(&slug, "space", "", "the space it is in")
	return cmd
}

// --- events -------------------------------------------------------------------------

func eventsCmd() *cobra.Command {
	var asJSON, watch bool
	var since int64
	cmd := &cobra.Command{
		Use:   "events <slug>",
		Short: "Raw event log. Mostly for debugging the cursor.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			page, err := a.ListEvents(args[0], since, 0)
			if err != nil {
				return fail(err)
			}
			if asJSON {
				if err := out(page); err != nil {
					return err
				}
			} else {
				for _, e := range page.Events {
					printEvent(e)
				}
			}
			if !watch {
				return nil
			}
			return a.StreamEvents(context.Background(), args[0], page.Cursor,
				func(event client.Event) error {
					if asJSON {
						return out(event)
					}
					printEvent(event)
					return os.Stdout.Sync()
				})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.Flags().BoolVar(&watch, "watch", false, "keep printing as events arrive")
	cmd.Flags().Int64Var(&since, "since", 0, "start after this seq")
	return cmd
}

func printEvent(e client.Event) {
	fmt.Printf("%4d  %s  %-19s %-28s %s\n",
		e.Seq, e.TS, e.Type, e.SubjectID, e.Actor)
}

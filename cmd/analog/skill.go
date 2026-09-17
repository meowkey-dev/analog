package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/skill"
)

// The `skill` group manages the installed copy of the skill without an actor in
// sight. `onboard` installs it too, but onboard is about one agent's wiring and
// politely skips a skill that is already there (#63). Refreshing after a binary
// upgrade is a different job: no identity, always overwrite, and say whether
// anything changed — which is also what a CI step or a human wants to know.
func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install and check the agent skill shipped inside this binary",
		Long: "Install and check the agent skill shipped inside this binary. The skill " +
			"is the workflow half of Analog (SKILL.md) and the card style (STYLE.md); " +
			"installed copies do not update themselves, so run `skill install` after " +
			"upgrading.",
	}
	cmd.AddCommand(skillInstallCmd(), skillStatusCmd(), skillCatCmd())
	return cmd
}

func skillInstallCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Copy the embedded skill into place, replacing what is there",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			into, err := skillDir(dir)
			if err != nil {
				return fail(err)
			}
			before, err := skillStatus(filepath.Join(into, "analog"))
			if err != nil {
				return fail(err)
			}
			target, err := installSkill(into)
			if err != nil {
				return fail(err)
			}
			switch before {
			case skillCurrent:
				fmt.Printf("skill unchanged: %s\n", target)
			case skillMissing:
				fmt.Printf("skill installed: %s\n", target)
			default:
				fmt.Printf("skill updated: %s\n", target)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "skills directory (default ~/.claude/skills)")
	return cmd
}

func skillStatusCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Compare the installed skill with the embedded one; exit 1 if it is stale or missing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			into, err := skillDir(dir)
			if err != nil {
				return fail(err)
			}
			target := filepath.Join(into, "analog")
			state, err := skillStatus(target)
			if err != nil {
				return fail(err)
			}
			fmt.Printf("skill %s: %s\n", state, target)
			if state != skillCurrent {
				fmt.Println("  run `analog skill install` to refresh it")
				os.Exit(exitError)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "skills directory (default ~/.claude/skills)")
	return cmd
}

// cat is for agents without a skill loader: the README says to paste the skill
// into the conversation, and this is the copy that matches the binary.
func skillCatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cat [file]",
		Short: "Print SKILL.md (or another file of the skill) to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "SKILL.md"
			if len(args) == 1 {
				name = args[0]
			}
			body, err := fs.ReadFile(skill.FS(), name)
			if err != nil {
				names, _ := skillFiles()
				return fail(fmt.Errorf("no %s in the skill; it has: %v", name, names))
			}
			_, err = os.Stdout.Write(body)
			return err
		},
	}
}

// skillDir is where the skill folder lives: an explicit --dir, else the same
// default `onboard` uses, so the two commands manage the same copy.
func skillDir(explicit string) (string, error) {
	dir, _, err := installDir(explicit)
	return dir, err
}

type skillState string

const (
	skillCurrent skillState = "current"
	skillStale   skillState = "stale"
	skillMissing skillState = "missing"
)

// skillStatus compares an installed folder with the embedded skill file by file.
// A folder with extra files counts as stale: the embedded set is the whole skill,
// and install replaces the folder rather than merging into it.
func skillStatus(target string) (skillState, error) {
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return skillMissing, nil
		}
		return "", err
	}
	names, err := skillFiles()
	if err != nil {
		return "", err
	}
	for _, name := range names {
		want, err := fs.ReadFile(skill.FS(), name)
		if err != nil {
			return "", err
		}
		got, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
		if err != nil || string(got) != string(want) {
			return skillStale, nil
		}
	}
	embedded := map[string]bool{}
	for _, name := range names {
		embedded[name] = true
	}
	extra := false
	err = filepath.WalkDir(target, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		if !embedded[filepath.ToSlash(rel)] {
			extra = true
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if extra {
		return skillStale, nil
	}
	return skillCurrent, nil
}

func skillFiles() ([]string, error) {
	var names []string
	err := fs.WalkDir(skill.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, path)
		return nil
	})
	sort.Strings(names)
	return names, err
}

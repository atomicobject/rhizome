package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/agentchat"
	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/atomicobject/rhizome/pkg/app/tasks"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/spf13/cobra"
)

func registerTasksFamily(rootCmd *cobra.Command) {
	rootCmd.AddCommand(newTasksCmd(registeredHarnesses))
}

func newTasksCmd(registry harnessRegistry) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "tasks",
		Short: "Run and manage this repository's scheduled tasks",
		Long: "Scheduled tasks are declared in .rhizome/tasks/*.yml on the primary branch. " +
			"Each runs an optional free check, then a prompt in your agent harness, " +
			"automatically or after you act on its inbox item.",
		Args: cobra.NoArgs,
	}
	cmd.PersistentFlags().BoolVar(&asJSON, "json", false, "print JSON")
	open := func(cmd *cobra.Command) (*tasks.Service, error) {
		return openTasksService(cmd.Context(), cmd, registry)
	}

	cmd.AddCommand(&cobra.Command{
		Use: "status", Short: "Show each task's mode, last run, and next due time", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			report, err := svc.Status(cmd.Context())
			if err != nil {
				return err
			}
			return printTasks(cmd, asJSON, report, func(w io.Writer) { renderTaskStatus(w, report) })
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "inbox", Short: "List tasks that need you", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			items, err := svc.Inbox()
			if err != nil {
				return err
			}
			return printTasks(cmd, asJSON, items, func(w io.Writer) { renderInbox(w, items) })
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use: "run-due", Short: "Run every task that is due, once per repository", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			summary, err := svc.RunDue(cmd.Context())
			if err != nil {
				return err
			}
			return printTasks(cmd, asJSON, summary, func(w io.Writer) { renderRunSummary(w, summary) })
		},
	})
	cmd.AddCommand(newTasksRunCmd(open, &asJSON))
	cmd.AddCommand(newTasksStartCmd(registry, open))
	cmd.AddCommand(newTasksSnoozeCmd(open))
	for _, action := range []struct {
		use, short string
		fn         func(*tasks.Service, context.Context, string) error
	}{
		{"skip <item>", "Skip a due task until its next cadence", (*tasks.Service).Skip},
		{"discard <item>", "Remove a finished run's worktree and branch", (*tasks.Service).Discard},
		{"done <item>", "Mark an item done", (*tasks.Service).Done},
	} {
		cmd.AddCommand(&cobra.Command{
			Use: action.use, Short: action.short, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				svc, err := open(cmd)
				if err != nil {
					return err
				}
				return action.fn(svc, cmd.Context(), args[0])
			},
		})
	}
	cmd.AddCommand(&cobra.Command{
		Use: "mode <task> auto|notify|off|default", Short: "Choose whether a task runs automatically, notifies, or is off", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			return svc.SetMode(cmd.Context(), args[0], args[1])
		},
	})
	for _, p := range []struct {
		use, short string
		paused     bool
	}{{"pause", "Stop all background task work for you here", true}, {"resume", "Resume background task work", false}} {
		cmd.AddCommand(&cobra.Command{
			Use: p.use, Short: p.short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				svc, err := open(cmd)
				if err != nil {
					return err
				}
				return svc.SetPaused(cmd.Context(), p.paused)
			},
		})
	}
	for _, sub := range cmd.Commands() {
		sub.SilenceUsage = true
	}
	return cmd
}

func newTasksRunCmd(open func(*cobra.Command) (*tasks.Service, error), asJSON *bool) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "run [<item>]",
		Short: "Do a due task's work in the background now, or try a draft task file with --file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (file == "") == (len(args) == 0) {
				return errors.New("name an inbox item, or pass --file with a draft task file")
			}
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			var outcome tasks.Outcome
			if file != "" {
				outcome, err = svc.RunDraft(cmd.Context(), file)
			} else {
				outcome, err = svc.Run(cmd.Context(), args[0])
				if tasks.NotStarted(err) {
					// The Tasks page launches this detached and shows the failure on the item.
					_ = svc.RecordLaunchError(context.WithoutCancel(cmd.Context()), args[0], err.Error())
				}
			}
			if err != nil {
				return err
			}
			return printTasks(cmd, *asJSON, outcome, func(w io.Writer) { renderOutcome(w, outcome) })
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "draft task file to run once, ignoring its cadence and mode")
	return cmd
}

func newTasksSnoozeCmd(open func(*cobra.Command) (*tasks.Service, error)) *cobra.Command {
	var span string
	cmd := &cobra.Command{
		Use: "snooze <item>", Short: "Hide an item for a while", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := tasks.ParseDuration(span)
			if err != nil {
				return fmt.Errorf("--for: %w", err)
			}
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			return svc.Snooze(cmd.Context(), args[0], time.Now().Add(d))
		},
	}
	cmd.Flags().StringVar(&span, "for", "1d", "how long to hide the item")
	return cmd
}

func newTasksStartCmd(registry harnessRegistry, open func(*cobra.Command) (*tasks.Service, error)) *cobra.Command {
	var printOnly bool
	var requested string
	cmd := &cobra.Command{
		Use:   "start <item>",
		Short: "Open a due task in your agent harness, in a new worktree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := open(cmd)
			if err != nil {
				return err
			}
			var kind harness.Kind
			if !printOnly {
				if kind, err = resolveTaskHarness(cmd, registry(), requested); err != nil {
					return err
				}
			}
			plan, err := svc.PlanStart(cmd.Context(), args[0], kind)
			if err != nil {
				return err
			}
			if printOnly {
				_, err := fmt.Fprint(cmd.OutOrStdout(), plan.Prompt)
				return err
			}
			for _, url := range plan.OpenPulls {
				fmt.Fprintf(cmd.ErrOrStderr(), "An open pull request already does this task: %s\n", url)
			}
			if len(plan.OpenPulls) > 0 && !confirm(cmd, "Start anyway? [y/N] ") {
				return nil
			}
			return launchTaskHarness(cmd, svc, plan)
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the prompt instead of starting a harness; changes nothing")
	cmd.Flags().StringVar(&requested, "harness", "", "harness to start (claude or codex); defaults to your agent.harness setting")
	return cmd
}

// launchTaskHarness runs the harness interactively in the main checkout. A
// harness that exits within a few seconds with an error never really
// started, so the item goes back to where it was.
func launchTaskHarness(cmd *cobra.Command, svc *tasks.Service, plan tasks.StartPlan) error {
	child := exec.CommandContext(cmd.Context(), plan.Argv[0], plan.Argv[1:]...)
	child.Dir = plan.Dir
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Reserve the item before launching, so a concurrent run cannot do the
	// same work; a launch that fails gives it back.
	if err := svc.MarkStarted(cmd.Context(), plan); err != nil {
		return err
	}
	started := time.Now()
	if err := child.Start(); err != nil {
		_ = svc.UndoStart(context.WithoutCancel(cmd.Context()), plan, "")
		return fmt.Errorf("start %s: %w", plan.Argv[0], err)
	}
	err := child.Wait()
	if err != nil && time.Since(started) < 10*time.Second {
		reason := fmt.Sprintf("%s exited at once: %v", plan.Argv[0], err)
		if plan.Harness == harness.KindClaude || plan.Harness == harness.KindCodex {
			reason += fmt.Sprintf(". If it asked you to trust this folder, run `%s` once in %s, accept, and start the task again.", plan.Argv[0], plan.Dir)
		}
		_ = svc.UndoStart(context.WithoutCancel(cmd.Context()), plan, reason)
		return errors.New(reason)
	}
	return nil
}

func resolveTaskHarness(cmd *cobra.Command, drivers map[harness.Kind]harness.Harness, requested string) (harness.Kind, error) {
	return agentchat.ResolveHarness(cmd.Context(), requested, drivers, func() (harness.Kind, error) {
		settings, err := agentchat.LoadSettings(commandEnvFromContext(cmd.Context()).Config)
		return settings.Harness, err
	})
}

// openTasksService composes the service for the repository the command runs in.
func openTasksService(ctx context.Context, cmd *cobra.Command, registry harnessRegistry) (*tasks.Service, error) {
	cwd, err := commandEnvFromContext(ctx).Getwd()
	if err != nil {
		return nil, err
	}
	trust, err := repositorytrust.DefaultStore()
	if err != nil {
		return nil, err
	}
	env := commandEnvFromContext(ctx).Config
	return tasks.Open(ctx, tasks.OpenOptions{
		Dir:      cwd,
		Drivers:  registry(),
		Settings: func() (agentchat.Settings, error) { return agentchat.LoadSettings(env) },
		Trusted:  trust.Trusted,
	})
}

// openTasksForPage opens the tasks service for the runtime's Tasks page.
// The page never runs prompts in process (Run spawns `rzm tasks run`), so it
// needs no harness drivers. The first successful open is reused.
// ponytail: config.yml changes need a runtime restart; reopen per request if that bites.
func openTasksForPage() func(context.Context, string) (web.TasksAPI, error) {
	var (
		mu      sync.Mutex
		service *tasks.Service
	)
	return func(ctx context.Context, vaultPath string) (web.TasksAPI, error) {
		mu.Lock()
		defer mu.Unlock()
		if service != nil {
			return service, nil
		}
		trust, err := repositorytrust.DefaultStore()
		if err != nil {
			return nil, err
		}
		opened, err := tasks.Open(ctx, tasks.OpenOptions{Dir: vaultPath, Trusted: trust.Trusted})
		if err != nil {
			return nil, err
		}
		service = opened
		return service, nil
	}
}

// startTasksRunDue is how a runtime asks for due tasks: a detached
// `rzm tasks run-due` in the vault, so the work outlives the runtime.
func startTasksRunDue(vaultPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return appruntime.StartDetached(exe, vaultPath, "tasks", "run-due")
}

func printTasks(cmd *cobra.Command, asJSON bool, v any, human func(io.Writer)) error {
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(cmd.OutOrStdout())
	return nil
}

func confirm(cmd *cobra.Command, question string) bool {
	fmt.Fprint(cmd.ErrOrStderr(), question)
	var answer string
	_, _ = fmt.Fscanln(cmd.InOrStdin(), &answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

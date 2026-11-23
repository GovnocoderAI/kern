// Package cli wires the command line surface.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/GovnocoderAI/kern/internal/agent"
	"github.com/GovnocoderAI/kern/internal/agents"
	appcore "github.com/GovnocoderAI/kern/internal/app"
	"github.com/GovnocoderAI/kern/internal/config"
	"github.com/GovnocoderAI/kern/internal/provider"
	"github.com/GovnocoderAI/kern/internal/server"
	"github.com/GovnocoderAI/kern/internal/session"
	"github.com/GovnocoderAI/kern/internal/setup"
	"github.com/GovnocoderAI/kern/internal/skill"
	"github.com/GovnocoderAI/kern/internal/tools"
	"github.com/GovnocoderAI/kern/internal/tui"
	"github.com/GovnocoderAI/kern/internal/ui"
	"github.com/GovnocoderAI/kern/internal/util"
	"github.com/GovnocoderAI/kern/internal/version"
)

var (
	flagModel string
	flagAuto  bool
	flagAgent string

	flagContinue bool
	flagSession  string
	flagFork     bool
	flagPrompt   string
	flagMini     bool
	flagSkill    string
)

var rootCmd = &cobra.Command{
	Use:           "kern [prompt..]",
	Short:         "Kern — the core of your terminal",
	Long:          "Kern is a fast, open-source coding agent for the terminal.\nIt reads opencode configuration and supports plusvibe, OpenAI, Anthropic, and Google providers.",
	Version:       version.String(),
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		// First-run wizard: if no global config exists, run setup before anything else.
		if !setup.IsInitialized() && isTTY() {
			if _, err := setup.Run(); err != nil {
				fmt.Fprintf(os.Stderr, "kern: setup: %v\n", err)
				fmt.Fprintf(os.Stderr, "kern: continuing without setup; run `kern init` to create a starter config\n")
			}
		}

		workdir := "."

		// Non-interactive mode: kern "prompt text" → run the prompt and exit.
		// Interactive mode: kern → start TUI.
		if len(args) > 0 && !flagMini && flagSession == "" && !flagContinue && !flagFork {
			prompt := strings.Join(args, " ")
			rt, err := newRuntime(workdir, runtimeOptions{
				sessionID:    flagSession,
				continueLast: flagContinue,
				fork:         flagFork,
				interactive:  false,
			})
			if err != nil {
				return err
			}
			defer rt.save()
			if err := rt.agent.Run(context.Background(), rt.runInput(prompt)); err != nil {
				return err
			}
			rt.sync()
			rt.console.Dim("session %s · %s/%s", rt.sess.ID, rt.providerID, rt.modelID)
			return nil
		}

		if isTTY() && !flagMini {
			return runTUI(workdir)
		}
		return runInteractive(workdir)
	},
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kern: "+err.Error())
		os.Exit(1)
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVarP(&flagModel, "model", "m", "", "model to use (provider/model)")
	pf.BoolVar(&flagAuto, "auto", false, "auto-approve permissions that are not explicitly denied")
	pf.BoolVarP(&flagContinue, "continue", "c", false, "continue the last session")
	pf.StringVarP(&flagSession, "session", "s", "", "session id to continue")
	pf.BoolVar(&flagFork, "fork", false, "fork the session when continuing")
	pf.StringVar(&flagPrompt, "prompt", "", "prompt to use")
	pf.BoolVar(&flagMini, "mini", false, "start the minimal interface")
	pf.StringVar(&flagSkill, "skill", "", "activate a skill by name for this run")
	pf.StringVar(&flagAgent, "agent", "", "role to run as: plan, build, test, review (default: orchestrator)")

	rootCmd.AddCommand(runCmd, modelsCmd, configCmd, doctorCmd, authCmd, sessionCmd, versionCmd, completionCmd, initCmd, setupCmd, miniCmd, tuiCmd, serveCmd, desktopCmd, statsCmd, agentCmd, skillCmd)
}

func isTTY() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) && isatty.IsTerminal(os.Stdout.Fd())
}

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}

// ---- runtime ----

type runtimeOptions struct {
	model        string
	agent        string
	sessionID    string
	continueLast bool
	fork         bool
	interactive  bool
	auto         bool
}

type app struct {
	core        *appcore.App
	cfg         *config.Config
	auth        *config.Auth
	agent       *agent.Agent
	sess        *session.Session
	store       *session.Store
	console     *ui.Console
	providerID  string
	modelID     string
	workdir     string
	skills      []skill.Skill
	activeSkill *skill.Skill
}

func newRuntime(workdir string, opts runtimeOptions) (*app, error) {
	modelRef := opts.model
	if modelRef == "" {
		modelRef = flagModel
	}
	agentRef := opts.agent
	if agentRef == "" {
		agentRef = flagAgent
	}
	core, err := appcore.New(appcore.Options{
		Workdir:     workdir,
		Model:       modelRef,
		Agent:       agentRef,
		SessionID:   opts.sessionID,
		Continue:    opts.continueLast,
		Fork:        opts.fork,
		Auto:        opts.auto || flagAuto,
		Interactive: opts.interactive,
	})
	if err != nil {
		return nil, err
	}
	rt := &app{
		core:       core,
		cfg:        core.Config,
		auth:       core.Auth,
		agent:      core.Agent,
		sess:       core.Session,
		store:      core.Store,
		console:    core.Console,
		providerID: core.ProviderID,
		modelID:    core.ModelID,
		workdir:    core.Workdir,
	}
	rt.skills = skill.Load(rt.workdir)
	// Pre-set activeSkill from the --skill flag if provided.
	if flagSkill != "" {
		for i := range rt.skills {
			if rt.skills[i].Name == flagSkill {
				rt.activeSkill = &rt.skills[i]
				break
			}
		}
		if rt.activeSkill == nil {
			rt.console.Warn("skill %q not found; see `kern skill list`", flagSkill)
		}
	}
	return rt, nil
}

func (r *app) sync() {
	r.core.Sync()
}

func (r *app) save() {
	if err := r.core.Save(); err != nil {
		r.console.Warn("could not save session: %v", err)
	}
}

func (r *app) rebuildModel(ref string) error {
	if err := r.core.RebuildModel(ref); err != nil {
		return err
	}
	r.agent = r.core.Agent
	r.sess = r.core.Session
	r.providerID = r.core.ProviderID
	r.modelID = r.core.ModelID
	return nil
}

// runSkillInput returns the RunInput for the given user text, attaching the
// active skill (from --skill flag or /skill command) to the system prompt.
func (r *app) runInput(text string) agent.RunInput {
	in := agent.RunInput{Text: text}
	if r.activeSkill != nil {
		in.Skill = r.activeSkill.Name
		in.SkillPrompt = r.activeSkill.Prompt
	}
	return in
}

func runTUI(workdir string) error {
	return tui.Run(appcore.Options{
		Workdir:   workdir,
		Model:     flagModel,
		Agent:     flagAgent,
		SessionID: flagSession,
		Continue:  flagContinue,
		Fork:      flagFork,
		Auto:      flagAuto,
	})
}

var tuiCmd = &cobra.Command{
	Use:   "tui [project]",
	Short: "Start the full-screen terminal interface",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir := "."
		if len(args) == 1 {
			workdir = args[0]
		}
		return runTUI(workdir)
	},
}

// ---- serve / desktop ----

var (
	flagServePort    int
	flagServeHost    string
	flagServeOpen    bool
	flagServeNoToken bool
)

var serveCmd = &cobra.Command{
	Use:   "serve [project]",
	Short: "Serve the session over HTTP/SSE for the web UI",
	Long: "Start a local HTTP server that streams turns over Server-Sent Events.\n" +
		"Prints a URL with an auth token; open it in a browser.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir := "."
		if len(args) == 1 {
			workdir = args[0]
		}
		return runServe(workdir, flagServeOpen)
	},
}

var desktopCmd = &cobra.Command{
	Use:   "desktop [project]",
	Short: "Serve and open the UI in an app window",
	Long: "Start the local server and open the web UI in a browser app window\n" +
		"(Edge/Chrome app mode), falling back to the default browser.",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir := "."
		if len(args) == 1 {
			workdir = args[0]
		}
		return runServe(workdir, true)
	},
}

func runServe(workdir string, open bool) error {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return err
	}
	token := ""
	if !flagServeNoToken {
		token = server.NewToken()
	}
	addr := fmt.Sprintf("%s:%d", flagServeHost, flagServePort)
	srv, err := server.New(server.Options{
		App: appcore.Options{
			Workdir: abs,
			Model:   flagModel,
			Agent:   flagAgent,
			Auto:    flagAuto,
		},
		Token: token,
		Addr:  addr,
	})
	if err != nil {
		return err
	}
	defer srv.Close()

	url, err := srv.Listen()
	if err != nil {
		return err
	}
	fmt.Printf("kern serve listening\n  %s\n", url)
	fmt.Println("press ctrl+c to stop")

	if open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "kern: could not open a window: %v\n", err)
			fmt.Println("open the URL above in your browser.")
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	<-ctx.Done()
	fmt.Println("\nbye")
	return nil
}

func init() {
	serveCmd.Flags().IntVar(&flagServePort, "port", 0, "port to listen on (0 = random)")
	serveCmd.Flags().StringVar(&flagServeHost, "host", "127.0.0.1", "host to bind")
	serveCmd.Flags().BoolVar(&flagServeOpen, "open", false, "open the UI in a browser")
	serveCmd.Flags().BoolVar(&flagServeNoToken, "no-token", false, "disable the auth token (localhost only)")
	desktopCmd.Flags().IntVar(&flagServePort, "port", 0, "port to listen on (0 = random)")
	desktopCmd.Flags().StringVar(&flagServeHost, "host", "127.0.0.1", "host to bind")
	desktopCmd.Flags().BoolVar(&flagServeNoToken, "no-token", false, "disable the auth token (localhost only)")
}

// ---- interactive ----

func runInteractive(workdirArg string) error {
	workdir := workdirArg
	if workdir == "" {
		workdir = "."
	}
	abs, err := filepath.Abs(workdir)
	if err != nil {
		return err
	}
	rt, err := newRuntime(abs, runtimeOptions{
		sessionID:    flagSession,
		continueLast: flagContinue,
		fork:         flagFork,
		interactive:  isTTY(),
	})
	if err != nil {
		return err
	}
	defer rt.save()

	c := rt.console
	c.Banner(version.Version, rt.providerID+"/"+rt.modelID, abs)
	c.Dim("Type /help for commands, /exit to quit. Use /model to switch models.")
	c.Println("")

	for {
		line, err := c.Prompt("❯ ")
		if errors.Is(err, io.EOF) {
			c.Println("")
			return nil
		}
		if err != nil {
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if rt.handleSlash(line) {
				return nil
			}
			continue
		}
		if err := rt.agent.Run(context.Background(), rt.runInput(line)); err != nil {
			c.Error("%v", err)
		}
		rt.sync()
		if err := rt.store.Save(rt.sess); err != nil {
			c.Warn("session: %v", err)
		}
		c.Println("")
	}
}

func (r *app) handleSlash(line string) bool {
	fields := strings.Fields(line)
	cmd := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(line, fields[0]))
	switch cmd {
	case "/exit", "/quit", "/q":
		return true
	case "/help":
		r.console.Println(helpText)
	case "/clear":
		r.agent.Reset()
		r.sess.Messages = nil
		r.sess.Usage = provider.Usage{}
		r.console.Success("conversation cleared")
	case "/model":
		if arg == "" {
			r.console.Info("model: %s/%s", r.providerID, r.modelID)
			break
		}
		if err := r.rebuildModel(arg); err != nil {
			r.console.Error("%v", err)
			break
		}
		r.console.Success("model: %s/%s", r.providerID, r.modelID)
	case "/models":
		r.printModels()
	case "/tools":
		r.console.Println(strings.Join(r.agent.Tools.Names(), ", "))
	case "/session":
		r.console.Info("%s  %s  %s", r.sess.ID, r.sess.Title, r.sess.Model)
	case "/cost":
		u := r.agent.Usage
		r.console.Info("tokens: in %d, out %d, total %d", u.InputTokens, u.OutputTokens, u.TotalTokens)
	case "/skill":
		if arg == "" {
			r.printSkills()
			break
		}
		if arg == "off" {
			r.activeSkill = nil
			r.console.Success("skill off")
			break
		}
		for i := range r.skills {
			if r.skills[i].Name == arg {
				r.activeSkill = &r.skills[i]
				r.console.Success("skill on: %s", arg)
				break
			}
		}
		if r.activeSkill == nil {
			r.console.Error("unknown skill %q; try /skill", arg)
		}
	default:
		r.console.Warn("unknown command %s; try /help", cmd)
	}
	return false
}

func (r *app) printSkills() {
	if len(r.skills) == 0 {
		r.console.Info("no skills found (create .kern/skills/*.md)")
		return
	}
	for _, s := range r.skills {
		mark := " "
		if r.activeSkill != nil && r.activeSkill.Name == s.Name {
			mark = "*"
		}
		r.console.Println(fmt.Sprintf("%s %s — %s", mark, s.Name, s.Description))
	}
}

func (r *app) printModels() {
	for _, pid := range r.cfg.ProviderIDs() {
		p := r.cfg.Providers[pid]
		marker := " "
		if pid == r.providerID {
			marker = "*"
		}
		r.console.Println(fmt.Sprintf("%s %s (%s)", marker, pid, p.Kind()))
		for _, mid := range p.SortedModels() {
			r.console.Println("    " + pid + "/" + mid)
		}
	}
}

const helpText = `Commands:
  /help              show this help
  /model [ref]       show or switch model (provider/model)
  /models            list configured models
  /tools             list available tools
  /session           show current session
  /cost              show token usage
  /skill [name]      list skills, or activate one by name
  /clear             clear the conversation
  /exit              quit

Tips:
  - Kern reads opencode.json/opencode.jsonc and kern.json automatically.
  - Use --auto to auto-approve tool permissions.
  - Use --skill <name> to activate a skill for this run.
  - Create .kern/skills/*.md files to define custom skills.
  - Run "kern run \"prompt\"" for non-interactive use.`

// ---- run ----

var runCmd = &cobra.Command{
	Use:   "run [message..]",
	Short: "Run a single prompt non-interactively",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := strings.TrimSpace(strings.Join(args, " "))
		if prompt == "" {
			prompt = flagPrompt
		}
		if prompt == "" {
			return errors.New("no prompt provided")
		}
		rt, err := newRuntime(cwd(), runtimeOptions{
			sessionID:    flagSession,
			continueLast: flagContinue,
			fork:         flagFork,
			interactive:  isTTY(),
		})
		if err != nil {
			return err
		}
		defer rt.save()
		if err := rt.agent.Run(context.Background(), rt.runInput(prompt)); err != nil {
			return err
		}
		rt.sync()
		rt.console.Dim("session %s · %s/%s", rt.sess.ID, rt.providerID, rt.modelID)
		return nil
	},
}

// ---- models ----

var modelsRemote bool

var modelsCmd = &cobra.Command{
	Use:   "models [provider]",
	Short: "List available models",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cwd())
		if err != nil {
			return err
		}
		auth := config.LoadAuth()
		filter := ""
		if len(args) == 1 {
			filter = args[0]
		}
		if len(cfg.Providers) == 0 {
			fmt.Println("no providers configured")
			return nil
		}
		for _, pid := range cfg.ProviderIDs() {
			if filter != "" && pid != filter {
				continue
			}
			p := cfg.Providers[pid]
			fmt.Printf("%s (%s) %s\n", pid, p.Kind(), p.Endpoint())
			local := p.SortedModels()
			if modelsRemote || len(local) == 0 {
				prov, err := provider.FromConfig(pid, p, cfg.APIKey(pid, auth))
				if err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					remote, rerr := prov.Models(ctx)
					cancel()
					if rerr == nil {
						for _, m := range remote {
							fmt.Printf("  %s/%s\n", pid, m.ID)
						}
						continue
					}
					if len(local) == 0 {
						fmt.Printf("  (could not fetch models: %v)\n", rerr)
					}
				}
			}
			for _, mid := range local {
				fmt.Printf("  %s/%s\n", pid, mid)
			}
		}
		return nil
	},
}

func init() {
	modelsCmd.Flags().BoolVar(&modelsRemote, "remote", false, "fetch models from provider APIs")
}

// ---- config ----

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect and validate configuration",
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print configuration locations and loaded sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("kern config:     %s\n", util.KernConfigDir())
		fmt.Printf("opencode config: %s\n", util.OpencodeConfigDir())
		fmt.Printf("kern data:       %s\n", util.KernDataDir())
		fmt.Printf("sessions:        %s\n", util.SessionsDir())
		cfg, err := config.Load(cwd())
		if err != nil {
			return err
		}
		fmt.Println("sources:")
		if len(cfg.Sources) == 0 {
			fmt.Println("  (none)")
		}
		for _, s := range cfg.Sources {
			fmt.Println("  " + s)
		}
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the resolved configuration with secrets masked",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cwd())
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(cfg.Redacted(), "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	},
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and the default model",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(cwd())
		if err != nil {
			return err
		}
		fmt.Printf("config: ok (%d file(s))\n", len(cfg.Sources))
		pid, mid, err := cfg.ResolveModel(flagModel)
		if err != nil {
			fmt.Printf("model:  %v\n", err)
			return nil
		}
		fmt.Printf("model:  %s/%s\n", pid, mid)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configPathCmd, configShowCmd, configValidateCmd)
}

// ---- doctor ----

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the environment, configuration and providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Kern doctor")
		fmt.Println("===========")
		fmt.Printf("version:  %s\n", version.String())
		fmt.Printf("platform: %s/%s\n", osName(), osArch())
		fmt.Printf("workdir:  %s\n", cwd())
		fmt.Printf("terminal: %s\n", boolText(isTTY(), "interactive", "non-interactive"))

		cfg, err := config.Load(cwd())
		if err != nil {
			fmt.Printf("config:   error: %v\n", err)
			return nil
		}
		fmt.Printf("config:   ok (%d source(s))\n", len(cfg.Sources))
		for _, s := range cfg.Sources {
			fmt.Println("          " + s)
		}

		auth := config.LoadAuth()
		if len(cfg.Providers) == 0 {
			fmt.Println("providers: none configured")
		}
		for _, pid := range cfg.ProviderIDs() {
			p := cfg.Providers[pid]
			key := cfg.APIKey(pid, auth)
			fmt.Printf("provider: %s (%s)\n", pid, p.Kind())
			fmt.Printf("          endpoint %s\n", p.Endpoint())
			fmt.Printf("          key      %s\n", boolText(key != "", "present", "missing"))
			fmt.Printf("          models   %d configured\n", len(p.Models))
		}
		if pid, mid, err := cfg.ResolveModel(flagModel); err == nil {
			fmt.Printf("model:    %s/%s\n", pid, mid)
		} else {
			fmt.Printf("model:    %v\n", err)
		}
		if path, err := exec.LookPath("git"); err == nil {
			fmt.Printf("git:      %s\n", path)
		} else {
			fmt.Println("git:      not found")
		}
		fmt.Printf("tools:    %d built in\n", len(tools.NewRegistry(cwd(), "").Names()))

		if len(cfg.External) > 0 {
			fmt.Println("discovered credentials (other tools):")
			for _, ec := range cfg.External {
				fmt.Printf("  %-10s %-10s %s\n", ec.ProviderID, ec.Tool, ec.Path)
			}
		}
		fmt.Printf("agents:   %s\n", strings.Join(agents.Names(), ", "))
		return nil
	},
}

func osName() string { return runtime.GOOS }
func osArch() string { return runtime.GOARCH }

func boolText(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

// ---- auth ----

var authCmd = &cobra.Command{
	Use:     "auth",
	Aliases: []string{"providers"},
	Short:   "Manage provider credentials",
}

var authSetCmd = &cobra.Command{
	Use:   "set <provider> <key>",
	Short: "Store a provider API key (use - to read from stdin)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		providerID, key := args[0], args[1]
		if key == "-" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			key = strings.TrimSpace(string(data))
		}
		if key == "" {
			return errors.New("empty key")
		}
		if err := config.SetKey(providerID, key); err != nil {
			return err
		}
		fmt.Printf("stored key for %s\n", providerID)
		return nil
	},
}

var authListCmd = &cobra.Command{
	Use:   "list",
	Short: "List providers with stored keys",
	RunE: func(cmd *cobra.Command, args []string) error {
		a := config.LoadAuth()
		ids := a.List()
		if len(ids) == 0 {
			fmt.Println("no stored credentials")
			return nil
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return nil
	},
}

var authRemoveCmd = &cobra.Command{
	Use:   "remove <provider>",
	Short: "Remove a stored provider key",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.RemoveKey(args[0]); err != nil {
			return err
		}
		fmt.Printf("removed key for %s\n", args[0])
		return nil
	},
}

func init() {
	authCmd.AddCommand(authSetCmd, authListCmd, authRemoveCmd)
}

// ---- session ----

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage saved sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		store := session.NewStore()
		list, err := store.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no sessions")
			return nil
		}
		for _, s := range list {
			fmt.Printf("%s  %s  %s  %s\n", s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.Model, s.Title)
		}
		return nil
	},
}

var sessionShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show a session transcript",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store := session.NewStore()
		s, err := store.Load(args[0])
		if err != nil {
			return err
		}
		fmt.Printf("%s  %s\n%s\n\n", s.ID, s.Title, s.Model)
		for _, m := range s.Messages {
			fmt.Printf("[%s] %s\n", m.Role, m.Text())
			for _, p := range m.Parts {
				if p.Type == provider.PartToolCall && p.ToolCall != nil {
					fmt.Printf("  -> %s %s\n", p.ToolCall.Name, string(p.ToolCall.Arguments))
				}
				if p.Type == provider.PartToolResult && p.ToolResult != nil {
					fmt.Printf("  <- %s: %s\n", p.ToolResult.Name, preview(p.ToolResult.Content, 200))
				}
			}
		}
		fmt.Printf("\ntokens: in %d, out %d, total %d\n", s.Usage.InputTokens, s.Usage.OutputTokens, s.Usage.TotalTokens)
		return nil
	},
}

var sessionDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return session.NewStore().Delete(args[0])
	},
}

var sessionExportCmd = &cobra.Command{
	Use:   "export <id> [file]",
	Short: "Export a session as JSON",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := session.NewStore().Load(args[0])
		if err != nil {
			return err
		}
		raw, err := json.MarshalIndent(s, "", "  ")
		if err != nil {
			return err
		}
		if len(args) == 2 {
			return os.WriteFile(args[1], raw, 0o644)
		}
		fmt.Println(string(raw))
		return nil
	},
}

var sessionImportCmd = &cobra.Command{
	Use:   "import <file>",
	Short: "Import a session from JSON",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		raw, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		var s session.Session
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		if s.ID == "" {
			return errors.New("session JSON has no id")
		}
		if err := session.NewStore().Save(&s); err != nil {
			return err
		}
		fmt.Printf("imported %s\n", s.ID)
		return nil
	},
}

func init() {
	sessionCmd.AddCommand(sessionListCmd, sessionShowCmd, sessionDeleteCmd, sessionExportCmd, sessionImportCmd)
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ---- mini / stats / agent ----

var miniCmd = &cobra.Command{
	Use:   "mini [project]",
	Short: "Start the minimal interactive interface",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		workdir := "."
		if len(args) == 1 {
			workdir = args[0]
		}
		return runInteractive(workdir)
	},
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show token usage and session statistics",
	RunE: func(cmd *cobra.Command, args []string) error {
		list, err := session.NewStore().List()
		if err != nil {
			return err
		}
		var in, out, total int
		byModel := map[string]int{}
		for _, s := range list {
			in += s.Usage.InputTokens
			out += s.Usage.OutputTokens
			total += s.Usage.TotalTokens
			byModel[s.Model]++
		}
		fmt.Printf("sessions: %d\n", len(list))
		fmt.Printf("tokens:   in %d, out %d, total %d\n", in, out, total)
		if len(byModel) > 0 {
			fmt.Println("sessions by model:")
			models := make([]string, 0, len(byModel))
			for m := range byModel {
				models = append(models, m)
			}
			sort.Strings(models)
			for _, m := range models {
				fmt.Printf("  %-40s %d\n", m, byModel[m])
			}
		}
		return nil
	},
}

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "List agent roles and any configured custom agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Roles (use with --agent or the task tool):")
		for _, r := range agents.Defaults {
			fmt.Println("  " + r.Describe())
		}
		fmt.Println()
		fmt.Println("The orchestrator delegates to these roles through the task tool.")

		cfg, err := config.Load(cwd())
		if err != nil {
			return err
		}
		if len(cfg.Agents) > 0 {
			fmt.Println()
			fmt.Println("Configured agents (from config):")
			ids := make([]string, 0, len(cfg.Agents))
			for id := range cfg.Agents {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				a := cfg.Agents[id]
				fmt.Printf("  %s  model=%s mode=%s  %s\n", id, a.Model, a.Mode, a.Description)
			}
		}
		return nil
	},
}

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "List and validate skills",
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available skills (project + global)",
	RunE: func(cmd *cobra.Command, args []string) error {
		skills := skill.Load(cwd())
		if len(skills) == 0 {
			fmt.Println("no skills found (create .kern/skills/*.md)")
			return nil
		}
		for _, s := range skills {
			fmt.Printf("%s  %s  %s\n", s.Name, s.Description, s.Path)
		}
		return nil
	},
}

var skillValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate all skill files",
	RunE: func(cmd *cobra.Command, args []string) error {
		skills := skill.Load(cwd())
		problems := skill.Validate(skills)
		if len(problems) == 0 {
			fmt.Printf("ok: %d skill(s) valid\n", len(skills))
			return nil
		}
		for _, p := range problems {
			fmt.Println("problem: " + p)
		}
		return fmt.Errorf("%d problem(s) found", len(problems))
	},
}

func init() {
	skillCmd.AddCommand(skillListCmd, skillValidateCmd)
}

// ---- version / completion / init ----

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Print(version.Detailed())
	},
}

var completionCmd = &cobra.Command{
	Use:       "completion [bash|zsh|fish|powershell]",
	Short:     "Generate a shell completion script",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up Kern: theme, provider, API key and model",
	Long: "Run the interactive wizard that writes ~/.kern/config/kern.json and\n" +
		"stores the provider key in ~/.kern/config/auth.json.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isTTY() {
			return fmt.Errorf("the wizard needs an interactive terminal; use `kern auth set <provider> <key>` instead")
		}
		if setup.IsInitialized() {
			fmt.Printf("Kern is already configured (%s).\n", filepath.Join(util.KernConfigDir(), "kern.json"))
			fmt.Print("Reconfigure it? [y/N]: ")
			var answer string
			fmt.Scanln(&answer)
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				fmt.Println("Run `kern auth list` to check stored keys.")
				return nil
			}
		}
		_, err := setup.RunWith(os.Stdin, os.Stdout, true)
		return err
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Alias for `kern init` (configure theme, provider and keys)",
	RunE:  initCmd.RunE,
}

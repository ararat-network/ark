package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/template"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/transform"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cmtcfg "github.com/cometbft/cometbft/config"

	"cosmossdk.io/tools/confix"
	confixcmd "cosmossdk.io/tools/confix/cmd"

	"github.com/cosmos/cosmos-sdk/client"

	"github.com/ararat-network/ark/pkg/fsutil"
)

// newConfigCmd adapts confix migrate, diff, and set to Ark's template and readAppConfig
// validation, and adds validate over the same checks. SDK-only templates omit
// Ark's pricefeed and prometheus tables.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Utilities for managing application configuration",
	}
	cmd.AddCommand(
		newConfigMigrateCmd(),
		newConfigDiffCmd(),
		confixcmd.GetCommand(),
		newConfigSetCmd(),
		newConfigValidateCmd(),
		confixcmd.ViewCommand(),
		confixcmd.HomeCommand(),
	)
	return cmd
}

const (
	flagStdout       = "stdout"
	flagVerbose      = "verbose"
	flagSkipValidate = "skip-validate"
)

// configWriteFlags are confix's flags on the commands that write a file.
type configWriteFlags struct {
	stdout, verbose, skipValidate bool
}

func (f *configWriteFlags) bind(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.stdout, flagStdout, false, "print the updated config instead of writing it")
	cmd.Flags().BoolVarP(&f.verbose, flagVerbose, "v", false, "log each change to stderr")
	cmd.Flags().BoolVarP(&f.skipValidate, flagSkipValidate, "s", false, "write without validating the result")
}

func newConfigMigrateCmd() *cobra.Command {
	var flags configWriteFlags
	cmd := &cobra.Command{
		Use:   "migrate [app.toml]",
		Short: "Bring app.toml up to the tables and keys this binary knows",
		Long: `Bring app.toml up to the tables and keys this binary knows. The file is
compared with the one arkd init writes: missing tables and keys are added with
their defaults and comments, keys this version has no use for are removed, and
every value the file already has is kept. The result must pass the validation
start applies. Defaults to the home directory's app.toml.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := appTOMLPath(cmd, args)
			if err != nil {
				return err
			}
			doc, err := confix.LoadConfig(path)
			if err != nil {
				return err
			}
			target, err := defaultAppTOML()
			if err != nil {
				return err
			}
			plan := appConfigPlan(doc, target)
			if len(plan) == 0 && !flags.stdout {
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is current: nothing to add or remove\n", path)
				return err
			}
			return applyConfigPlan(cmd, doc, plan, path, validateAppTOML, flags)
		},
	}
	flags.bind(cmd)
	return cmd
}

func newConfigDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff [app.toml]",
		Short: "Show how app.toml differs from the file this binary writes",
		Long: `Show how app.toml differs from the file arkd init writes: what migrate would
add and remove, then the keys whose values differ from the defaults, which
migrate keeps. Defaults to the home directory's app.toml.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := appTOMLPath(cmd, args)
			if err != nil {
				return err
			}
			doc, err := confix.LoadConfig(path)
			if err != nil {
				return err
			}
			target, err := defaultAppTOML()
			if err != nil {
				return err
			}
			plan := appConfigPlan(doc, target)
			kept := changedValues(doc, target)
			out := cmd.OutOrStdout()
			if len(plan) == 0 && len(kept) == 0 {
				_, err := fmt.Fprintf(out, "%s matches this binary's defaults\n", path)
				return err
			}
			for _, step := range plan {
				fmt.Fprintln(out, step.Desc)
			}
			for _, kv := range kept {
				fmt.Fprintf(out, "keep %s = %s (default %s)\n", kv.key, kv.value, kv.def)
			}
			return nil
		},
	}
}

func newConfigSetCmd() *cobra.Command {
	var flags configWriteFlags
	cmd := &cobra.Command{
		Use:   "set [config] [key] [value]",
		Short: "Set a config value",
		Long: `Set one value in a config file, named without its extension: app, config,
or client, or given as a path to a .toml file. app.toml must still pass the
validation start applies and config.toml CometBFT's own. A key the file lacks is
not created here; migrate adds the keys this binary knows.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := configPath(cmd, args[0])
			key := strings.Split(args[1], ".")
			value, err := parser.ParseValue(args[2])
			if err != nil {
				// Not TOML on its own, so a string: 0.01anoah, 2s, localhost:9464.
				if value, err = parser.ParseValue(strconv.Quote(args[2])); err != nil {
					return fmt.Errorf("value %q: %w", args[2], err)
				}
			}
			doc, err := confix.LoadConfig(path)
			if err != nil {
				return err
			}
			plan := transform.Plan{{
				Desc: fmt.Sprintf("set %s = %s", args[1], value),
				T: transform.Func(func(_ context.Context, doc *tomledit.Document) error {
					found := doc.Find(key...)
					switch {
					case len(found) == 0:
						return fmt.Errorf("key %q not found", args[1])
					case len(found) > 1:
						return fmt.Errorf("key %q is ambiguous", args[1])
					case !found[0].IsMapping():
						return fmt.Errorf("%q is a table", args[1])
					}
					found[0].Value = value
					return nil
				}),
			}}
			return applyConfigPlan(cmd, doc, plan, path, configValidator(path), flags)
		},
	}
	flags.bind(cmd)
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate [config]",
		Short: "Validate a config file",
		Long: `Validate a config file, named without its extension: app, config, or
client, or given as a path to a .toml file. app.toml gets the validation start
applies, config.toml CometBFT's own, and client.toml confix's. Defaults to the
home directory's app.toml.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "app"
			if len(args) > 0 {
				name = args[0]
			}
			path := configPath(cmd, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := configValidator(path)(data); err != nil {
				return fmt.Errorf("%s is invalid: %w", path, err)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s is valid\n", path)
			return err
		},
	}
}

// appTOMLPath is the app.toml a command works on: the argument when given,
// else the home directory's.
func appTOMLPath(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		if filepath.Ext(args[0]) != ".toml" {
			return "", fmt.Errorf("%q is not a .toml file; there is no version argument, the target is the file this binary writes", args[0])
		}
		return args[0], nil
	}
	if home := client.GetClientContextFromCmd(cmd).HomeDir; home != "" {
		return filepath.Join(home, "config", confix.AppConfig), nil
	}
	return "", errors.New("no home directory: give the path to app.toml")
}

// configPath is the file set and validate work on: a .toml path as given,
// otherwise a name without its extension under the home directory's config,
// as confix resolves one.
func configPath(cmd *cobra.Command, name string) string {
	if filepath.Ext(name) == ".toml" {
		return name
	}
	if home := client.GetClientContextFromCmd(cmd).HomeDir; home != "" {
		return filepath.Join(home, "config", name+".toml")
	}
	return name
}

// defaultAppTOML is the app.toml arkd init writes, parsed: the migrate target
// and the diff baseline.
func defaultAppTOML() (*tomledit.Document, error) {
	tmpl, err := template.New("app.toml").Parse(appConfigTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, defaultAppConfig()); err != nil {
		return nil, err
	}
	return tomledit.Parse(&buf)
}

// appConfigPlan aligns tables and keys with the target while preserving retained values. Missing
// tables include target comments; missing keys append in target order. Unsupported keys and tables
// are removed.
func appConfigPlan(doc, target *tomledit.Document) transform.Plan {
	plan := missingKeySteps(nil, nil, doc.Global, target.Global)
	for _, want := range target.Sections {
		name := want.TableName()
		have := transform.FindTable(doc, name...)
		if have == nil {
			plan = append(plan, transform.Step{
				Desc: "add " + want.String(),
				T: transform.Func(func(_ context.Context, doc *tomledit.Document) error {
					doc.Sections = append(doc.Sections, want)
					return nil
				}),
			})
			continue
		}
		plan = missingKeySteps(plan, name, have.Section, want)
	}
	removed := map[string]bool{}
	doc.Scan(func(key parser.Key, e *tomledit.Entry) bool {
		switch {
		case e.IsSection():
			if transform.FindTable(target, key...) == nil {
				removed[key.String()] = true
				plan = append(plan, removeStep(key, e.Heading.String()))
			}
		case removed[e.Section.TableName().String()]:
			// Goes with its table.
		case target.First(key...) == nil:
			plan = append(plan, removeStep(key, key.String()))
		}
		return true
	})
	return plan
}

// missingKeySteps adds a step for each key want has and have lacks, in want's
// order.
func missingKeySteps(plan transform.Plan, table parser.Key, have, want *tomledit.Section) transform.Plan {
	for _, item := range want.Items {
		kv, ok := item.(*parser.KeyValue)
		if !ok || hasKey(have, kv.Name) {
			continue
		}
		plan = append(plan, transform.Step{
			Desc: fmt.Sprintf("add %s = %s", slices.Concat(table, kv.Name), kv.Value),
			T:    transform.EnsureKey(table, kv),
		})
	}
	return plan
}

func hasKey(s *tomledit.Section, name parser.Key) bool {
	for _, item := range s.Items {
		if kv, ok := item.(*parser.KeyValue); ok && kv.Name.Equals(name) {
			return true
		}
	}
	return false
}

func removeStep(key parser.Key, desc string) transform.Step {
	return transform.Step{Desc: "remove " + desc, T: transform.Remove(key)}
}

// keptValue is a key doc and target share with different values.
type keptValue struct {
	key, value, def string
}

// changedValues lists the keys whose value differs from target's; migrate
// leaves them as they are.
func changedValues(doc, target *tomledit.Document) []keptValue {
	var kept []keptValue
	doc.Scan(func(key parser.Key, e *tomledit.Entry) bool {
		if !e.IsMapping() {
			return true
		}
		def := target.First(key...)
		if def == nil || !def.IsMapping() {
			return true
		}
		if have, want := e.Value.String(), def.Value.String(); have != want {
			kept = append(kept, keptValue{key: key.String(), value: have, def: want})
		}
		return true
	})
	return kept
}

// applyConfigPlan applies plan to doc, the parsed file at path, validates the
// result with validate, and writes it back with the file's own mode; confix
// writes 0600 over the SDK's 0644. --stdout prints instead and
// --skip-validate writes unchecked.
func applyConfigPlan(
	cmd *cobra.Command,
	doc *tomledit.Document,
	plan transform.Plan,
	path string,
	validate func([]byte) error,
	flags configWriteFlags,
) error {
	ctx := cmd.Context()
	if flags.verbose {
		ctx = transform.WithLogWriter(ctx, cmd.ErrOrStderr())
	}
	if err := plan.Apply(ctx, doc); err != nil {
		return fmt.Errorf("updating %s: %w", path, err)
	}
	var buf bytes.Buffer
	if err := tomledit.Format(&buf, doc); err != nil {
		return fmt.Errorf("formatting %s: %w", path, err)
	}
	if !flags.skipValidate {
		if err := validate(buf.Bytes()); err != nil {
			return fmt.Errorf("updated %s is invalid: %w", path, err)
		}
	}
	if flags.stdout {
		_, err := cmd.OutOrStdout().Write(buf.Bytes())
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return fsutil.ReplaceFile(path, buf.Bytes(), mode)
}

// tomlViper reads data as TOML into a fresh viper: no flags and no
// environment, so a verdict over it is the file's own.
func tomlViper(data []byte) (*viper.Viper, error) {
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return v, nil
}

// validateAppTOML is readAppConfig over the file alone.
func validateAppTOML(data []byte) error {
	v, err := tomlViper(data)
	if err != nil {
		return err
	}
	_, err = readAppConfig(v)
	return err
}

// validateCometTOML is CometBFT's own check of config.toml, which confix
// declines to run; start's advice to set mempool.type through this command
// depends on it.
func validateCometTOML(data []byte) error {
	v, err := tomlViper(data)
	if err != nil {
		return err
	}
	cfg := cmtcfg.DefaultConfig()
	if err := v.Unmarshal(cfg); err != nil {
		return err
	}
	return cfg.ValidateBasic()
}

// configValidator is the check for a config file, classified by name suffix
// as confix classifies one: Ark's for app.toml, CometBFT's for config.toml,
// confix's for the rest.
func configValidator(path string) func([]byte) error {
	switch {
	case strings.HasSuffix(path, confix.AppConfig):
		return validateAppTOML
	case strings.HasSuffix(path, confix.CMTConfig):
		return validateCometTOML
	}
	return func(data []byte) error { return confix.CheckValid(path, data) }
}

package command

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"

	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/api"
	"github.com/cli/cli/pkg/cmdutil"
	"github.com/cli/cli/utils"
	"github.com/spf13/cobra"
)

func init() {
	RootCmd.AddCommand(gistCmd)
	gistCmd.AddCommand(gistCreateCmd)
	gistCreateCmd.Flags().StringP("desc", "d", "", "A description for this gist")
	gistCreateCmd.Flags().BoolP("public", "p", false, "List the gist publicly (default: private)")
}

var gistCmd = &cobra.Command{
	Use:   "gist",
	Short: "Create gists",
	Long:  `Work with GitHub gists.`,
}

var gistCreateCmd = &cobra.Command{
	Use:   `create [<filename>... | -]`,
	Short: "Create a new gist",
	Long: `Create a new GitHub gist with given contents.

Gists can be created from one or multiple files. Alternatively, pass "-" as
file name to read from standard input.

By default, gists are private; use '--public' to make publicly listed ones.`,
	Example: heredoc.Doc(`
	# publish file 'hello.py' as a public gist
	$ gh gist create --public hello.py
	
	# create a gist with a description
	$ gh gist create hello.py -d "my Hello-World program in Python"

	# create a gist containing several files
	$ gh gist create hello.py world.py cool.txt
	
	# read from standard input to create a gist
	$ gh gist create -
	
	# create a gist from output piped from another command
	$ cat cool.txt | gh gist create
	`),
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return nil
		}

		info, err := os.Stdin.Stat()
		if err != nil {
			return fmt.Errorf("failed to check STDIN: %w", err)
		}

		stdinIsTTY := (info.Mode() & os.ModeCharDevice) == os.ModeCharDevice
		if stdinIsTTY {
			return &cmdutil.FlagError{Err: errors.New("no filenames passed and nothing on STDIN")}
		}
		return nil
	},
	RunE: gistCreate,
}

type Opts struct {
	Description string
	Public      bool
}

func gistCreate(cmd *cobra.Command, args []string) error {
	ctx := contextForCommand(cmd)
	client, err := apiClientForContext(ctx)
	if err != nil {
		return err
	}

	// This performs a dummy query, checks what scopes we have, and then asks for a user to reauth
	// with expanded scopes. it introduces latency whenever this command is run: a trade-off to avoid
	// having every single user reauth as a result of this feature even if they never once use gists.
	//
	// In the future we'd rather have the ability to detect a "reauth needed" scenario and replay
	// failed requests but some short spikes indicated that that would be a fair bit of work.
	client, err = ensureScopes(ctx, client, "gist")
	if err != nil {
		return err
	}

	opts, err := processOpts(cmd)
	if err != nil {
		return fmt.Errorf("did not understand arguments: %w", err)
	}

	fileArgs := args
	if len(args) == 0 {
		fileArgs = []string{"-"}
	}

	files, err := processFiles(os.Stdin, fileArgs)
	if err != nil {
		return fmt.Errorf("failed to collect files for posting: %w", err)
	}

	errOut := colorableErr(cmd)
	fmt.Fprintf(errOut, "%s Creating gist...\n", utils.Gray("-"))

	gist, err := api.GistCreate(client, opts.Description, opts.Public, files)
	if err != nil {
		return fmt.Errorf("%s Failed to create gist: %w", utils.Red("X"), err)
	}

	fmt.Fprintf(errOut, "%s Created gist\n", utils.Green("✓"))

	fmt.Fprintln(cmd.OutOrStdout(), gist.HTMLURL)

	return nil
}

func processOpts(cmd *cobra.Command) (*Opts, error) {
	description, err := cmd.Flags().GetString("desc")
	if err != nil {
		return nil, err
	}

	public, err := cmd.Flags().GetBool("public")
	if err != nil {
		return nil, err
	}

	return &Opts{
		Description: description,
		Public:      public,
	}, err
}

func processFiles(stdin io.ReadCloser, filenames []string) (map[string]string, error) {
	fs := map[string]string{}

	if len(filenames) == 0 {
		return nil, errors.New("no files passed")
	}

	for i, f := range filenames {
		var filename string
		var content []byte
		var err error
		if f == "-" {
			filename = fmt.Sprintf("gistfile%d.txt", i)
			content, err = ioutil.ReadAll(stdin)
			if err != nil {
				return fs, fmt.Errorf("failed to read from stdin: %w", err)
			}
			stdin.Close()
		} else {
			content, err = ioutil.ReadFile(f)
			if err != nil {
				return fs, fmt.Errorf("failed to read file %s: %w", f, err)
			}
			filename = path.Base(f)
		}

		fs[filename] = string(content)
	}

	return fs, nil
}

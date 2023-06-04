package donut

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

type App struct {
	Config   *Config
	Store    *Store
	Logger   zerolog.Logger
	template *template.Template
	in       io.Reader
	out      io.Writer
	err      io.Writer
}

func New(opts ...AppOption) (*App, error) {
	app := &App{
		in:  os.Stdin,
		out: os.Stdout,
		err: os.Stderr,
	}
	for _, opt := range opts {
		if err := opt(app); err != nil {
			return nil, err
		}
	}

	if app.Config != nil {
		if err := app.createTemplates(); err != nil {
			return nil, err
		}
	}

	return app, nil
}

func (a *App) createTemplates() error {
	var tmpl = template.New("")
	var err error

	diffArgs := strings.Join(a.Config.Diff[1:], " ")
	tmpl, err = tmpl.New("diff").Parse(diffArgs)
	if err != nil {
		return err
	}
	mergeArgs := strings.Join(a.Config.Merge[1:], " ")
	tmpl, err = tmpl.New("merge").Parse(mergeArgs)
	if err != nil {
		return err
	}

	a.template = tmpl

	return nil
}

type diffTemplateData struct {
	Source      string
	Destination string
}

type mergeTemplateData struct {
	Source      string
	Destination string
	Base        string
}

func (a *App) Init(_ context.Context) error {
	// check config file exists in default path
	// if exists, no need to init
	_, err := NewConfig(WithDefault(), WithNameAndPath(AppName, DefaultConfigDirs()...))
	if err == nil {
		return errors.New("config file already exists. no need to init")
	} else if !errors.As(err, &viper.ConfigFileNotFoundError{}) {
		return fmt.Errorf("config file already exists, but error: %w", err)
	}

	path := DefaultConfigFile()
	if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
		return err
	}
	a.Logger.Info().Str("name", filepath.Dir(path)).Msg("Created")

	v, _ := NewConfig(WithDefault())
	if err := v.SafeWriteConfigAs(path); err != nil {
		return err
	}

	fmt.Fprintln(a.out, "Configuration file created in", path)
	return nil
}

func (a *App) List(_ context.Context) error {
	mapper, err := NewPathMapperBuilder(
		a.Config.Source, a.Config.Destination, WithExcludes(a.Config.Excludes...),
	).Build()
	if err != nil {
		return err
	}

	for _, relSourcePath := range mapper.RelSourcePaths() {
		fmt.Fprintln(a.out, relSourcePath)
	}
	return nil
}

func (a *App) Diff(ctx context.Context) error {
	mapper, err := NewPathMapperBuilder(
		a.Config.Source, a.Config.Destination, WithExcludes(a.Config.Excludes...),
	).Build()
	if err != nil {
		return err
	}

	diffCmdName := a.Config.Diff[0]
	diffCh := make(chan []byte, len(mapper.Mapping))
	eg, ectx := errgroup.WithContext(context.Background())
	eg.SetLimit(a.Config.Concurrency)
	for _, pm := range mapper.Mapping {
		pm := pm
		eg.Go(func() error {
			select {
			case <-ectx.Done():
				return ectx.Err()
			default:
				ss, err := fileSystem.GetSum(pm.Source)
				if err != nil {
					return err
				}
				ds, err := fileSystem.GetSum(pm.Destination)
				if err != nil {
					return err
				}
				if bytes.Equal(ss, ds) {
					return nil
				}

				argsBuilder := strings.Builder{}
				if err := a.template.ExecuteTemplate(&argsBuilder, "diff", diffTemplateData(pm)); err != nil {
					return err
				}
				args := strings.Split(argsBuilder.String(), " ")
				cmd := exec.CommandContext(ectx, diffCmdName, args...)
				out, _ := cmd.Output()
				a.Logger.Info().Str("command", diffCmdName).Strs("args", args).Msg("Executed")
				diffCh <- out
				return nil
			}
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}
	close(diffCh)
	var diff []byte
	for v := range diffCh {
		diff = append(diff, v...)
	}

	pagerCmdName := a.Config.Pager[0]
	pagerCmdArgs := a.Config.Pager[1:]
	cmd := exec.CommandContext(ctx, pagerCmdName, pagerCmdArgs...)
	cmd.Stdin = bytes.NewBuffer(diff)
	cmd.Stdout = a.out
	if err := cmd.Run(); err != nil {
		return err
	}
	a.Logger.Info().Str("command", pagerCmdName).Strs("args", pagerCmdArgs).Msg("Executed")
	return nil
}

func (a *App) Merge(ctx context.Context) error {
	mapper, err := NewPathMapperBuilder(
		a.Config.Source, a.Config.Destination, WithExcludes(a.Config.Excludes...),
	).Build()
	if err != nil {
		return err
	}

	mergeCmdName := a.Config.Merge[0]
	for _, pm := range mapper.Mapping {
		ss, err := fileSystem.GetSum(pm.Source)
		if err != nil {
			return err
		}
		ds, err := fileSystem.GetSum(pm.Destination)
		if err != nil {
			return err
		}
		if bytes.Equal(ss, ds) {
			continue
		}

		var be *FileEntry
		if err := a.Store.Get(FileEntryBucket, pm.Destination, &be); err != nil {
			return err
		}
		bc, err := be.GetContent()
		if err != nil {
			return err
		}
		f, err := os.CreateTemp("", fmt.Sprintf("%s.base.*", filepath.Base(pm.Destination)))
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if _, err := io.Copy(f, bytes.NewReader(bc)); err != nil {
			return err
		}

		data := mergeTemplateData{
			Source:      pm.Source,
			Destination: pm.Destination,
			Base:        f.Name(),
		}
		argsBuilder := strings.Builder{}
		if err := a.template.ExecuteTemplate(&argsBuilder, "merge", data); err != nil {
			return err
		}
		args := strings.Split(argsBuilder.String(), " ")
		cmd := exec.CommandContext(ctx, mergeCmdName, args...)
		cmd.Stdin = a.in
		cmd.Stdout = a.out
		if err := cmd.Run(); err != nil {
			return err
		}
		a.Logger.Info().Str("command", mergeCmdName).Strs("args", args).Msg("Executed")
	}

	return nil
}

func (a *App) Where(_ context.Context, dir string) error {
	switch dir {
	case "source":
		fmt.Fprint(a.out, a.Config.Source)
	case "destination":
		fmt.Fprint(a.out, a.Config.Destination)
	case "config":
		fmt.Fprint(a.out, filepath.Dir(GetConfig().ConfigFileUsed()))
	default:
	}
	return nil
}

func (a *App) ConfigEdit(_ context.Context) error {
	v := GetConfig()
	editorCmdName := a.Config.Editor[0]
	editorCmdArgs := a.Config.Editor[1:]
	args := append(editorCmdArgs, v.ConfigFileUsed())
	cmd := exec.Command(editorCmdName, args...)
	cmd.Stdin = a.in
	cmd.Stdout = a.out
	return cmd.Run()
}

func (a *App) Apply(ctx context.Context, overwrite bool) error {
	mapper, err := NewPathMapperBuilder(
		a.Config.Source, a.Config.Destination, WithExcludes(a.Config.Excludes...),
	).Build()
	if err != nil {
		return err
	}

	eg, ectx := errgroup.WithContext(ctx)
	eg.SetLimit(a.Config.Concurrency)
	for _, pm := range mapper.Mapping {
		pm := pm
		eg.Go(func() error {
			select {
			case <-ectx.Done():
				return ectx.Err()
			default:
				ss, err := fileSystem.GetSum(pm.Source)
				if err != nil {
					return err
				}
				ds, err := fileSystem.GetSum(pm.Destination)
				if err != nil {
					return err
				}
				if bytes.Equal(ss, ds) {
					return nil
				}

				var be *FileEntry
				if err := a.Store.Get(FileEntryBucket, pm.Destination, &be); err != nil {
					return err
				}
				bs, err := be.GetSum()
				if err != nil {
					return err
				}

				// skip if the following conditions are met
				// 1. exist in store
				// 2. checksum is equal to destination
				// 3. overwrite is false
				if bs != nil && !bytes.Equal(bs, ds) && !overwrite {
					fmt.Fprintf(a.out, "%s has been modified since the last apply. use --overwrite to overwrite\n", pm.Destination)
					return nil
				}

				// If the directory does not exist, create it
				// os.MkdirAll will return nil if directory already exists
				dir := filepath.Dir(pm.Destination)
				if err := os.MkdirAll(dir, os.ModePerm); err != nil {
					return err
				}
				a.Logger.Info().Str("name", dir).Msg("Created")
				if err := a.Overwrite(pm.Source, pm.Destination); err != nil {
					return err
				}

				de, err := fileSystem.Reload(pm.Destination)
				if err != nil {
					return err
				}
				if err := a.Store.Set(FileEntryBucket, pm.Destination, de); err != nil {
					return err
				}
				fmt.Fprintf(a.out, "Applied to %s from %s\n", pm.Destination, pm.Source)
				return nil
			}
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}

// Overwrite replaces the contents of dst with the contents of src.
func (a *App) Overwrite(src, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	se, err := fileSystem.Get(src)
	if err != nil {
		return err
	}
	sc, err := se.GetContent()
	if err != nil {
		return err
	}
	if _, err := f.Write(sc); err != nil {
		return err
	}

	a.Logger.Info().Str("name", dst).Msg("Updated")
	return nil
}

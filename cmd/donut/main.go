package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/gleamsoda/donut"
)

func main() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

var isDebug bool

func init() {
	pflag.BoolVarP(&isDebug, "debug", "d", false, "Enable debug log")

	cobra.OnInitialize(func() {
		if err := donut.InitStore(); err != nil {
			panic(err)
		}
		if err := donut.InitLogger(os.Stdout, isDebug); err != nil {
			panic(err)
		}
	})
	cobra.OnFinalize(func() {
		if err := donut.GetStore().Close(); err != nil {
			panic(err)
		}
	})
}

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "donut",
		Version:      donut.GetVersion(),
		Short:        "Tiny dotfiles management tool written in Go.",
		SilenceUsage: true,
	}

	cmd.PersistentFlags().StringP("file", "f", "", "Specify the path to the configuration file")

	cmd.AddCommand(
		NewInitCmd(),
		NewListCmd(),
		NewDiffCmd(),
		NewMergeCmd(),
		NewWhereCmd(),
		NewConfigCmd(),
		NewApplyCmd(),
	)

	return cmd
}

func NewInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, _ := donut.New(donut.WithLogger(donut.GetLogger()))
			return d.Init(cmd.Context())
		},
	}

	return cmd
}

func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Display a list of source files",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			return d.List(cmd.Context())
		},
	}

	return cmd
}

func NewDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Display a list of differences between source and destination files",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithStore(donut.GetStore()), donut.WithLogger(donut.GetLogger()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			return d.Diff(cmd.Context())
		},
	}

	return cmd
}

func NewMergeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge the source file into the destination file",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithStore(donut.GetStore()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			return d.Merge(cmd.Context())
		},
		Args: cobra.NoArgs,
	}

	return cmd
}

func NewWhereCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "where",
		Short: "Display the location of the source or destination directory",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			return d.Where(cmd.Context(), args[0])
		},
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"source", "destination", "config"},
	}

	return cmd
}

func NewConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Edit the configuration file",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			return d.ConfigEdit(cmd.Context())
		},
	}

	return cmd
}

func NewApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the content of the source file to the destination file",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			f, _ := cmd.Flags().GetString("file")
			return donut.InitConfig(f)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := donut.New(donut.WithConfig(donut.GetConfig()), donut.WithStore(donut.GetStore()), donut.WithLogger(donut.GetLogger()))
			if err != nil {
				return err
			}
			o, _ := cmd.Flags().GetBool("overwrite")
			return d.Apply(cmd.Context(), o)
		},
	}

	cmd.Flags().BoolP("overwrite", "o", false, "Overwrite the destination file with the source file")

	return cmd
}

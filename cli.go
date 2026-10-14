package main

import (
	"context"
	"errors"
	"github.com/spf13/cobra"
	"io"
	"strings"
)

var errArguments = errors.New("Ошибка в аргументах")

type settings struct{ bucket, accessKey, secretKey string }
type archiveFactory func(settings) *archive

func validName(s string) bool { return s != "" && !strings.ContainsAny(s, "/\x00") }

func execute(ctx context.Context, args []string, out io.Writer, getenv func(string) string, factory archiveFactory) error {
	config := settings{getenv("BUCKET"), getenv("AWS_ACCESS_KEY_ID"), getenv("AWS_SECRET_ACCESS_KEY")}
	if config.bucket == "" || config.accessKey == "" || config.secretKey == "" {
		return errors.New("Не заданы переменные окружения")
	}
	root := &cobra.Command{Use: "cloudphoto", Short: "Фотоархив в Yandex Object Storage", SilenceUsage: true, SilenceErrors: true,
		RunE: func(*cobra.Command, []string) error { return errArguments }}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetOut(out)
	root.SetErr(io.Discard)
	root.SetFlagErrorFunc(func(*cobra.Command, error) error { return errArguments })
	for _, name := range []string{"list", "upload", "download", "delete", "mksite"} {
		var album, photo, path string
		cmd := &cobra.Command{Use: name, Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errArguments
			}
			if cmd.Name() != "mksite" && (cmd.Name() != "list" || cmd.Flags().Changed("album")) && !validName(album) {
				return errArguments
			}
			if cmd.Flags().Changed("photo") && !validName(photo) {
				return errArguments
			}
			if cmd.Flags().Changed("path") && path == "" {
				return errArguments
			}
			return nil
		}, RunE: func(cmd *cobra.Command, _ []string) error {
			a := factory(config)
			switch cmd.Name() {
			case "list":
				return a.list(cmd.Context(), album, out)
			case "upload":
				return a.upload(cmd.Context(), album, path)
			case "download":
				return a.download(cmd.Context(), album, path)
			case "delete":
				return a.delete(cmd.Context(), album, photo)
			default:
				return a.mksite(cmd.Context())
			}
		}}
		if name != "mksite" {
			cmd.Flags().StringVar(&album, "album", "", "Название альбома")
		}
		if name == "upload" || name == "download" {
			cmd.Flags().StringVar(&path, "path", ".", "Каталог фотографий")
		}
		if name == "delete" {
			cmd.Flags().StringVar(&photo, "photo", "", "Имя файла фотографии")
		}
		root.AddCommand(cmd)
	}
	if _, _, err := root.Find(args); err != nil {
		return errArguments
	}
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

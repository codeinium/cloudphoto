package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestInvalidArguments(t *testing.T) {
	cases := [][]string{
		{}, {"unknown"}, {"list", "extra"}, {"list", "--bad"}, {"list", "--album="},
		{"upload"}, {"upload", "--album", "a/b"}, {"download", "--album", "x", "--path="},
		{"delete", "--album", "x", "--photo="}, {"mksite", "extra"}, {"mksite", "--album", "x"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := execute(context.Background(), args, &bytes.Buffer{}, func(string) string { return "test" }, func(settings) *archive { t.Fatal("storage called for invalid arguments"); return nil })
			if err != errArguments {
				t.Fatalf("got %v, want %v", err, errArguments)
			}
		})
	}
}

func TestMissingEnvironment(t *testing.T) {
	for _, missing := range []string{"BUCKET", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"} {
		err := execute(context.Background(), []string{"list"}, &bytes.Buffer{}, func(key string) string {
			if key == missing {
				return ""
			}
			return "test"
		}, nil)
		if err == nil || err.Error() != "Не заданы переменные окружения" {
			t.Fatal(err)
		}
	}
}

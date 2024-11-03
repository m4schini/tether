package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/m4schini/tether"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	AddSource: false,
	Level:     slog.LevelDebug,
}))

func main() {
	tether.Logger = logger
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL, os.Kill, os.Interrupt)
	defer cancel()
	var outputDir = flag.String("output", "./tether", "photo download directory")
	flag.Parse()

	err := os.MkdirAll(*outputDir, 0755)
	if err != nil {
		logger.Error("failed to create output directory", slog.Any("err", err))
		os.Exit(1)
	}

	images := tether.Start(ctx)
	var i int
	for image := range images {
		imageFileName := filepath.Join(*outputDir, fmt.Sprintf("%v.jpg", i))
		f, err := os.OpenFile(imageFileName, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logger.Error("failed to create jpeg file", slog.Any("file", imageFileName), slog.Any("err", err))
			continue
		}

		_, err = io.Copy(f, bytes.NewReader(image))
		if err != nil {
			logger.Debug("failed to write image to file", slog.Any("err", err))
			continue
		}
		f.Close()
		logger.Info("wrote image to file", slog.Any("file", imageFileName))

		i++
	}
}

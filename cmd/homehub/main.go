package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/TheOriUHD/homehub/internal/app"
	"github.com/TheOriUHD/homehub/internal/config"
	"github.com/TheOriUHD/homehub/internal/mctex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "minecraft-textures":
			if err := installTextures(ctx, os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, "homehub:", err)
				os.Exit(1)
			}
			return
		case "help", "-h", "--help":
			usage()
			return
		default:
			fmt.Fprintf(os.Stderr, "homehub: unknown command %q\n\n", os.Args[1])
			usage()
			os.Exit(2)
		}
	}

	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "homehub:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage:
  homehub                          run the server
  homehub minecraft-textures       download Minecraft item icons from Mojang for the inventory view
`)
}

// installTextures downloads the official client jar from Mojang and keeps
// the textures the Minecraft inventory view draws with in the data dir.
func installTextures(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("minecraft-textures", flag.ContinueOnError)
	version := fs.String("version", "", "Minecraft version to take textures from (default: latest release)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	res, err := mctex.Install(ctx, cfg.DataDir, *version, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
	if err != nil {
		return err
	}
	fmt.Printf("installed %d textures from Minecraft %s into %s\n", res.Files, res.Version, mctex.Dir(cfg.DataDir))
	return nil
}

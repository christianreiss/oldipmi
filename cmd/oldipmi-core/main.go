package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net"
	"oldipmi"
	"oldipmi/internal/backend"
	"oldipmi/internal/protocol"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() { os.Exit(run()) }
func run() int {
	probe := flag.Bool("probe", false, "read-only connection/video probe; JSON credentials on stdin")
	capture := flag.String("capture", "", "save first probe frame as PNG to this explicit path")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(oldipmi.Version)
		return 0
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *probe {
		var in struct {
			Profile  protocol.Profile `json:"profile"`
			Password string           `json:"password"`
		}
		if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
			fmt.Fprintln(os.Stderr, "invalid probe input")
			return 2
		}
		ctx, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		s, e := backend.Open(ctx, in.Profile, in.Password)
		in.Password = ""
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 1
		}
		defer s.Close()
		count := 0
		captureFailed := false
		e = s.Run(ctx, func(f protocol.Frame) {
			count++
			fmt.Printf("frame %d: %dx%d (%d bytes)\n", count, f.Width, f.Height, len(f.Pixels))
			if count == 1 && *capture != "" {
				file, err := os.OpenFile(*capture, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					err = png.Encode(file, &image.NRGBA{Pix: f.Pixels, Stride: f.Width * 4, Rect: image.Rect(0, 0, f.Width, f.Height)})
					file.Close()
				}
				if err != nil {
					captureFailed = true
					fmt.Fprintln(os.Stderr, "capture failed:", err)
				}
			}
			if count == 3 {
				stop()
			}
		})
		if captureFailed {
			return 1
		}
		if count == 0 || (e != nil && ctx.Err() == nil) {
			fmt.Fprintln(os.Stderr, e)
			return 1
		}
		return 0
	}
	fd := func(n uintptr) (net.Conn, error) {
		f := os.NewFile(n, "oldipmi-ipc")
		if f == nil {
			return nil, fmt.Errorf("missing desktop IPC descriptor")
		}
		defer f.Close()
		return net.FileConn(f)
	}
	ctl, e := fd(3)
	if e != nil {
		fmt.Fprintln(os.Stderr, "Launch OldIPMI through its desktop application, or use --probe.")
		return 2
	}
	defer ctl.Close()
	vid, e := fd(4)
	if e != nil {
		return 2
	}
	defer vid.Close()
	if e = backend.Serve(ctx, ctl, vid); e != nil && ctx.Err() == nil {
		return 1
	}
	return 0
}

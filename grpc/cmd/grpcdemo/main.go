// grpcdemo: a small gooey app that is also a gRPC control-plane server —
// and its own client.
//
// Run it and any gooey.control.v1 client (the generated Go/Python/TS
// clients, grpcurl) can attach to the running terminal app:
//
//	cd grpc && go run ./cmd/grpcdemo -grpc 127.0.0.1:7788
//
// Then, from another terminal, drive it with the generated Go client:
//
//	cd grpc && go run ./cmd/grpcdemo -drive 127.0.0.1:7788
//
// The driver is the demo's other half: it snapshots the tree, sets a
// property, invokes a command, and holds a session open printing every
// FrameDelta the app pushes — SnapshotTree/SetProperty/InvokeCommand/
// Attach over the real wire, against the app you are looking at.
//
// It lives inside grpc/ because grpc/ is a nested module: grpc-go is
// quarantined there, and a binary that imports it builds from there too.
// The UI is ordinary markup with named elements and a viewmodel of
// typed property handles; the control surface falls out of that with no
// extra declaration beyond the single grpc.Serve call.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/WonderForgeLabs/gooey/control"
	gooeygrpc "github.com/WonderForgeLabs/gooey/grpc"
	controlv1 "github.com/WonderForgeLabs/gooey/grpc/gen/gooey/control/v1"
)

func main() {
	addr := flag.String("grpc", "127.0.0.1:7788", "loopback address for the gRPC server; empty disables it")
	// The GUEST endpoint. Same app, second listener, and the only
	// difference is that this one carries a Grant — so a client dialing
	// it reaches <Border Name="Guest"> and the "Guest" value namespace,
	// and is refused everything else BY THE SERVER.
	//
	// Registration is the grant: the host names the island here, in code
	// it owns, exactly as it registers Components and Handlers. Nothing a
	// guest sends can widen it, because there is no request field to
	// widen. The address it was handed IS its capability.
	guest := flag.String("guest", "127.0.0.1:7789", "loopback address for the SCOPED guest endpoint; empty disables it")
	drive := flag.String("drive", "", "drive a running grpcdemo at this address instead of being one")
	flag.Parse()

	if *drive != "" {
		if err := driver(*drive); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	serve(*addr, *guest)
}

// ---- the app half ----

func serve(addr, guestAddr string) {
	count := prop.NewSource(0)
	note := prop.NewSource("")
	help := prop.NewSource("")
	guestBody := prop.NewSource("this subtree belongs to whoever holds the guest endpoint")
	readout := prop.NewComputed(func() string {
		return fmt.Sprintf("count=%d   note=%q", count.Get(), note.Get())
	})

	var app *gooey.App
	ctx := &markup.Context{
		Values: map[string]any{
			"Readout":   readout,
			"Note":      note,
			"Help":      help,
			"Increment": gooey.Command(func() { count.Set(count.Get() + 1) }),
			"Reset":     gooey.Command(func() { count.Set(0) }),
			"Quit":      gooey.Command(func() { app.Quit() }),
			"Guest":     map[string]any{"Body": guestBody},
		},
		Styles: map[string]render.Style{
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"dim":    {Fg: render.RGB(140, 140, 150)},
		},
	}

	dir := "cmd/grpcdemo"
	if _, err := os.Stat(filepath.Join(dir, "grpcdemo.gooey")); err != nil {
		exe, _ := os.Executable()
		dir = filepath.Dir(exe)
	}
	app = gooey.NewApp(markup.Page(os.DirFS(dir), "grpcdemo.gooey", ctx))

	if addr != "" {
		// Serving is one call and it is opt-in, which is the whole
		// security posture of v1: a control-plane client can do anything
		// the keyboard can, so nothing listens unless the app said so —
		// and only on loopback.
		srv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
			Addr:    addr,
			Context: ctx,
			Name:    "gooey-grpcdemo",
			Version: "1",
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer srv.Close()
		help.Set("gRPC control plane on " + srv.Addr() + "  (gooey.control.v1)\n" +
			"drive it:  go run ./cmd/grpcdemo -drive " + srv.Addr())
	} else {
		help.Set("started with -grpc \"\": no server is listening")
	}

	if guestAddr != "" {
		gsrv, err := gooeygrpc.Serve(app, gooeygrpc.Options{
			Addr:    guestAddr,
			Context: ctx,
			Name:    "gooey-grpcdemo (guest)",
			Version: "1",
			// The whole grant, in one expression. Everything outside
			// <Border Name="Guest"> and the "Guest" namespace is refused
			// by the server, whatever the client believes it owns.
			Grant: control.Island("Guest", "Guest"),
		})
		if err != nil {
			gooey.Exit(err)
		}
		defer gsrv.Close()
		help.Set(help.Get() + "\nscoped guest endpoint on " + gsrv.Addr() +
			"  (island <Guest>) — try:  wysiwyg -attach " + gsrv.Addr() + " -island Guest")
	}

	if err := app.Run(context.Background()); err != nil {
		gooey.Exit(err)
	}
}

// ---- the client half ----

// driver is a generated-client session against a running grpcdemo: the
// unary reads and writes, then an Attach stream that acts and listens.
func driver(addr string) error {
	conn, err := grpcgo.NewClient(addr, grpcgo.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctl := controlv1.NewControlServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// The tree, by name.
	tree, err := ctl.SnapshotTree(ctx, &controlv1.SnapshotTreeRequest{})
	if err != nil {
		return fmt.Errorf("SnapshotTree: %w", err)
	}
	fmt.Println("tree:")
	printTree(tree.Root, "  ")

	// A typed write, settled before the call returns.
	if _, err := ctl.SetProperty(ctx, &controlv1.SetPropertyRequest{
		Name:  "Note",
		Value: &controlv1.TypedValue{Kind: &controlv1.TypedValue_StringValue{StringValue: "written by the generated client"}},
	}); err != nil {
		return fmt.Errorf("SetProperty: %w", err)
	}
	fmt.Println("\nSetProperty Note: ok (the app repainted before this line printed)")

	// A session: subscribe, act, and hear the frames the acts cause.
	stream, err := controlv1.NewSessionServiceClient(conn).Attach(ctx)
	if err != nil {
		return fmt.Errorf("Attach: %w", err)
	}
	if err := stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Subscribe{
		Subscribe: &controlv1.Subscription{Properties: true, Frames: true, Lifecycle: true},
	}}); err != nil {
		return err
	}
	for i := uint64(1); i <= 3; i++ {
		if err := stream.Send(&controlv1.AttachRequest{Msg: &controlv1.AttachRequest_Act{Act: &controlv1.Act{
			Id:  i,
			Act: &controlv1.Act_InvokeCommand{InvokeCommand: &controlv1.InvokeCommandRequest{Name: "Increment"}},
		}}}); err != nil {
			return err
		}
	}

	// Three acts, three results; every frame push in between, printed.
	fmt.Println("\nsession:")
	for done := 0; done < 3; {
		m, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("session recv: %w", err)
		}
		switch v := m.Msg.(type) {
		case *controlv1.AttachResponse_Welcome:
			fmt.Printf("  welcome: %s v%s, %dx%d, frame %d\n",
				v.Welcome.AppName, v.Welcome.AppVersion, v.Welcome.Columns, v.Welcome.Rows, v.Welcome.Frame)
		case *controlv1.AttachResponse_Frame:
			f := v.Frame
			fmt.Printf("  frame %d: repainted=%d, %d change(s)", f.Frame, f.Repainted, len(f.Changes))
			for _, c := range f.Changes {
				fmt.Printf("  %s=%s", c.Name, c.Value.GetStringValue())
			}
			fmt.Println()
		case *controlv1.AttachResponse_Result:
			fmt.Printf("  act %d: code=%d\n", v.Result.Id, v.Result.Code)
			done++
		case *controlv1.AttachResponse_Lifecycle:
			fmt.Printf("  lifecycle: %v\n", v.Lifecycle)
		}
	}
	fmt.Println("\nlook at the app: count went up three times, one FrameDelta per frame.")
	return nil
}

func printTree(n *controlv1.TreeNode, indent string) {
	if n == nil {
		return
	}
	line := indent + n.Type
	if n.Name != "" {
		line += "  Name=" + n.Name
	}
	if b := n.Bounds; b != nil {
		line += fmt.Sprintf("  [%d,%d %dx%d]", b.X, b.Y, b.Width, b.Height)
	}
	fmt.Println(line)
	for _, c := range n.Children {
		printTree(c, indent+"  ")
	}
}

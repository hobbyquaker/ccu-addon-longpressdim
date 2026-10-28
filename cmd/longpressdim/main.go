// longpressdim is the ccu-addon-longpressdim daemon: it links virtual keys of an interface to
// its dimmers and blinds, relays the long presses of keys on other interfaces to them, and
// serves its web app and API on the loopback, behind lighttpd's proxy of /addons/longpressdim/.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit"
	"github.com/hobbyquaker/ccu-addon-kit/go/addonkit/ccuauth"
	"github.com/hobbyquaker/ccu-addon-kit/go/ccusource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
	"github.com/hobbyquaker/ccu-addon-longpressdim/docs"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/httpapi"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/service"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/store"
	"github.com/hobbyquaker/ccu-addon-longpressdim/internal/system"
)

// version is set by the build (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	log.SetFlags(0) // the journal adds the time
	addonDir := flag.String("addon-dir", "/usr/local/addons/longpressdim", "the addon's directory (etc/, var/, app/)")
	id := flag.String("id", "longpressdim", "the addon id (token file, base path)")
	port := flag.Int("port", 8293, "the loopback port lighttpd proxies to")
	base := flag.String("system", "http://127.0.0.1", "openccu-lite's URL")
	basePath := flag.String("base-path", "", "the path the app is served at (/addons/<id> by default)")
	www := flag.String("www", "", "the built web app (<addon-dir>/app by default; www/ holds the settings page)")
	tokenFile := flag.String("token-file", "", "a token file for the system's API (the addon's own token by default)")
	noSession := flag.Bool("no-session-check", false, "serve without the session check (development on a system that is not openccu-lite)")
	platform := flag.String("platform", "", "openccu-lite, openccu or ccu3; detected from the system by default")
	callbackPort := flag.Int("callback-port", 8294, "classic CCU: the loopback port the interface processes send events to")
	lang := flag.String("lang", "de", "the language of the room names in the API")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *basePath == "" {
		*basePath = "/addons/" + *id
	}
	if *www == "" {
		*www = filepath.Join(*addonDir, "app")
	}
	kind := addonkit.Kind(*platform)
	if kind == "" {
		kind = addonkit.Platform()
	}
	if err := run(*addonDir, *id, *port, *base, *basePath, *www, *tokenFile, *lang, *noSession, kind, *callbackPort); err != nil {
		log.Fatalf("longpressdim: %v", err)
	}
}

func run(addonDir, id string, port int, base, basePath, www, tokenFile, lang string, noSession bool, kind addonkit.Kind, callbackPort int) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(addonDir)
	if err != nil {
		return err
	}
	settings := store.OpenSettings(addonDir)

	// the system: openccu-lite's APIs, or on a classic CCU the interface processes and ReGa
	var sys system.System
	wrap := func(h http.Handler) http.Handler { return h }
	if kind == addonkit.OpenccuLite {
		var token addonkit.TokenSource
		if tokenFile != "" {
			token = &addonkit.FileToken{Path: tokenFile}
		} else {
			token = addonkit.AddonToken(id)
		}
		lite := litesource.New(base, token)
		lite.Log = func(format string, args ...any) { log.Printf("longpressdim: "+format, args...) }
		// wait for the system: at boot its web server answers 503 "starting" until occulited is
		// up, and the token may not exist yet. On openccu-lite (the platform rule, not the flag)
		// that is never a reason to give up - the wait goes on, with a log line a minute; only
		// elsewhere is "not openccu-lite" the end.
		if _, err := lite.Await(ctx, addonkit.IsOpenccuLite()); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("%s is not openccu-lite", base)
		}
		sys = system.NewLite(lite)
		if !noSession {
			c := addonkit.NewChecker(addonkit.SessionOptions{Base: base, Need: addonkit.LevelUser, OnRefused: func(r *http.Request, reason string) {
				log.Printf("longpressdim: refused %s %s: %s", r.Method, r.URL.Path, reason)
			}})
			wrap = c.Handler
		}
	} else {
		ccu := ccusource.New(ccusource.Options{ID: id, CallbackPort: callbackPort, SweepEvery: -1, Log: log.Printf})
		for {
			if err := ccu.Probe(ctx); err == nil {
				break
			}
			log.Printf("longpressdim: waiting for an interface process of the CCU")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(10 * time.Second):
			}
		}
		sys = system.NewCCU(ccu, addonDir)
		if !noSession {
			c := ccuauth.New(id, ccuauth.Options{Need: addonkit.LevelUser, OnRefused: func(r *http.Request, reason string) {
				log.Printf("longpressdim: refused %s %s: %s", r.Method, r.URL.Path, reason)
			}})
			wrap = c.Handler
		}
	}
	log.Printf("longpressdim: platform %s", kind)

	svc := service.New(service.Options{System: sys, Store: st, Settings: settings, Log: log.Printf, Lang: lang})
	go svc.Run(ctx)

	h := httpapi.New(httpapi.Options{BasePath: basePath, WWW: www, Service: svc, Wrap: wrap, OpenAPI: docs.OpenAPI, Version: version, Platform: string(kind), Started: time.Now()})
	l, err := addonkit.Listen(port, "")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("longpressdim %s: serving %s on %s, %d bindings", version, basePath, l.Addr(), len(st.List()))
	if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
		return err
	}
	log.Printf("longpressdim: stopping")
	os.Stdout.Sync()
	return nil
}

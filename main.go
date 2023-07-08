package main

import (
	"embed"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/fsnotify.v1"

	"github.com/omeid/livereload"
	"github.com/russross/blackfriday/v2"
)

const name = "mkup"

const version = "0.0.3"

var revision = "HEAD"

const (
	template = `
<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<title>%s</title>
<link rel="stylesheet" href="/_assets/sanitize.css" media="all">

<!-- custom light theme -->
<link rel="stylesheet" href="/_assets/github-light.css" media="all">
<link rel="stylesheet" href="/_assets/github-markdown-light.css" media="all">
<link rel="stylesheet" href="/_assets/style-light.css" media="all">

<!-- original dark theme
<link rel="stylesheet" href="/_assets/style.css" media="all">
<link rel="stylesheet" href="/_assets/github-dark.css" media="all">
-->

<script src="/_assets/highlight.min.js"></script>
<script>hljs.highlightAll();</script>
<script>document.write('<script src="http://'
    + (location.host || 'localhost').split(':')[0]
    + ':35729/livereload.js?snipver=1"></'
    + 'script>')</script>
</head>
<body>
<div class="markdown-body">%s</div>
</body>
</html>
`
	extensions = blackfriday.NoIntraEmphasis |
		blackfriday.Tables |
		blackfriday.FencedCode |
		blackfriday.Autolink |
		blackfriday.Strikethrough |
		blackfriday.SpaceHeadings
)

var (
	addr = flag.String("http", ":8000", "HTTP service address (e.g., ':8000')")
)

//go:embed _assets
var local embed.FS

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	var argpath string
	var cwd string
	var urlpath string
	if len(os.Args) == 2 {
		argpath = os.Args[1]
	} else if len(os.Args) == 1 {
		argpath, _ = os.Getwd()
	}
	stat, err := os.Stat(argpath)
	if err != nil {
		panic(err)
	}
	if stat.IsDir() {
		cwd = argpath
		urlpath = ""
	} else {
		cwd = filepath.Dir(argpath)
		urlpath = filepath.Base(argpath)
	}
	fmt.Printf("watch %s\n", cwd)

	lrs := livereload.New("mkup")
	defer lrs.Close()

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/livereload.js", func(w http.ResponseWriter, r *http.Request) {
			b, err := local.ReadFile("_assets/livereload.js")
			if err != nil {
				http.Error(w, "404 page not found", 404)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			w.Write(b)
			return
		})
		mux.Handle("/", lrs)
		log.Fatal(http.ListenAndServe(":35729", mux))
	}()

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	go func() {
		fsw.Add(cwd)
		err = filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
			if info == nil {
				return err
			}
			if !info.IsDir() {
				return nil
			}
			fsw.Add(path)
			return nil
		})

		for {
			select {
			case event := <-fsw.Events:
				if path, err := filepath.Rel(cwd, event.Name); err == nil {
					path = "/" + filepath.ToSlash(path)
					log.Println("reload", path)
					lrs.Reload(path, true)
				}
			case err := <-fsw.Errors:
				if err != nil {
					log.Println(err)
				}
			}
		}
	}()

	fs := http.FileServer(http.Dir(cwd))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Path
		if strings.HasPrefix(name, "/_assets/") {
			b, err := local.ReadFile(name[1:])
			if err != nil {
				http.Error(w, "404 page not found", 404)
				return
			}

			w.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(name)))
			w.Write(b)
			return
		}
		ext := filepath.Ext(name)
		if ext != ".md" && ext != ".mkd" && ext != ".markdown" {
			fs.ServeHTTP(w, r)
			return
		}
		b, err := ioutil.ReadFile(filepath.Join(cwd, name))
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "404 page not found", 404)
				return
			}
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{})
		b = blackfriday.Run(
			b,
			blackfriday.WithRenderer(renderer),
			blackfriday.WithExtensions(extensions),
		)
		w.Write([]byte(fmt.Sprintf(template, name, string(b))))
	})

	server := &http.Server{
		Addr: *addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s %s", r.RemoteAddr, r.Method, r.URL.RequestURI())
			http.DefaultServeMux.ServeHTTP(w, r)
		}),
	}

	fmt.Fprintln(os.Stderr, "Listening at "+*addr)

	if err := openurl("http://localhost:8000/" + urlpath); err != nil {
		panic(err)
	}

	log.Fatal(server.ListenAndServe())
}

func openurl(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	fmt.Println(args)
	return exec.Command(cmd, args...).Start()
}

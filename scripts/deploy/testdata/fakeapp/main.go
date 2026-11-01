// Test double for the admin binary. Only used by the isolated Linux deploy test; never shipped.
// Behaviour is fixed at build time: -X main.version=<sha> -X main.mode=<ok|crash|wrongstatus>.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
)

var (
	version = "unset"
	mode    = "ok"
)

func main() {
	port := flag.Int("port", 3000, "")
	logDir := flag.String("log-dir", "./logs", "")
	showVersion := flag.Bool("version", false, "")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if mode == "crash" {
		fmt.Fprintln(os.Stderr, "simulated migration failure")
		os.Exit(1)
	}
	_ = os.MkdirAll(*logDir, 0o700)
	reported := version
	if mode == "wrongstatus" {
		reported = "not-the-deployed-version"
	}
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"success":true,"data":{"version":%q,"setup":true,"register_enabled":false}}`, reported)
	})
	http.HandleFunc("/assets/app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		for range 20 {
			fmt.Fprint(w, "console.log('fake application bundle');\n")
		}
	})
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><script src="/assets/app.js"></script></body></html>`)
	})
	if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *port), nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

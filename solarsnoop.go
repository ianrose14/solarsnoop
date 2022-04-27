package main

import (
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

// go:embed templates/index.templates
var rootContent string

var (
	rootTemplate = template.Must(template.New("root").Parse(rootContent))
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	flag.Parse()

	log.Printf("listening on port :%d", *port)

	// http.HandleFunc("/favicon"
	http.HandleFunc("/", rootHandler)

	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	log.Fatalf("ListenAndServe: %v", err)
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	defer func() {
		log.Printf("%s [%s] \"%s %s %s\" %d %d %s", r.RemoteAddr, start.Format(time.RFC3339), r.Method, r.RequestURI, r.Proto, status, size, time.Since(start))
	}()

	var args struct {
		Uid string
		Cid string
	}

	if c, err := r.Cookie("enl_uid"); err != nil {
		if err != http.ErrNoCookie {
			log.Printf("failed to read \"enl_uid\" cookie: %s", err)
		}
	} else {
		args.Uid = c.Value
	}

	if c, err := r.Cookie("enl_cid"); err != nil {
		log.Printf("failed to read \"enl_cid\" cookie: %s", err)
	} else {
		args.Cid = c.Value
	}

	err := rootTemplate.Execute(w, &args)
	if err != nil {
		log.Printf("failed to execute template: %s", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

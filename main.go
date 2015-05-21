package main

import (
	"bytes"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

func main() {
	var (
		startUrl    = flag.String("url", "", "initial url to start at")
		rootFolder  = flag.String("root", "", "folder to save")
		waitBetween = flag.Duration("wait", time.Millisecond*20, "duration to wait between requests")
	)
	flag.Parse()

	if *startUrl == "" || *rootFolder == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}
	s := &Spider{
		FolderRoot: *rootFolder,
		viewed:     make(map[string]struct{}, 100),
		queue:      make(chan string, 40000),
	}

	s.EnqueueUrl(*startUrl)
	s.Run(*waitBetween)
}

type Spider struct {
	FolderRoot string
	Host       string

	viewed map[string]struct{}
	queue  chan string
}

func (s *Spider) Run(wait time.Duration) {
	if len(s.queue) == 0 {
		return
	}
	var lastHit time.Time
	for {
		was := time.Now().Sub(lastHit)
		if was < wait {
			time.Sleep(wait - was)
		}
		s.getUrl()
		lastHit = time.Now()
		if len(s.queue) == 0 {
			return
		}
	}
}

func (s *Spider) EnqueueUrl(urlString string) {
	if _, seen := s.viewed[urlString]; seen {
		return
	}
	s.viewed[urlString] = struct{}{}

	s.queue <- urlString
}

func (s *Spider) getUrl() {
	urlString := <-s.queue
	res, err := http.Get(urlString)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	url := res.Request.URL
	if s.Host == "" {
		s.Host = url.Host
	}
	if s.Host != url.Host {
		return
	}

	defer res.Body.Close()
	if res.StatusCode != 200 {
		log.Printf("%d: %s\n", res.StatusCode, urlString)
		return
	}
	ct := res.Header.Get("Content-Type")
	ct = strings.SplitN(ct, ";", 2)[0]
	body := &bytes.Buffer{}

	_, err = io.Copy(body, res.Body)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	file := filepath.Join(s.FolderRoot, url.Path)
	err = os.MkdirAll(filepath.Dir(file), 0700)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	err = ioutil.WriteFile(file, body.Bytes(), 0600)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	log.Printf("%s: %s\n", ct, url.Path)
	err = s.Parse(ct, url, body)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
}

func (s *Spider) Parse(contentType string, location *url.URL, body *bytes.Buffer) error {
	switch contentType {
	case "text/html":
		return s.parseHtml(body, location)
	case "text/css":
		return s.parseCss(body, location)
	}
	return nil
}

var (
	qLink   = cascadia.MustCompile("a[href]")
	qImg    = cascadia.MustCompile("img[src]")
	qStyle  = cascadia.MustCompile(`link[href][rel="stylesheet"]`)
	qScript = cascadia.MustCompile(`script[src]`)
)

func (s *Spider) parseHtml(body *bytes.Buffer, location *url.URL) error {
	page, err := html.Parse(body)
	if err != nil {
		return err
	}
	var (
		links   = qLink.MatchAll(page)
		imgs    = qImg.MatchAll(page)
		styles  = qStyle.MatchAll(page)
		scripts = qScript.MatchAll(page)
	)
	attr := func(node *html.Node, name string) string {
		for _, item := range node.Attr {
			if item.Key == name {
				return item.Val
			}
		}
		return ""
	}
	getNext := func(next string) {
		nextUrl, err := url.Parse(next)
		if err != nil {
			return
		}
		if nextUrl.Scheme == "" {
			nextUrl.Scheme = location.Scheme
		}
		if nextUrl.Host == "" {
			nextUrl.Host = location.Host
		}
		if path.IsAbs(nextUrl.Path) == false {
			nextUrl.Path = path.Join(path.Dir(location.Path), nextUrl.Path)
		}
		nextUrl.Fragment = ""
		s.EnqueueUrl(nextUrl.String())
	}
	for _, item := range links {
		v := attr(item, "href")
		if v == "" {
			continue
		}
		getNext(v)
	}
	for _, item := range imgs {
		v := attr(item, "src")
		if v == "" {
			continue
		}
		getNext(v)
	}
	for _, item := range styles {
		v := attr(item, "href")
		if v == "" {
			continue
		}
		getNext(v)
	}
	for _, item := range scripts {
		v := attr(item, "src")
		if v == "" {
			continue
		}
		getNext(v)
	}

	return nil
}
func (s *Spider) parseCss(body *bytes.Buffer, location *url.URL) error {
	return nil
}

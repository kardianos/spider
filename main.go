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
	"sync"
	"time"

	"github.com/andybalholm/cascadia"
	"github.com/tdewolff/parse/css"
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
	s := NewSpider(*rootFolder)

	urls := strings.Split(*startUrl, ",")
	for _, u := range urls {
		s.AddHost(u)
		s.EnqueueUrl(u, nil)
	}
	s.Run(*waitBetween)
}

type Spider struct {
	FolderRoot string

	lock   sync.RWMutex
	hosts  map[string]struct{}
	viewed map[string]struct{}

	queue chan string

	inProcess     int
	inProcessLock sync.RWMutex
}

func NewSpider(root string) *Spider {
	return &Spider{
		FolderRoot: root,
		hosts:      make(map[string]struct{}, 1),
		viewed:     make(map[string]struct{}, 100),
		queue:      make(chan string, 100),
	}
}

func (s *Spider) processAdd() {
	s.inProcessLock.Lock()
	defer s.inProcessLock.Unlock()
	s.inProcess++
}

func (s *Spider) processDone() {
	s.inProcessLock.Lock()
	defer s.inProcessLock.Unlock()
	s.inProcess--
}
func (s *Spider) processAllDone() bool {
	s.inProcessLock.RLock()
	defer s.inProcessLock.RUnlock()
	return s.inProcess == 0
}

func (s *Spider) Run(wait time.Duration) {
	if len(s.queue) == 0 {
		return
	}
	ticker := time.NewTicker(wait)
	for {
		select {
		case <-ticker.C:
			go s.getUrl()
			if len(s.queue) == 0 && s.processAllDone() {
				return
			}
		}
	}
}
func (s *Spider) AddHost(urlString string) {
	u, err := url.Parse(urlString)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	s.lock.Lock()
	s.hosts[u.Host] = struct{}{}
	s.lock.Unlock()
}

func (s *Spider) EnqueueUrl(next string, location *url.URL) {
	var urlString = next
	if location != nil {
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
		urlString = nextUrl.String()
	}

	s.lock.Lock()
	if _, seen := s.viewed[urlString]; seen {
		s.lock.Unlock()
		return
	}
	s.viewed[urlString] = struct{}{}
	s.lock.Unlock()

	s.queue <- urlString
}

func (s *Spider) getUrl() {
	urlString := <-s.queue
	s.processAdd()
	defer s.processDone()

	tryU, err := url.Parse(urlString)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
		return
	}
	s.lock.RLock()
	if _, has := s.hosts[tryU.Host]; !has {
		s.lock.RUnlock()
		return
	}
	s.lock.RUnlock()

	res, err := http.Get(urlString)
	if err != nil {
		log.Printf("%v: %s\n", err, urlString)
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
	file := filepath.Join(s.FolderRoot, res.Request.URL.Path)
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
	log.Printf("%s: %s\n", ct, res.Request.URL.String())
	err = s.Parse(ct, res.Request.URL, body)
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
		s.EnqueueUrl(next, location)
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
	parser := css.NewParser(body, true)
	for {
		gr, _, _ := parser.Next()
		if gr == css.ErrorGrammar {
			break
		}
		vals := parser.Values()
		for _, v := range vals {
			if v.TokenType == css.URLToken {
				var us = v.Data
				us = bytes.TrimPrefix(us, []byte("url("))
				us = bytes.TrimSuffix(us, []byte(")"))
				us = bytes.Trim(us, `"'`)
				s.EnqueueUrl(string(us), location)
			}
		}
	}
	return nil
}

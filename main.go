// The MIT License (MIT)

// Copyright (c) 2015 Matt Ho

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

	"github.com/codegangsta/cli"
	"github.com/mitchellh/goamz/aws"
	"github.com/mitchellh/goamz/s3"
)

type Options struct {
	Port      string
	Username  string
	Password  string
	Realm     string
	Bucket    string
	Prefix    string
	MaxAge    int
	Verbose   bool
	IndexFile string
}

func (o *Options) RequiresAuth() bool {
	return o.Username != "" && o.Password != ""
}

func Opts(c *cli.Context) *Options {
	return &Options{
		Port:      c.String("port"),
		Username:  c.String("username"),
		Password:  c.String("password"),
		Realm:     c.String("realm"),
		Bucket:    c.String("bucket"),
		Prefix:    c.String("prefix"),
		MaxAge:    c.Int("max-age"),
		Verbose:   c.Bool("verbose"),
		IndexFile: c.String("index-file"),
	}
}

func main() {
	app := cli.NewApp()
	app.Flags = []cli.Flag{
		cli.StringFlag{"port", "8080", "port to run on", "PORT"},
		cli.StringFlag{"username", "", "the username to prompt for", "USERNAME"},
		cli.StringFlag{"password", "", "the password to prompt for", "PASSWORD"},
		cli.StringFlag{"realm", "Realm", "the challenge realm", "REALM"},
		cli.StringFlag{"bucket", "", "the name of the s3 bucket to serve from", "BUCKET"},
		cli.StringFlag{"prefix", "", "the optional prefix to serve from e.g. s3://bucket/prefix/...", "PREFIX"},
		cli.IntFlag{"max-age", 90, "the cache-control header; max-age", "MAX_AGE"},
		cli.BoolFlag{"verbose", "enable enhanced logging", "VERBOSE"},
		cli.StringFlag{"index-file", "index.html", "file to search for indexes", "INDEX"},
	}
	app.Action = Run
	app.Run(os.Args)
}

func check(err error) {
	if err != nil {
		log.Fatalln(err)
	}
}

func Run(c *cli.Context) {
	opts := Opts(c)

	handler, err := S3Handler(opts)
	check(err)

	if opts.Verbose {
		log.Printf("starting server on port %s\n", opts.Port)
	}
	err = http.ListenAndServe(":"+opts.Port, handler)
	check(err)
}

func S3Handler(opts *Options) (http.HandlerFunc, error) {
	auth, err := aws.EnvAuth()
	if err != nil {
		return nil, err
	}

	api := s3.New(auth, aws.USEast)
	bucket := api.Bucket(opts.Bucket)
	if opts.Verbose {
		log.Printf("s3 bucket: %s\n", opts.Bucket)
	}

	return func(w http.ResponseWriter, req *http.Request) {
		if opts.RequiresAuth() {
			u, p, _ := req.BasicAuth()
			if opts.Verbose {
				log.Printf("Authorization: %s/%s\n", u, p)
			}

			if u != opts.Username || p != opts.Password {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=\"%s\"", opts.Realm))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}

		path := fmt.Sprintf("%s%s", opts.Prefix, req.URL.Path)
		if strings.Contains(path, "//") {
			path = strings.Replace(path, "//", "/", -1)
		}
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		if strings.HasSuffix(req.URL.Path, "/") {
			path = path + opts.IndexFile
		}
		if opts.Verbose {
			log.Printf("> %s => s3://%s/%s\n", req.URL.Path, opts.Bucket, path)
		}

		readCloser, err := bucket.GetReader(path)
		if err != nil {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf("Basic realm=\"%s\"", opts.Realm))
			w.WriteHeader(http.StatusNotFound)
			return
		}
		defer readCloser.Close()

		contentType := mime.TypeByExtension(path)
		w.Header().Set("Content-Type", contentType)

		io.Copy(w, readCloser)
	}, nil
}

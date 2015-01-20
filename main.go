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

		path := fmt.Sprintf("%s%s", opts.Prefix, req.RequestURI)
		if strings.Contains(path, "//") {
			path = strings.Replace(path, "//", "/", -1)
		}
		if strings.HasPrefix(path, "/") {
			path = path[1:]
		}
		if strings.HasSuffix(req.RequestURI, "/") {
			path = path + opts.IndexFile
		}
		if opts.Verbose {
			log.Printf("> %s => s3://%s/%s\n", req.RequestURI, opts.Bucket, path)
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

package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

type Config struct {
	// Options for listening for incoming messages.
	BindAddr        string        `help:"local bind address"`
	SocketFd        int           `help:"file descriptor of socket to listen on"`
	Credentials     string        `help:"username:password for authenticating to failmail"`
	TlsCert         string        `help:"PEM certificate file for TLS"`
	TlsKey          string        `help:"PEM key file for TLS"`
	ShutdownTimeout time.Duration `help:"wait this long for open connections to finish when shutting down or reloading"`

	// Options for storing messages.
	MemoryStore  bool   `help:"store messages in memory instead of an on-disk maildir"`
	MessageStore string `help:"use this directory as a maildir for holding received messages"`

	// Options for summarizing messages.
	From       string        `help:"from address"`
	WaitPeriod time.Duration `help:"wait this long for more batchable messages"`
	MaxWait    time.Duration `help:"wait at most this long from first message to send summary"`
	BatchExpr  string        `help:"an expression used to determine how messages are batched into summary emails"`
	GroupExpr  string        `help:"an expression used to determine how messages are grouped within summary emails"`
	Template   string        `help:"path to a summary message template file"`

	// Options for relaying outgoing messages.
	RelayAddr     string `help:"upstream relay server address"`
	RelayUser     string `help:"username for auth to relay server"`
	RelayPassword string `help:"password for auth to relay server"`
	FailDir       string `help:"write failed sends to this maildir"`
	AllDir        string `help:"write all sends to this maildir"`

	// Options that control what gets run.
	Receiver bool `help:"receive and store incoming messages"`
	Sender   bool `help:"summarize and send messages"`

	// Monitoring options.
	BindHTTP string `help:"local bind address for the HTTP server"`
	Pidfile  string `help:"write a pidfile to this path"`

	Version bool `help:"show the version number and exit"`
}

func Defaults() *Config {
	return &Config{
		BindAddr:        "localhost:2525",
		ShutdownTimeout: 5 * time.Second,

		MessageStore: "incoming",

		From:       DefaultFromAddress("failmail"),
		WaitPeriod: 30 * time.Second,
		MaxWait:    5 * time.Minute,
		BatchExpr:  `{{.Header.Get "X-Failmail-Split"}}`,
		GroupExpr:  `{{.Header.Get "Subject"}}`,

		RelayAddr: "localhost:25",
		FailDir:   "failed",

		BindHTTP: "localhost:8025",
	}
}

func (c *Config) Auth() (Auth, error) {
	if c.Credentials == "" {
		return nil, nil
	}

	parts := strings.SplitN(c.Credentials, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("credentials must be in username:password format")
	}

	return &SingleUserPlainAuth{Username: parts[0], Password: parts[1]}, nil
}

func (c *Config) Batch() GroupBy {
	return GroupByExpr("batch", c.BatchExpr)
}

func (c *Config) Group() GroupBy {
	return GroupByExpr("group", c.GroupExpr)
}

func (c *Config) Upstream() (Upstream, error) {
	var upstream Upstream
	if c.RelayAddr == "debug" {
		upstream = &DebugUpstream{os.Stdout}
	} else {
		upstream = &LiveUpstream{logger("upstream"), c.RelayAddr, c.RelayUser, c.RelayPassword}
	}

	if c.AllDir != "" {
		allMaildir := &Maildir{Path: c.AllDir}
		if err := allMaildir.Create(); err != nil {
			return upstream, err
		}
		upstream = NewMultiUpstream(&MaildirUpstream{allMaildir}, upstream)
	}
	return upstream, nil
}

func (c *Config) TLSConfig() (*tls.Config, error) {
	if c.TlsCert == "" || c.TlsKey == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(c.TlsCert, c.TlsKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func (c *Config) Socket() (ServerSocket, error) {
	if c.SocketFd > 0 {
		return NewFileServerSocket(uintptr(c.SocketFd))
	}
	return NewTCPServerSocket(c.BindAddr)
}

func (c *Config) SummaryRenderer() SummaryRenderer {
	if c.Template != "" {
		tmpl := template.Must(template.New(c.Template).Funcs(SUMMARY_TEMPLATE_FUNCS).ParseFiles(c.Template))
		return &TemplateRenderer{tmpl}
	}
	return &NoRenderer{}
}

func (c *Config) Store() (MessageStore, error) {
	switch {
	case c.MemoryStore:
		return NewMemoryStore(), nil
	case c.MessageStore == "":
		return nil, fmt.Errorf("must have either a memory store or a disk-backed store")
	default:
		maildir := &Maildir{Path: c.MessageStore}
		err := maildir.Create()
		if err != nil {
			return nil, err
		}
		return NewDiskStore(maildir)
	}
}

func (c *Config) Listener() (*Listener, error) {
	auth, err := c.Auth()
	if err != nil {
		return nil, err
	}

	tlsConfig, err := c.TLSConfig()
	if err != nil {
		return nil, err
	}

	// The listener talks SMTP to clients, and puts any messages they send onto
	// the `received` channel.
	socket, err := c.Socket()
	if err != nil {
		return nil, err
	}

	listener := &Listener{Logger: logger("listener"), Socket: socket, Auth: auth, TLSConfig: tlsConfig}
	return listener, nil
}

func (c *Config) Writer() (*MessageWriter, error) {
	if store, err := c.Store(); err != nil {
		return nil, err
	} else {
		return &MessageWriter{store}, nil
	}
}

func (c *Config) Buffer() (*MessageBuffer, error) {
	if store, err := c.Store(); err != nil {
		return nil, err
	} else {
		return &MessageBuffer{
			SoftLimit: c.WaitPeriod,
			HardLimit: c.MaxWait,
			Batch:     c.Batch(),
			Group:     c.Group(),
			From:      c.From,
			Store:     store,
			Renderer:  c.SummaryRenderer(),
			batches:   NewBatches(),
		}, nil
	}
}

func (c *Config) MakeSender() (*Sender, error) {
	upstream, err := c.Upstream()
	if err != nil {
		return nil, err
	}

	failedMaildir := &Maildir{Path: c.FailDir}
	if err := failedMaildir.Create(); err != nil {
		return nil, err
	}

	return &Sender{upstream, failedMaildir}, nil
}

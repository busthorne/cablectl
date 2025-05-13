package langfuse

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/busthorne/cablectl/gateway/api"
)

type Client struct {
	API *api.Client
}

type ClientOptions struct {
	Host       string
	PrivateKey string
	PublicKey  string

	HTTPClient *http.Client
}

func New(opts *ClientOptions) (*Client, error) {
	const (
		cloudHost = "https://cloud.langfuse.com"
		keepAlive = 30 * time.Second
	)

	if opts.Host == "" {
		opts.Host = cloudHost
	}
	if opts.PublicKey == "" {
		return nil, errors.New("langfuse: public key is required")
	}
	if opts.PrivateKey == "" {
		return nil, errors.New("langfuse: private key is required")
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2:     true,
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				MaxConnsPerHost:       0,
				DisableKeepAlives:     false,
				DisableCompression:    false,
				IdleConnTimeout:       3 * keepAlive,
				TLSHandshakeTimeout:   keepAlive,
				ExpectContinueTimeout: 1 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   keepAlive,
					KeepAlive: keepAlive,
				}).DialContext,
			},
			Timeout: keepAlive,
		}
	}

	basicAuth := func(ctx context.Context, req *http.Request) error {
		b := []byte(opts.PublicKey + ":" + opts.PrivateKey)
		h := "Basic " + base64.StdEncoding.EncodeToString(b)
		req.Header.Set("Authorization", h)
		return nil
	}
	api, err := api.NewClient(opts.Host,
		api.WithBaseURL(opts.Host),
		api.WithRequestEditorFn(basicAuth))
	if err != nil {
		return nil, fmt.Errorf("langfuse: %w", err)
	}

	client := &Client{
		API: api,
	}
	return client, nil
}
